// Package ioscompanion drives an iOS simulator through the native simulator
// companion. This file implements the DeviceDriver surface on top of the
// brand-free transport, supervises the companion child process, and recovers
// from a dropped connection with one in-place restart.
package ioscompanion

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/priyanshujain/sanderling/internal/driver"
	"github.com/priyanshujain/sanderling/internal/driver/ioscompanion/companionassets"
	"github.com/priyanshujain/sanderling/internal/driver/ioscompanion/transport"
	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// startupTimeout bounds how long New waits for the spawned companion to accept
// a connection and answer a health probe.
const startupTimeout = 30 * time.Second

// runnerStartupTimeout bounds the in-simulator runner's startup. The runner is
// hosted by a test session whose cold start is far slower than the companion's.
const runnerStartupTimeout = 120 * time.Second

// shutdownGrace bounds how long the companion child gets to exit after SIGTERM
// before it is killed. A variable so the kill-escalation test can shrink it.
var shutdownGrace = 15 * time.Second

// launchTimeout bounds a single app lifecycle RPC. The runner serves lifecycle
// inside its XCTest session, and a launch the simulator rejects sends that
// session down a recovery chain (a 120s accessibility wait, a spindump, then an
// idle wait) that answers minutes late or never. Callers reach Launch with an
// undeadlined context, since it runs before the run's duration clock starts, so
// the bound has to come from here or a wedged session hangs the run with no
// trace, no error, and no end. Kept under runnerStartupTimeout: launching an
// app inside a live session must cost less than cold-starting that session.
// A variable so the timeout test can shrink it.
var launchTimeout = 90 * time.Second

// launchRecoveryTimeout bounds the whole recovery a blown launch bound
// triggers, the session restart and the second attempt together. It keeps the
// launch path inside the three minutes testrun allows it, so what a user sees
// when the app really cannot be launched stays the driver's error rather than
// that backstop firing over the top of it. A variable so the bound test can
// shrink it.
var launchRecoveryTimeout = 60 * time.Second

// longPressHoldMilliseconds is how long LongPress holds the finger down.
const longPressHoldMilliseconds = 600

// Options configures a Driver.
type Options struct {
	// UniqueDeviceIdentifier selects the booted simulator the companion drives.
	UniqueDeviceIdentifier string
	// BundleID is the app under test. Launch and Terminate act on it.
	BundleID string
	// AppPath is the .app bundle directory. Required for clear-state reinstall;
	// when empty, clear state falls back to resetting the data container.
	AppPath string
	// Output receives companion stdout and stderr plus driver warnings.
	Output io.Writer
	// DoubleTapGapMilliseconds overrides the synthesized double-tap gap.
	DoubleTapGapMilliseconds float64

	// spawnChild, dialCompanion, and pickAddress are test seams. Production
	// leaves them nil and New wires the real extraction, spawn, and dial.
	spawnChild    func(ctx context.Context, address string) (*exec.Cmd, error)
	dialCompanion func(address string) (transport.Companion, error)
	pickAddress   func() (string, error)
}

// Driver implements driver.DeviceDriver against an iOS simulator companion.
type Driver struct {
	companion transport.Companion
	udid      string
	bundleID  string
	appPath   string
	output    io.Writer

	screenWidth  int
	screenHeight int

	doubleTapGapMilliseconds float64

	// mu guards Snapshot's hierarchy+screenshot pairing and the lastTap record.
	mu      sync.Mutex
	lastTap struct {
		x, y float64
		set  bool
	}

	clearStateWarned bool

	// restart rebuilds the transport in place after a connection-level failure.
	// It is a seam so tests exercise the supervision logic without spawning a
	// real companion. restarting guards against re-entrant restarts.
	restart    func(ctx context.Context) error
	restarting bool
	address    string

	// resetContainer wipes the app data container for the clear-state fallback.
	// A seam so tests skip the xcrun shell-out.
	resetContainer func(ctx context.Context) error

	// reinstallApp uninstalls and reinstalls the app bundle for clear-state.
	// A seam so tests skip the simctl shell-outs.
	reinstallApp func(ctx context.Context) error

	// grantPaste pre-authorizes the app's pasteboard access. A seam so tests
	// skip the sqlite shell-out.
	grantPaste func(ctx context.Context) error

	// idleClock drives WaitForIdle's settle poll. A seam so tests substitute a
	// fake clock and avoid the real settle cap.
	idleClock  Clock
	spawnChild func(ctx context.Context, address string) (*exec.Cmd, error)
	dial       func(address string) (transport.Companion, error)
	child      *exec.Cmd

	// The hybrid simulator companion pairs the legacy child (HID gestures,
	// lifecycle, screenshot) with an in-simulator runner that serves
	// collapse-free accessibility snapshots and native unicode typing.
	// runnerClient is nil on the legacy-only path.
	runnerClient  transport.Companion
	runnerChild   *exec.Cmd
	runnerAddress string
	spawnRunner   func(ctx context.Context, address string) (*exec.Cmd, error)
	dialRunner    func(address string) (transport.Companion, error)
	hybrid        bool

	// Device-mode fields. On the physical-device path d.companion is the runner
	// dialed over a usbmux tunnel, hybrid is false, and runnerClient is nil.
	// coreDeviceID feeds devicectl; tunnel is the in-process usbmux forwarder
	// bridging the host loopback port to the runner's device-side port.
	deviceMode        bool
	coreDeviceID      string
	tunnel            io.Closer
	startTunnel       func(ctx context.Context, hardwareUDID, localAddress, devicePort string) (io.Closer, error)
	pickDeviceAddress func() (string, error)

	// processContext owns the companion child's lifetime: it is derived from
	// New's context (so a canceled run still reaps the child) and canceled by
	// Close. Spawning under a startup-scoped context would SIGTERM the child
	// the moment startup finishes.
	processContext context.Context
	processCancel  context.CancelFunc

	// deviceLock is the exclusive claim on the target, held for the driver's
	// whole life and released by Close.
	deviceLock io.Closer
}

// acquireDeviceLock takes an exclusive advisory lock on the target so only one
// run drives it at a time. Two runs on one device interleave app lifecycle: the
// second run's uninstall and reinstall land under the first's live automation
// session, leaving its app proxies bound to a bundle the simulator no longer
// knows, and every later snapshot and launch on that session stalls. Failing
// fast beats recovering silently, since the other run owns the device and would
// be corrupted either way. The lock lives on the file descriptor, so a crashed
// run's claim is released by the kernel and never strands the device.
func acquireDeviceLock(udid string) (io.Closer, error) {
	path := filepath.Join(os.TempDir(), "sanderling-ios-"+udid+".lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open device lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf(
			"ios target %s is already driven by another sanderling run (lock %s); "+
				"wait for that run to finish or point this one at a different device with --ios-device",
			udid, path)
	}
	return file, nil
}

// New extracts the embedded companion, spawns it against the configured
// simulator, dials the transport, health-probes it, and caches the screen
// point dimensions. Call Close when done to stop the child.
func New(ctx context.Context, options Options) (*Driver, error) {
	if options.UniqueDeviceIdentifier == "" {
		return nil, errors.New("ios companion: UniqueDeviceIdentifier is required")
	}
	output := options.Output
	if output == nil {
		output = io.Discard
	}
	gap := options.DoubleTapGapMilliseconds
	if gap <= 0 {
		gap = DefaultDoubleTapGapMilliseconds
	}

	driverInstance := &Driver{
		udid:                     options.UniqueDeviceIdentifier,
		bundleID:                 options.BundleID,
		appPath:                  options.AppPath,
		output:                   output,
		doubleTapGapMilliseconds: gap,
		spawnChild:               options.spawnChild,
		dial:                     options.dialCompanion,
		hybrid:                   hybridCompanionEnabled(),
	}
	if driverInstance.spawnChild == nil {
		driverInstance.spawnChild = driverInstance.realSpawnChild
	}
	if driverInstance.dial == nil {
		driverInstance.dial = transport.Dial
	}
	if driverInstance.spawnRunner == nil {
		driverInstance.spawnRunner = driverInstance.realSpawnRunner
	}
	if driverInstance.dialRunner == nil {
		driverInstance.dialRunner = func(address string) (transport.Companion, error) {
			return transport.DialRunner(address, driverInstance.udid, driverInstance.bundleID)
		}
	}
	pickAddress := options.pickAddress
	if pickAddress == nil {
		pickAddress = pickLoopbackAddress
	}

	address, err := pickAddress()
	if err != nil {
		return nil, err
	}
	driverInstance.address = address
	driverInstance.restart = driverInstance.respawnAndRedial
	driverInstance.resetContainer = driverInstance.resetDataContainer
	driverInstance.reinstallApp = driverInstance.simctlReinstall
	driverInstance.grantPaste = driverInstance.grantPasteboardAccess
	driverInstance.processContext, driverInstance.processCancel = context.WithCancel(ctx)

	lock, err := acquireDeviceLock(driverInstance.udid)
	if err != nil {
		driverInstance.processCancel()
		return nil, err
	}
	driverInstance.deviceLock = lock

	if err := driverInstance.bringUp(ctx); err != nil {
		driverInstance.Close()
		return nil, err
	}
	if driverInstance.hybrid {
		if err := driverInstance.bringUpRunner(ctx); err != nil {
			driverInstance.Close()
			return nil, fmt.Errorf("simulator runner: %w (set SANDERLING_SIMULATOR_COMPANION=legacy to bypass)", err)
		}
	}

	description, err := driverInstance.companion.Describe(ctx)
	if err != nil {
		driverInstance.Close()
		return nil, fmt.Errorf("describe target: %w", err)
	}
	driverInstance.screenWidth = description.WidthPoints
	driverInstance.screenHeight = description.HeightPoints
	return driverInstance, nil
}

// bringUp spawns the companion child, waits for the listener, dials, and
// confirms health. It is used by New and by the in-place restart.
func (d *Driver) bringUp(ctx context.Context) error {
	startupCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	child, err := d.spawnChild(d.processContext, d.address)
	if err != nil {
		return fmt.Errorf("spawn companion: %w", err)
	}
	d.child = child

	if err := waitForListener(startupCtx, d.address); err != nil {
		d.stopChild()
		return fmt.Errorf("companion listener: %w", err)
	}

	companion, err := d.dial(d.address)
	if err != nil {
		d.stopChild()
		return fmt.Errorf("dial companion: %w", err)
	}
	d.companion = companion

	if err := d.waitForHealth(startupCtx); err != nil {
		_ = companion.Close()
		d.stopChild()
		return fmt.Errorf("companion health: %w", err)
	}
	return nil
}

// waitForHealth probes AccessibilityInfo until it succeeds or the context
// expires. A successful describe-all means the companion is attached to the
// simulator and ready to serve.
func (d *Driver) waitForHealth(ctx context.Context) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := d.companion.AccessibilityInfo(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// respawnAndRedial tears down the current transports and children, then brings
// fresh ones up at the same addresses. Used as the supervision restart. On the
// hybrid path both halves restart together: their failure modes overlap (a
// rebooted simulator drops both) and one orchestration keeps recovery simple.
func (d *Driver) respawnAndRedial(ctx context.Context) error {
	// The dead transports are closed but kept in place until their fresh
	// replacements land: if the restart fails, later calls error gracefully on
	// the closed transport instead of dereferencing nil, and a later incident
	// earns another restart attempt.
	if d.companion != nil {
		_ = d.companion.Close()
	}
	d.stopChild()
	if d.runnerClient != nil {
		_ = d.runnerClient.Close()
	}
	d.stopRunnerChild()
	if err := d.bringUp(ctx); err != nil {
		return err
	}
	if d.hybrid {
		return d.bringUpRunner(ctx)
	}
	return nil
}

// hybridCompanionEnabled reports whether the simulator driver should pair the
// legacy companion with the in-simulator runner. The hybrid is the default;
// SANDERLING_SIMULATOR_COMPANION=legacy forces the legacy companion alone.
func hybridCompanionEnabled() bool {
	return os.Getenv("SANDERLING_SIMULATOR_COMPANION") != "legacy"
}

// bringUpRunner spawns the in-simulator runner, waits for its listener, dials,
// and confirms it serves snapshots. Used by New and the in-place restart.
func (d *Driver) bringUpRunner(ctx context.Context) error {
	startupCtx, cancel := context.WithTimeout(ctx, runnerStartupTimeout)
	defer cancel()

	// A fresh port every bring-up: after a restart the dying session's
	// listener may still answer on the old port and would satisfy the wait
	// below with a dead server.
	address, err := pickLoopbackAddress()
	if err != nil {
		return err
	}
	d.runnerAddress = address

	child, err := d.spawnRunner(d.processContext, d.runnerAddress)
	if err != nil {
		return fmt.Errorf("spawn runner: %w", err)
	}
	d.runnerChild = child

	if err := waitForListener(startupCtx, d.runnerAddress); err != nil {
		d.stopRunnerChild()
		return fmt.Errorf("runner listener: %w", err)
	}

	client, err := d.dialRunner(d.runnerAddress)
	if err != nil {
		d.stopRunnerChild()
		return fmt.Errorf("dial runner: %w", err)
	}

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, healthErr := client.AccessibilityInfo(startupCtx); healthErr == nil {
			break
		}
		select {
		case <-startupCtx.Done():
			_ = client.Close()
			d.stopRunnerChild()
			return fmt.Errorf("runner health: %w", startupCtx.Err())
		case <-ticker.C:
		}
	}
	d.runnerClient = client
	return nil
}

// withRecovery runs call, and on a connection-level failure performs one
// in-place restart before retrying the call once. A non-connection error, or a
// second failure of any kind, surfaces to the caller. The restart budget is per
// failure incident: each healthy call resets restarting to false, so a later
// drop earns its own single restart.
func (d *Driver) withRecovery(ctx context.Context, call func() error) error {
	err := call()
	if err == nil || !isConnectionError(err) || d.restarting || d.restart == nil {
		return err
	}
	d.restarting = true
	defer func() { d.restarting = false }()
	fmt.Fprintf(d.output, "companion connection lost (%v); restarting once\n", err)
	// The restart runs under the driver's own lifetime context, not the
	// failed call's: an action whose deadline already expired must not doom
	// the recovery that later actions depend on.
	restartCtx := d.processContext
	if restartCtx == nil {
		restartCtx = ctx
	}
	if restartErr := d.restart(restartCtx); restartErr != nil {
		return fmt.Errorf("companion restart failed: %w (original: %v)", restartErr, err)
	}
	return call()
}

// isConnectionError reports whether err is a dropped-connection signal that a
// restart can recover from: the transport's unavailable sentinel, a gRPC
// Unavailable status, or an EOF.
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, transport.ErrCompanionUnavailable) {
		return true
	}
	if statusValue, ok := status.FromError(err); ok {
		return statusValue.Code() == codes.Unavailable
	}
	return false
}

func (d *Driver) Launch(ctx context.Context, bundleID string, clearState bool, env map[string]string) error {
	if bundleID != "" {
		d.bundleID = bundleID
	}
	if len(env) > 0 {
		// The launch Start message carries an env map, but this backend does
		// not pass it through: passing it would change the app's process
		// environment in ways the rest of the run does not account for. Reject
		// loudly rather than silently dropping the request.
		return errors.New("ios companion: launch with environment variables is unsupported on this backend")
	}

	// Terminate first so the launch is a clean cold start regardless of the
	// app's prior state. A not-running app is not an error here.
	_ = d.lifecycleCall(ctx, func(callCtx context.Context, companion transport.Companion) error {
		return companion.Terminate(callCtx, d.bundleID)
	})

	if clearState {
		if err := d.clearAppState(ctx); err != nil {
			return err
		}
	}

	// Grant the app pasteboard access before it runs so unicode input (which
	// must go through the pasteboard, since HID cannot express it) never trips
	// the iOS paste-permission prompt. clearState reinstall resets the grant,
	// so it is reapplied on every launch. Best effort: if it fails, the paste
	// path still handles the prompt, just slower. The hybrid path types
	// natively and never touches the pasteboard, so it skips the grant.
	if d.bundleID != "" && d.runnerTyper() == nil {
		if err := d.grantPaste(ctx); err != nil {
			fmt.Fprintf(d.output, "grant pasteboard access failed (continuing): %v\n", err)
		}
	}

	if err := d.launchWithSessionRecovery(ctx); err != nil {
		return fmt.Errorf("launch %s: %w", d.bundleID, err)
	}
	return nil
}

// launchWithSessionRecovery runs the launch RPC and, when it blows its own
// bound, replaces the session and launches again.
//
// A launch the simulator refuses, which is what a clear-state reinstall racing
// FrontBoard's registration produces, never comes back as an error: XCTest
// records the refusal as a test failure the runner cannot observe, then holds
// the session's main thread for about four minutes walking a diagnostic chain
// (a 120s accessibility wait, a spindump, an idle wait). So there is no error
// text to key a retry on, only the expired bound, and every later call queues
// behind the same wedge. Only a session that never served the refused launch
// can serve the retry, which is why this restarts rather than calls again.
func (d *Driver) launchWithSessionRecovery(ctx context.Context) error {
	launch := func(callCtx context.Context, companion transport.Companion) error {
		return companion.Launch(callCtx, d.bundleID, true)
	}
	err := d.lifecycleCall(ctx, launch)
	// A caller whose own budget ran out gets no restart: the bound that expired
	// was the caller's to spend, and the second attempt would inherit it dead.
	if err == nil || !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil || d.restart == nil {
		return err
	}
	fmt.Fprintf(d.output, "launch %s blew its %v bound (%v); restarting the session and launching once more\n",
		d.bundleID, launchTimeout, err)

	// The restart runs under the driver's own lifetime context for the same
	// reason withRecovery's does, while the second attempt stays on the
	// caller's. Both end at one deadline, so a launch that already spent
	// launchTimeout cannot then wait out a session cold start on top of it.
	recoveryDeadline := time.Now().Add(launchRecoveryTimeout)
	restartCtx := d.processContext
	if restartCtx == nil {
		restartCtx = ctx
	}
	restartCtx, cancelRestart := context.WithDeadline(restartCtx, recoveryDeadline)
	defer cancelRestart()
	if restartErr := d.restart(restartCtx); restartErr != nil {
		return fmt.Errorf("session restart failed: %w (original: %v)", restartErr, err)
	}
	relaunchCtx, cancelRelaunch := context.WithDeadline(ctx, recoveryDeadline)
	defer cancelRelaunch()
	return d.lifecycleCall(relaunchCtx, launch)
}

// lifecycleCall runs an app lifecycle RPC against lifecycleCompanion under a
// launchTimeout-bounded context, with the usual one-restart recovery. The
// companion is resolved inside the retry so a restart's replacement client
// serves the second attempt.
func (d *Driver) lifecycleCall(ctx context.Context, call func(context.Context, transport.Companion) error) error {
	boundedCtx, cancel := context.WithTimeout(ctx, launchTimeout)
	defer cancel()
	return d.withRecovery(boundedCtx, func() error {
		return call(boundedCtx, d.lifecycleCompanion())
	})
}

// lifecycleCompanion is the transport that owns app launch and terminate: the
// in-simulator runner when the hybrid is active, otherwise the legacy
// companion. Lifecycle performed outside the runner's automation session
// leaves the session's app proxies bound to dead processes, after which
// snapshots hang and typing asserts.
func (d *Driver) lifecycleCompanion() transport.Companion {
	if d.runnerClient != nil {
		return d.runnerClient
	}
	return d.companion
}

// clearAppState resets the app to a first-launch state. With an app path it
// uninstalls and reinstalls; without one it falls back to wiping the app's data
// container and warns once that a full reinstall needs the app path.
func (d *Driver) clearAppState(ctx context.Context) error {
	if d.appPath != "" {
		if err := d.reinstallApp(ctx); err != nil {
			return fmt.Errorf("reinstall %s: %w", d.appPath, err)
		}
		return nil
	}
	if !d.clearStateWarned {
		fmt.Fprintln(d.output, "clear-state requested without an app path: resetting the data container only; pass the app path for a full reinstall")
		d.clearStateWarned = true
	}
	return d.resetContainer(ctx)
}

// simctlReinstall uninstalls and reinstalls the app bundle via simctl. App
// lifecycle stays with simctl: the companion's install RPC misreads current
// simulator targets' architectures and rejects valid bundles.
func (d *Driver) simctlReinstall(ctx context.Context) error {
	_ = exec.CommandContext(ctx, "xcrun", "simctl", "uninstall", d.udid, d.bundleID).Run()
	output, err := exec.CommandContext(ctx, "xcrun", "simctl", "install", d.udid, d.appPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("simctl install: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// grantPasteboardAccess authorizes the app to read the pasteboard without the
// iOS permission prompt, by writing an allow row into the simulator's privacy
// (TCC) database. This is the simulator counterpart to `simctl privacy grant`,
// which does not expose the pasteboard service. Without it, every unicode input
// (which must paste, since HID cannot express unicode) blocks on a modal that
// costs seconds; with it the paste lands in one frame.
func (d *Driver) grantPasteboardAccess(ctx context.Context) error {
	databasePath := filepath.Join(
		os.Getenv("HOME"), "Library", "Developer", "CoreSimulator", "Devices",
		d.udid, "data", "Library", "TCC", "TCC.db",
	)
	if _, err := os.Stat(databasePath); err != nil {
		return fmt.Errorf("locate privacy database: %w", err)
	}
	statement := fmt.Sprintf(
		"INSERT OR REPLACE INTO access "+
			"(service,client,client_type,auth_value,auth_reason,auth_version,indirect_object_identifier) "+
			"VALUES ('kTCCServicePasteboard','%s',0,2,4,1,'UNUSED');",
		d.bundleID,
	)
	output, err := exec.CommandContext(ctx, "sqlite3", databasePath, statement).CombinedOutput()
	if err != nil {
		return fmt.Errorf("write privacy grant: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// resetDataContainer deletes the contents of the app's data container so the
// next launch starts with empty storage.
func (d *Driver) resetDataContainer(ctx context.Context) error {
	output, err := exec.CommandContext(ctx, "xcrun", "simctl", "get_app_container", d.udid, d.bundleID, "data").Output()
	if err != nil {
		return fmt.Errorf("get app container: %w", err)
	}
	container := string(bytes.TrimSpace(output))
	if container == "" {
		return nil
	}
	entries, err := os.ReadDir(container)
	if err != nil {
		return fmt.Errorf("read app container: %w", err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(container, entry.Name())); err != nil {
			return fmt.Errorf("clear app container: %w", err)
		}
	}
	return nil
}

func (d *Driver) Terminate(ctx context.Context) error {
	return d.lifecycleCall(ctx, func(callCtx context.Context, companion transport.Companion) error {
		return companion.Terminate(callCtx, d.bundleID)
	})
}

func (d *Driver) Tap(ctx context.Context, x, y int) error {
	d.mu.Lock()
	d.lastTap.x = float64(x)
	d.lastTap.y = float64(y)
	d.lastTap.set = true
	d.mu.Unlock()
	return d.withRecovery(ctx, func() error {
		return d.companion.SendHID(ctx, tapEvents(float64(x), float64(y))...)
	})
}

func (d *Driver) DoubleTap(ctx context.Context, x, y int) error {
	return d.withRecovery(ctx, func() error {
		return d.companion.SendHID(ctx, doubleTapEvents(float64(x), float64(y), d.doubleTapGapMilliseconds)...)
	})
}

func (d *Driver) LongPress(ctx context.Context, x, y int) error {
	return d.withRecovery(ctx, func() error {
		return d.companion.SendHID(ctx, longPressEvents(float64(x), float64(y), longPressHoldMilliseconds)...)
	})
}

func (d *Driver) Swipe(ctx context.Context, fromX, fromY, toX, toY int, duration time.Duration) error {
	seconds := duration.Seconds()
	if seconds <= 0 {
		seconds = 0.25
	}
	return d.withRecovery(ctx, func() error {
		return d.companion.SendHID(ctx, transport.SwipeEvent(
			float64(fromX), float64(fromY), float64(toX), float64(toY), seconds))
	})
}

func (d *Driver) PressKey(ctx context.Context, key string) error {
	if d.textEditor() != nil {
		return d.withRecovery(ctx, func() error {
			return d.textEditor().PressKey(ctx, key)
		})
	}
	usage, ok := pressKeyUsage(key)
	if !ok {
		return fmt.Errorf("ios companion: unsupported key %q", key)
	}
	return d.withRecovery(ctx, func() error {
		return d.companion.SendHID(ctx, transport.KeyDown(usage), transport.KeyUp(usage))
	})
}

// textEditor returns the companion's native text-editing capability, or nil
// when the transport does not implement it. Resolved per call because a
// restart replaces d.companion.
func (d *Driver) textEditor() transport.TextEditor {
	if editor, ok := d.companion.(transport.TextEditor); ok {
		return editor
	}
	return nil
}

// runnerTyper returns the in-simulator runner's native typing capability, or
// nil outside the hybrid path. Resolved per call because a restart replaces
// d.runnerClient.
func (d *Driver) runnerTyper() transport.TextTyper {
	if typer, ok := d.runnerClient.(transport.TextTyper); ok {
		return typer
	}
	return nil
}

// pressKeyUsage maps the logical key names mobile runs emit to a HID usage.
// Only Return/Enter has a hardware-keyboard equivalent on the simulator; other
// names (notably "back" and "home") have no HID key and report unsupported.
func pressKeyUsage(key string) (uint32, bool) {
	switch key {
	case "enter", "return", "Enter", "Return":
		return usageReturn, true
	default:
		return 0, false
	}
}

func (d *Driver) TapSelector(ctx context.Context, selector string) error {
	x, y, err := d.resolveSelectorCenter(ctx, selector)
	if err != nil {
		return err
	}
	return d.Tap(ctx, x, y)
}

func (d *Driver) DoubleTapSelector(ctx context.Context, selector string) error {
	x, y, err := d.resolveSelectorCenter(ctx, selector)
	if err != nil {
		return err
	}
	return d.DoubleTap(ctx, x, y)
}

// resolveSelectorCenter fetches a fresh hierarchy and returns the center of the
// first element matching selector.
func (d *Driver) resolveSelectorCenter(ctx context.Context, selector string) (int, int, error) {
	hierarchyJSON, err := d.Hierarchy(ctx)
	if err != nil {
		return 0, 0, err
	}
	tree, err := hierarchy.Parse(hierarchyJSON)
	if err != nil {
		return 0, 0, fmt.Errorf("parse hierarchy: %w", err)
	}
	element := tree.Find(selector)
	if element == nil {
		return 0, 0, fmt.Errorf("selector %q matched no element", selector)
	}
	x, y := element.Bounds.Center()
	return x, y, nil
}

func (d *Driver) InputText(ctx context.Context, text string) error {
	// Hybrid path. Mappable text rides one HID stream: select-all chord plus
	// keystrokes, atomic and strictly ordered on a single channel. Unicode
	// (which HID cannot express) is typed natively by the runner after the
	// chord; chord and typing ride different channels with no ordering
	// guarantee between them, so the clear is verified through a snapshot
	// before the first keystroke goes out.
	if typer := d.runnerTyper(); typer != nil {
		if !usesPasteboard(text) {
			events := append(clearFieldEvents(), keyPressEvents(typeStringPresses(text))...)
			return d.withRecovery(ctx, func() error {
				return d.companion.SendHID(ctx, events...)
			})
		}
		return d.withRecovery(ctx, func() error {
			if err := d.companion.SendHID(ctx, clearFieldEvents()...); err != nil {
				return fmt.Errorf("clear field: %w", err)
			}
			d.waitFieldCleared(ctx)
			return d.runnerTyper().TypeText(ctx, text, false)
		})
	}
	// A text-editing companion replaces the field's content natively, which
	// covers unicode without the pasteboard and its permission dialog.
	if d.textEditor() != nil {
		return d.withRecovery(ctx, func() error {
			return d.textEditor().InputText(ctx, text)
		})
	}
	// The field target is only needed for the pasteboard path. Resolving it
	// requires a describe-all, so the fast keyboard path skips that round-trip
	// and lets inputText send the key presses directly.
	var field fieldTarget
	if usesPasteboard(text) {
		field = d.resolveInputField(ctx)
	}
	return inputText(ctx, d.makeRunner(), text, field)
}

// fieldClearedWaitCap and fieldClearedPoll bound the verify-cleared loop
// between the HID clear chord and the runner's native typing.
const fieldClearedWaitCap = 1200 * time.Millisecond
const fieldClearedPoll = 150 * time.Millisecond

// waitFieldCleared polls the focused field (the editable element under the
// last tap) until its value reads empty, so the clear chord has demonstrably
// landed before typing starts on the other channel. Best effort: when the
// field cannot be resolved or the cap elapses, typing proceeds anyway.
func (d *Driver) waitFieldCleared(ctx context.Context) {
	d.mu.Lock()
	tap := d.lastTap
	d.mu.Unlock()
	if !tap.set {
		return
	}
	deadline := time.Now().Add(fieldClearedWaitCap)
	for time.Now().Before(deadline) {
		dump, err := d.describeAllRaw(ctx)
		if err != nil {
			return
		}
		cleared := true
		for _, element := range decodeDump(dump) {
			if !isEditable(element.Type) {
				continue
			}
			frame := element.Frame
			if !finite(frame.X) || !finite(frame.Y) || !finite(frame.Width) || !finite(frame.Height) {
				continue
			}
			if tap.x < frame.X || tap.x > frame.X+frame.Width ||
				tap.y < frame.Y || tap.y > frame.Y+frame.Height {
				continue
			}
			if value := stringValue(element.AXValue); value != "" && value != emptyFieldValueSentinel {
				cleared = false
			}
			break
		}
		if cleared {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(fieldClearedPoll):
		}
	}
}

// resolveInputField finds the editable element under the last tap so the
// pasteboard fallback can confirm the paste landed and refocus after dismissing
// the permission dialog. The runner always taps a field before typing, so
// lastTap names the focus point. An empty fieldTarget is returned when no
// editable element contains the tap (the fast keyboard path ignores it).
func (d *Driver) resolveInputField(ctx context.Context) fieldTarget {
	d.mu.Lock()
	tap := d.lastTap
	d.mu.Unlock()
	if !tap.set {
		return fieldTarget{}
	}
	dump, err := d.describeAll(ctx)
	if err != nil {
		return fieldTarget{}
	}
	for _, element := range decodeDump(dump) {
		if !isEditable(element.Type) {
			continue
		}
		frame := element.Frame
		if !finite(frame.X) || !finite(frame.Y) || !finite(frame.Width) || !finite(frame.Height) {
			continue
		}
		if tap.x < frame.X || tap.x > frame.X+frame.Width ||
			tap.y < frame.Y || tap.y > frame.Y+frame.Height {
			continue
		}
		return fieldTarget{
			identifier: stringValue(element.AXUniqueID),
			centerX:    frame.X + frame.Width/2,
			centerY:    frame.Y + frame.Height/2,
		}
	}
	return fieldTarget{}
}

func (d *Driver) EraseText(ctx context.Context, characterCount int) error {
	if d.textEditor() != nil {
		return d.withRecovery(ctx, func() error {
			return d.textEditor().EraseText(ctx, characterCount)
		})
	}
	return eraseText(ctx, d.makeRunner(), characterCount)
}

func (d *Driver) Hierarchy(ctx context.Context) (string, error) {
	dump, err := d.describeAll(ctx)
	if err != nil {
		return "", err
	}
	mapped, err := MapHierarchy(dump, d.screenWidth, d.screenHeight)
	if err != nil {
		return "", err
	}
	return string(mapped), nil
}

func (d *Driver) Screenshot(ctx context.Context) (driver.Image, error) {
	var data []byte
	err := d.withRecovery(ctx, func() error {
		var screenshotErr error
		data, _, screenshotErr = d.companion.Screenshot(ctx)
		return screenshotErr
	})
	if err != nil {
		return driver.Image{}, fmt.Errorf("screenshot: %w", err)
	}
	return decodeScreenshot(data)
}

func (d *Driver) Snapshot(ctx context.Context) (string, driver.Image, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// The hierarchy and the screenshot ride different transports on the
	// hybrid path, so they are captured concurrently. Only the hierarchy leg
	// runs under withRecovery: two concurrent recoveries would race the
	// restart bookkeeping, and a screenshot connection failure surfaces as a
	// plain error that the next serialized call recovers from. The goroutine
	// works through a captured local because a hierarchy-leg recovery
	// reassigns d.companion mid-flight; a screenshot against the torn-down
	// transport then fails as a plain error rather than racing the field.
	var data []byte
	screenshotDone := make(chan error, 1)
	companion := d.companion
	go func() {
		imageData, _, callErr := companion.Screenshot(ctx)
		data = imageData
		screenshotDone <- callErr
	}()

	dump, err := d.describeAll(ctx)
	screenshotErr := <-screenshotDone
	if err != nil {
		return "", driver.Image{}, err
	}
	if screenshotErr != nil {
		return "", driver.Image{}, fmt.Errorf("screenshot: %w", screenshotErr)
	}
	mapped, err := MapHierarchy(dump, d.screenWidth, d.screenHeight)
	if err != nil {
		return "", driver.Image{}, err
	}
	image, err := decodeScreenshot(data)
	if err != nil {
		return string(mapped), driver.Image{}, err
	}
	return string(mapped), image, nil
}

// WaitForIdle polls the hierarchy until it settles. The duration argument is
// ignored: the ported settle constants (StabilityPollCap and friends) own the
// cap, matching the companion's own settle behavior.
func (d *Driver) WaitForIdle(ctx context.Context, _ time.Duration) error {
	clock := d.idleClock
	if clock == nil {
		clock = SystemClock()
	}
	PollUntilStable(ctx, clock, func() *hierarchy.Tree {
		dump, err := d.describeAllRaw(ctx)
		if err != nil || dumpIsCollapsed(dump) {
			// A collapsed dump is the bridge mid-transition; report it
			// transitional so the streak resets and the poll waits for the
			// real tree rather than settling on the empty shell.
			return nil
		}
		mapped, err := MapHierarchy(dump, d.screenWidth, d.screenHeight)
		if err != nil {
			return nil
		}
		tree, err := hierarchy.Parse(string(mapped))
		if err != nil {
			return nil
		}
		return tree
	})
	return nil
}

// RecentLogs returns no entries: the companion log RPC is a follow-up, so v1
// reports an empty slice rather than failing.
func (d *Driver) RecentLogs(_ context.Context, _ time.Time, _ string) ([]driver.LogEntry, error) {
	return []driver.LogEntry{}, nil
}

func (d *Driver) Metrics(_ context.Context, _ string) (driver.Metrics, error) {
	return driver.Metrics{}, nil
}

func (d *Driver) Health(_ context.Context) (driver.Health, error) {
	return driver.Health{Ready: true, Platform: "ios"}, nil
}

// ForegroundApp reports the foreground app. It returns the app under test when
// it is running; otherwise it names another running user app, or "" when none
// is. The companion exposes process state but not a foreground flag, so "the
// app under test is running" stands in for "in the foreground".
func (d *Driver) ForegroundApp(ctx context.Context) (string, error) {
	var apps []transport.InstalledApp
	if err := d.withRecovery(ctx, func() error {
		var listErr error
		apps, listErr = d.companion.ListApps(ctx)
		return listErr
	}); err != nil {
		return "", err
	}
	other := ""
	for _, app := range apps {
		if app.ProcessState != transport.ProcessStateRunning {
			continue
		}
		if app.BundleID == d.bundleID {
			return d.bundleID, nil
		}
		if app.InstallType == "user" && other == "" {
			other = app.BundleID
		}
	}
	return other, nil
}

// collapsedDumpRetries and collapsedDumpDelay bound how long describeAll waits
// out a collapsed accessibility dump. The bridge briefly reports only the app
// shell (no UI content) during cold start and screen transitions; it recovers
// within a few hundred milliseconds. Re-fetching past the collapse keeps the
// runner from acting on, and snapshotting, an empty tree.
const collapsedDumpRetries = 6
const collapsedDumpDelay = 150 * time.Millisecond

// snapshotCompanion is the transport that serves accessibility dumps: the
// in-simulator runner when the hybrid is active (its snapshots never collapse),
// otherwise the legacy companion.
func (d *Driver) snapshotCompanion() transport.Companion {
	if d.runnerClient != nil {
		return d.runnerClient
	}
	return d.companion
}

// describeAllRaw fetches the flat accessibility dump with one-restart recovery
// and no collapse handling. The settle loop uses it: it treats a collapsed dump
// as transitional itself, so an inner retry here would double the wait.
func (d *Driver) describeAllRaw(ctx context.Context) ([]byte, error) {
	var dump []byte
	err := d.withRecovery(ctx, func() error {
		info, infoErr := d.snapshotCompanion().AccessibilityInfo(ctx)
		if infoErr != nil {
			return infoErr
		}
		dump = []byte(info)
		return nil
	})
	return dump, err
}

// describeAll fetches the flat accessibility dump, retrying past a transient
// collapsed dump so one-shot reads (Snapshot, Hierarchy) see real UI content.
func (d *Driver) describeAll(ctx context.Context) ([]byte, error) {
	dump, err := d.describeAllRaw(ctx)
	if err != nil {
		return dump, err
	}
	for attempt := 0; attempt < collapsedDumpRetries && dumpIsCollapsed(dump); attempt++ {
		select {
		case <-ctx.Done():
			return dump, nil
		case <-time.After(collapsedDumpDelay):
		}
		next, nextErr := d.describeAllRaw(ctx)
		if nextErr != nil {
			return dump, nil
		}
		dump = next
	}
	return dump, nil
}

// makeRunner builds the input runner backed by the current transport. The text
// runner does not route through withRecovery: it is invoked synchronously
// inside a single InputText call and a mid-paste connection drop surfaces as a
// normal error the runner retries.
func (d *Driver) makeRunner() runner {
	return simctlRunner{companion: d.companion, udid: d.udid}
}

// Close stops the companion and runner children and releases the transports.
func (d *Driver) Close() {
	if d.companion != nil {
		_ = d.companion.Close()
		d.companion = nil
	}
	d.stopChild()
	if d.runnerClient != nil {
		_ = d.runnerClient.Close()
		d.runnerClient = nil
	}
	d.stopRunnerChild()
	d.stopTunnel()
	if d.processCancel != nil {
		d.processCancel()
	}
	if d.deviceLock != nil {
		_ = d.deviceLock.Close()
		d.deviceLock = nil
	}
}

// stopTunnel closes the in-process usbmux forwarder on the device path. Closing
// its listener ends the accept loop and lets the open bridges drain; a nil
// tunnel (the simulator path) is a no-op.
func (d *Driver) stopTunnel() {
	tunnel := d.tunnel
	d.tunnel = nil
	if tunnel != nil {
		_ = tunnel.Close()
	}
}

// stopChild terminates the companion child gracefully (SIGTERM, grace window,
// then SIGKILL) so it leaves no orphan behind.
func (d *Driver) stopChild() {
	child := d.child
	d.child = nil
	stopProcess(child)
}

// stopRunnerChild terminates the runner's hosting session the same way. The
// session tears down its in-simulator children on SIGTERM; killing it outright
// would orphan them.
func (d *Driver) stopRunnerChild() {
	child := d.runnerChild
	d.runnerChild = nil
	stopProcess(child)
}

func stopProcess(child *exec.Cmd) {
	if child == nil || child.Process == nil {
		return
	}
	if err := child.Process.Signal(syscall.SIGTERM); err != nil {
		_ = child.Process.Kill()
		_ = child.Wait()
		return
	}
	done := make(chan struct{})
	go func() {
		_ = child.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(shutdownGrace):
		_ = child.Process.Kill()
		<-done
	}
}

// decodeScreenshot sniffs the PNG magic and decodes the pixel dimensions. The
// companion leaves image_format empty in practice, so the magic bytes are the
// only reliable format signal. No scaling is applied: the dimensions are pixels.
func decodeScreenshot(data []byte) (driver.Image, error) {
	if len(data) < 8 || !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		return driver.Image{}, errors.New("screenshot: response is not a PNG")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return driver.Image{}, fmt.Errorf("decode screenshot: %w", err)
	}
	return driver.Image{PNG: data, Width: config.Width, Height: config.Height}, nil
}

// realSpawnChild extracts the embedded companion and starts it on the given
// address. Cancel sends SIGTERM so the companion detaches cleanly from the
// simulator; WaitDelay bounds the grace before the runtime kills it.
func (d *Driver) realSpawnChild(ctx context.Context, address string) (*exec.Cmd, error) {
	extractDirectory := filepath.Join(os.TempDir(), "sanderling-companion")
	binaryPath, err := companionassets.Extract(extractDirectory)
	if err != nil {
		return nil, fmt.Errorf("extract companion: %w", err)
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, binaryPath, "--udid", d.udid, "--grpc-port", port)
	command.Stdout = d.output
	command.Stderr = d.output
	// The companion echoes its whole environment into the run log at startup,
	// so it gets a minimal one: secrets in the parent environment must never
	// reach run artifacts.
	command.Env = []string{
		"HOME=" + os.Getenv("HOME"),
		"PATH=/usr/bin:/bin",
		"TMPDIR=" + os.TempDir(),
	}
	command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
	command.WaitDelay = shutdownGrace
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start companion: %w", err)
	}
	fmt.Fprintf(d.output, "companion pid=%d listening on %s\n", command.Process.Pid, address)
	return command, nil
}

// pickLoopbackAddress reserves a free loopback port and returns its address.
func pickLoopbackAddress() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()
	return listener.Addr().String(), nil
}

// waitForListener blocks until address accepts a TCP connection or ctx expires.
func waitForListener(ctx context.Context, address string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		dialer := net.Dialer{Timeout: time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// ReplacesTextOnInput reports that InputText replaces the field's content, so
// the runner skips its pre-erase. The driver clears the field inside InputText,
// which is robust even when a collapsed accessibility bridge would make the
// runner read the field length as zero and wrongly skip erasing.
func (d *Driver) ReplacesTextOnInput() bool { return true }

var (
	_ driver.DeviceDriver      = (*Driver)(nil)
	_ driver.ForegroundChecker = (*Driver)(nil)
	_ driver.TextReplacer      = (*Driver)(nil)
)
