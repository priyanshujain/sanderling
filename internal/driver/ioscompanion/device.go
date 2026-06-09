// This file implements the physical-device mode of Driver. The device is driven
// runner-only: the in-device XCUITest runner serves every capability over a
// usbmux tunnel, with no legacy companion. The simulator hybrid path is left
// byte-identical; device mode swaps three sim-only seams (reinstall, container
// reset, paste grant) and brings the runner up over the tunnel instead of a
// local listener.
package ioscompanion

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/priyanshujain/sanderling/internal/driver/ioscompanion/transport"
)

// DeviceOptions configures a device-mode Driver. Signing credentials are not
// carried here: realSpawnDeviceRunner reads them from the environment at the
// point of use so secrets never reach the Options struct or run artifacts.
type DeviceOptions struct {
	// HardwareUDID feeds xcodebuild -destination and the usbmux device match.
	HardwareUDID string
	// CoreDeviceID feeds devicectl install/uninstall.
	CoreDeviceID string
	// BundleID is the app under test.
	BundleID string
	// AppPath is the .app bundle installed via devicectl for clear-state.
	AppPath string
	// Output receives the runner session log path and driver warnings.
	Output io.Writer
	// DoubleTapGapMilliseconds overrides the synthesized double-tap gap.
	DoubleTapGapMilliseconds float64

	// Test seams. Production leaves them nil and NewDevice wires the real
	// build/spawn/tunnel/dial.
	spawnRunner func(ctx context.Context, address string) (*exec.Cmd, error)
	startTunnel func(ctx context.Context, hardwareUDID, localAddress, devicePort string) (io.Closer, error)
	dialRunner  func(address string) (transport.Companion, error)
	pickAddress func() (string, error)
}

// deviceStartupTimeout bounds the runner's startup once its hosting test
// session is spawned and the tunnel is up. The session's cold start on a
// physical device is slower than the simulator's, and the build that precedes
// it runs outside this window (under the process context, not the startup one).
const deviceStartupTimeout = 180 * time.Second

// NewDevice brings up a runner-only Driver against a physical device: it builds
// and spawns the in-device runner, opens a usbmux tunnel to it, dials the runner
// over the tunnel, health-probes it, and caches the screen dimensions. Call
// Close when done to stop the runner session and the tunnel.
func NewDevice(ctx context.Context, options DeviceOptions) (*Driver, error) {
	if options.HardwareUDID == "" {
		return nil, errors.New("ios device: HardwareUDID is required")
	}
	if options.CoreDeviceID == "" {
		return nil, errors.New("ios device: CoreDeviceID is required")
	}
	output := options.Output
	if output == nil {
		output = io.Discard
	}
	gap := options.DoubleTapGapMilliseconds
	if gap <= 0 {
		gap = DefaultDoubleTapGapMilliseconds
	}

	d := &Driver{
		udid:                     options.HardwareUDID,
		coreDeviceID:             options.CoreDeviceID,
		bundleID:                 options.BundleID,
		appPath:                  options.AppPath,
		output:                   output,
		doubleTapGapMilliseconds: gap,
		deviceMode:               true,
		hybrid:                   false,
		spawnRunner:              options.spawnRunner,
		startTunnel:              options.startTunnel,
	}
	if d.spawnRunner == nil {
		d.spawnRunner = d.realSpawnDeviceRunner
	}
	if d.startTunnel == nil {
		d.startTunnel = startUsbmuxTunnel
	}
	d.dialRunner = options.dialRunner
	if d.dialRunner == nil {
		d.dialRunner = func(address string) (transport.Companion, error) {
			return transport.DialRunner(address, d.udid, d.bundleID)
		}
	}
	if options.pickAddress != nil {
		d.pickDeviceAddress = options.pickAddress
	} else {
		d.pickDeviceAddress = pickLoopbackAddress
	}

	// Device seams: clear-state reinstalls via devicectl; the container reset and
	// paste grant are simulator-only and become no-ops. The runner types
	// natively, so no paste prompt is ever hit.
	d.reinstallApp = d.devicectlReinstall
	d.resetContainer = d.deviceResetContainerUnsupported
	d.grantPaste = func(context.Context) error { return nil }
	d.restart = d.respawnDevice
	d.processContext, d.processCancel = context.WithCancel(ctx)

	if err := d.bringUpDevice(ctx); err != nil {
		d.Close()
		return nil, err
	}

	description, err := d.companion.Describe(ctx)
	if err != nil {
		d.Close()
		return nil, fmt.Errorf("describe device: %w", err)
	}
	d.screenWidth = description.WidthPoints
	d.screenHeight = description.HeightPoints
	return d, nil
}

// bringUpDevice builds (if needed) and spawns the in-device runner, opens the
// tunnel, waits for the forwarded listener, dials the runner, and confirms
// health. The build runs inside spawnRunner under the process context, so the
// startup timeout only bounds the post-spawn wait, not the build.
func (d *Driver) bringUpDevice(ctx context.Context) error {
	address, err := d.pickDeviceAddress()
	if err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	d.runnerAddress = address

	// The runner listens on the device loopback at the same port number the host
	// tunnel forwards from, so one picked free port covers both ends.
	runnerChild, err := d.spawnRunner(d.processContext, address)
	if err != nil {
		return fmt.Errorf("spawn device runner: %w", err)
	}
	d.runnerChild = runnerChild

	// The forwarder listens on the host loopback port and bridges to the same
	// port number on the device, where the runner listens.
	tunnel, err := d.startTunnel(d.processContext, d.udid, address, port)
	if err != nil {
		d.stopRunnerChild()
		return fmt.Errorf("start tunnel: %w", err)
	}
	d.tunnel = tunnel

	startupCtx, cancel := context.WithTimeout(ctx, deviceStartupTimeout)
	defer cancel()

	if err := waitForListener(startupCtx, address); err != nil {
		d.stopTunnel()
		d.stopRunnerChild()
		return fmt.Errorf("device runner listener: %w", err)
	}

	companion, err := d.dialRunner(address)
	if err != nil {
		d.stopTunnel()
		d.stopRunnerChild()
		return fmt.Errorf("dial device runner: %w", err)
	}
	d.companion = companion

	if err := d.waitForHealth(startupCtx); err != nil {
		_ = companion.Close()
		d.stopTunnel()
		d.stopRunnerChild()
		return fmt.Errorf("device runner health: %w", err)
	}
	return nil
}

// respawnDevice is the device-path supervision restart: it tears down the runner
// transport, its hosting session, and the tunnel, then brings a fresh set up.
// Both the session and the tunnel restart together because a dropped usbmux
// connection can take either down.
func (d *Driver) respawnDevice(ctx context.Context) error {
	if d.companion != nil {
		_ = d.companion.Close()
	}
	d.stopRunnerChild()
	d.stopTunnel()
	return d.bringUpDevice(ctx)
}

// devicectlReinstall uninstalls then installs the app bundle via devicectl,
// keyed on the CoreDevice id. App lifecycle stays with devicectl: the runner's
// own install path is simulator-specific.
func (d *Driver) devicectlReinstall(ctx context.Context) error {
	_ = exec.CommandContext(ctx, "xcrun", "devicectl", "device", "uninstall", "app", "--device", d.coreDeviceID, d.bundleID).Run()
	output, err := exec.CommandContext(ctx, "xcrun", "devicectl", "device", "install", "app", "--device", d.coreDeviceID, d.appPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("devicectl install: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// deviceResetContainerUnsupported warns once that device clear-state needs an
// app path for a devicectl reinstall: there is no simulator-style data-container
// wipe on a physical device.
func (d *Driver) deviceResetContainerUnsupported(context.Context) error {
	if !d.clearStateWarned {
		fmt.Fprintln(d.output, "clear-state on a physical device requires --ios-app-path for a reinstall; skipping (state not cleared)")
		d.clearStateWarned = true
	}
	return nil
}
