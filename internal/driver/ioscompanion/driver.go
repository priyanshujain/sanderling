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

// shutdownGrace bounds how long the companion child gets to exit after SIGTERM
// before it is killed.
const shutdownGrace = 15 * time.Second

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

	// processContext owns the companion child's lifetime: it is derived from
	// New's context (so a canceled run still reaps the child) and canceled by
	// Close. Spawning under a startup-scoped context would SIGTERM the child
	// the moment startup finishes.
	processContext context.Context
	processCancel  context.CancelFunc
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
	}
	if driverInstance.spawnChild == nil {
		driverInstance.spawnChild = driverInstance.realSpawnChild
	}
	if driverInstance.dial == nil {
		driverInstance.dial = transport.Dial
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

	if err := driverInstance.bringUp(ctx); err != nil {
		return nil, err
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

// respawnAndRedial tears down the current transport and child, then brings a
// fresh pair up at the same address. Used as the supervision restart.
func (d *Driver) respawnAndRedial(ctx context.Context) error {
	if d.companion != nil {
		_ = d.companion.Close()
		d.companion = nil
	}
	d.stopChild()
	return d.bringUp(ctx)
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
	if restartErr := d.restart(ctx); restartErr != nil {
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
	_ = d.withRecovery(ctx, func() error { return d.companion.Terminate(ctx, d.bundleID) })

	if clearState {
		if err := d.clearAppState(ctx); err != nil {
			return err
		}
	}

	// Grant the app pasteboard access before it runs so unicode input (which
	// must go through the pasteboard, since HID cannot express it) never trips
	// the iOS paste-permission prompt. clearState reinstall resets the grant,
	// so it is reapplied on every launch. Best effort: if it fails, the paste
	// path still handles the prompt, just slower.
	if d.bundleID != "" {
		if err := d.grantPaste(ctx); err != nil {
			fmt.Fprintf(d.output, "grant pasteboard access failed (continuing): %v\n", err)
		}
	}

	if err := d.withRecovery(ctx, func() error {
		return d.companion.Launch(ctx, d.bundleID, true)
	}); err != nil {
		return fmt.Errorf("launch %s: %w", d.bundleID, err)
	}
	return nil
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
	return d.withRecovery(ctx, func() error { return d.companion.Terminate(ctx, d.bundleID) })
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
	dump, err := d.describeAll(ctx)
	if err != nil {
		return "", driver.Image{}, err
	}
	var data []byte
	if err := d.withRecovery(ctx, func() error {
		var screenshotErr error
		data, _, screenshotErr = d.companion.Screenshot(ctx)
		return screenshotErr
	}); err != nil {
		return "", driver.Image{}, fmt.Errorf("screenshot: %w", err)
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

// describeAllRaw fetches the flat accessibility dump with one-restart recovery
// and no collapse handling. The settle loop uses it: it treats a collapsed dump
// as transitional itself, so an inner retry here would double the wait.
func (d *Driver) describeAllRaw(ctx context.Context) ([]byte, error) {
	var dump []byte
	err := d.withRecovery(ctx, func() error {
		info, infoErr := d.companion.AccessibilityInfo(ctx)
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

// Close stops the companion child and releases the transport.
func (d *Driver) Close() {
	if d.companion != nil {
		_ = d.companion.Close()
		d.companion = nil
	}
	d.stopChild()
	if d.processCancel != nil {
		d.processCancel()
	}
}

// stopChild terminates the companion child gracefully (SIGTERM, grace window,
// then SIGKILL) so it leaves no orphan behind.
func (d *Driver) stopChild() {
	child := d.child
	d.child = nil
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
