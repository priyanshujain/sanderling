package testrun

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/priyanshujain/sanderling/internal/android"
	"github.com/priyanshujain/sanderling/internal/driver"
	"github.com/priyanshujain/sanderling/internal/driver/chrome"
	"github.com/priyanshujain/sanderling/internal/driver/ioscompanion"
	driverSidecar "github.com/priyanshujain/sanderling/internal/driver/sidecar"
	"github.com/priyanshujain/sanderling/internal/ios"
	"github.com/priyanshujain/sanderling/internal/sidecarassets"
)

// iOS resolution seams: package-level so routing tests substitute canned
// resolvers instead of shelling out to xcrun.
var (
	iosResolveTarget   = ios.ResolveTarget
	iosResolveDevice   = ios.ResolveDevice
	iosEnsureSimulator = ios.EnsureSimulator
)

// resolveIOSTarget decides whether a run drives a simulator or a physical
// device and fills the iOS fields on Options. A simulator target is booted if
// needed; a physical-device target is resolved to its hardware UDID and
// CoreDevice id via devicectl.
func resolveIOSTarget(ctx context.Context, options Options, stdout io.Writer) (Options, error) {
	udid, isSimulator, err := iosResolveTarget(ctx, options.IosDevice)
	if err != nil {
		// No query and nothing booted: keep boot-first behavior, then resolve
		// the simulator that EnsureSimulator just brought up.
		if options.IosDevice == "" {
			if bootErr := iosEnsureSimulator(ctx, "", stdout); bootErr != nil {
				return options, bootErr
			}
			udid, isSimulator, err = iosResolveTarget(ctx, "")
		}
		if err != nil {
			return options, err
		}
	} else if isSimulator {
		if err := iosEnsureSimulator(ctx, options.IosDevice, stdout); err != nil {
			return options, err
		}
		udid, isSimulator, err = iosResolveTarget(ctx, options.IosDevice)
		if err != nil {
			return options, err
		}
	}
	options.iosUDID = udid
	options.iosIsSimulator = isSimulator
	if !isSimulator {
		device, err := iosResolveDevice(ctx, options.IosDevice)
		if err != nil {
			return options, err
		}
		options.iosUDID = device.HardwareUDID
		options.iosCoreDeviceID = device.CoreDeviceID
		fmt.Fprintf(stdout, "using device: %s (udid %s, id %s)\n", device.Name, device.HardwareUDID, device.CoreDeviceID)
	}
	return options, nil
}

// preflight is a seam so routing tests exercise driver construction without the
// host-readiness checks (xcrun, java) that are absent on a Linux CI runner.
var preflight = Preflight

// newDeviceDriver constructs the physical-device iOS driver and its cleanup. A
// seam so routing tests assert the resolved identifiers reach DeviceOptions
// without building or spawning a real runner.
var newDeviceDriver = func(ctx context.Context, options ioscompanion.DeviceOptions) (driver.DeviceDriver, func(), error) {
	d, err := ioscompanion.NewDevice(ctx, options)
	if err != nil {
		return nil, nil, err
	}
	return d, d.Close, nil
}

// newSimulatorDriver constructs the iOS simulator driver and its cleanup. A
// seam so routing tests assert the run's options reach ioscompanion.Options
// without spawning a companion.
var newSimulatorDriver = func(ctx context.Context, options ioscompanion.Options) (driver.DeviceDriver, func(), error) {
	d, err := ioscompanion.New(ctx, options)
	if err != nil {
		return nil, nil, err
	}
	return d, d.Close, nil
}

// buildDriver creates the appropriate DeviceDriver for the platform and returns
// a cleanup function. For web, ChromeDriver is used directly. An iOS simulator
// is driven by the native simulator companion (no JVM). A physical iOS device
// is driven runner-only over a usbmux tunnel. Android uses the JVM sidecar,
// which is extracted, spawned, and dialed.
func buildDriver(ctx context.Context, options Options, stdout io.Writer) (driver.DeviceDriver, func(), error) {
	if err := preflight(ctx, options.Platform); err != nil {
		return nil, nil, err
	}
	if options.Platform == "web" {
		d := chrome.New()
		return d, func() { _ = d.Terminate(context.Background()) }, nil
	}

	if options.Platform == "ios" && options.iosIsSimulator {
		d, cleanup, err := newSimulatorDriver(ctx, ioscompanion.Options{
			UniqueDeviceIdentifier: options.iosUDID,
			BundleID:               options.BundleID,
			AppPath:                options.IosAppPath,
			ClearState:             options.ClearData,
			Output:                 stdout,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("ios simulator driver: %w", err)
		}
		return d, cleanup, nil
	}

	if options.Platform == "ios" {
		d, cleanup, err := newDeviceDriver(ctx, ioscompanion.DeviceOptions{
			HardwareUDID: options.iosUDID,
			CoreDeviceID: options.iosCoreDeviceID,
			BundleID:     options.BundleID,
			AppPath:      options.IosAppPath,
			ClearState:   options.ClearData,
			Output:       stdout,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("ios device driver: %w", err)
		}
		return d, cleanup, nil
	}

	sidecarDirectory := os.TempDir() + "/sanderling-sidecar"
	jarPath, err := sidecarassets.Extract(sidecarDirectory)
	if err != nil {
		return nil, nil, fmt.Errorf("extract sidecar: %w", err)
	}
	fmt.Fprintf(stdout, "sidecar JAR: %s (size=%d)\n", jarPath, sidecarassets.EmbeddedSize())

	sidecarPort, err := pickFreePort()
	if err != nil {
		return nil, nil, err
	}
	sidecarArgs := []string{"-jar", jarPath,
		"--port", strconv.Itoa(sidecarPort),
		"--platform", options.Platform,
	}
	if options.Device != "" {
		sidecarArgs = append(sidecarArgs, "--serial", options.Device)
	}
	adbPath, err := android.AdbBinary()
	if err != nil {
		return nil, nil, preflightFailure("android", err)
	}
	sidecarCommand := exec.CommandContext(ctx, "java", sidecarArgs...)
	sidecarCommand.Stdout = stdout
	sidecarCommand.Stderr = stdout
	sidecarCommand.Env = android.EnvWithAndroidPlatformTools(os.Environ(), adbPath)
	// SIGTERM lets the sidecar run its shutdown hook. SIGKILL skips it and
	// leaves the adb connection and the device-side instrumentation behind.
	sidecarCommand.Cancel = func() error {
		return sidecarCommand.Process.Signal(syscall.SIGTERM)
	}
	sidecarCommand.WaitDelay = sidecarShutdownGrace
	if err := sidecarCommand.Start(); err != nil {
		return nil, nil, fmt.Errorf("spawn sidecar: %w", err)
	}
	sidecarExited := watchSidecar(sidecarCommand)
	address := fmt.Sprintf("127.0.0.1:%d", sidecarPort)
	fmt.Fprintf(stdout, "sidecar pid=%d listening on %s (adb: %s)\n", sidecarCommand.Process.Pid, address, adbPath)

	driverClient, err := driverSidecar.Dial(address)
	if err != nil {
		stopSidecar(sidecarCommand, sidecarExited)
		return nil, nil, fmt.Errorf("dial sidecar: %w", err)
	}
	driverClient.SetPlatform(options.Platform)
	driverClient.SetClearStateReinstall(options.Device, options.AndroidAppPath, stdout)
	// WaitForHealth confirms the gRPC sidecar is up. This path is Android
	// only: iOS has not routed through the sidecar since the native companion
	// replaced it, and buildDriver returns before reaching here.
	healthCtx, healthCancel := context.WithTimeout(ctx, sidecarStartupTimeout)
	healthErr := awaitSidecar(healthCtx, address, sidecarStartupTimeout, func(pollCtx context.Context) error {
		return driverClient.WaitForHealth(pollCtx, 250e6)
	}, sidecarExited)
	healthCancel()
	if healthErr != nil {
		stopSidecar(sidecarCommand, sidecarExited)
		_ = driverClient.Close()
		return nil, nil, healthErr
	}
	fmt.Fprintln(stdout, "sidecar is healthy")

	cleanup := func() {
		_ = driverClient.Close()
		stopSidecar(sidecarCommand, sidecarExited)
	}
	return driverClient, cleanup, nil
}

// watchSidecar reaps the sidecar and publishes its exit status. The channel is
// closed after the send so the shutdown path can still receive once the startup
// path has taken the status.
func watchSidecar(sidecarCommand *exec.Cmd) <-chan error {
	exited := make(chan error, 1)
	go func() {
		exited <- sidecarCommand.Wait()
		close(exited)
	}()
	return exited
}

// awaitSidecar waits for the sidecar to answer a health check, racing that
// against the process exiting so a sidecar that dies during startup is reported
// as the exit it was rather than as a deadline half a minute later. Neither
// failure knows why the sidecar was unhappy, so both name what to look at
// instead of picking a cause.
func awaitSidecar(
	ctx context.Context,
	address string,
	timeout time.Duration,
	health func(context.Context) error,
	exited <-chan error,
) error {
	healthy := make(chan error, 1)
	go func() { healthy <- health(ctx) }()
	select {
	case exitErr := <-exited:
		return sidecarExitedError(address, exitErr)
	case err := <-healthy:
		if err == nil {
			return nil
		}
		select {
		case exitErr := <-exited:
			return sidecarExitedError(address, exitErr)
		default:
			return fmt.Errorf(
				"sidecar did not answer a health check on %s within %s and is still running\n%s",
				address, timeout, sidecarWhatToCheck,
			)
		}
	}
}

const sidecarWhatToCheck = "check the sidecar output above, then `sanderling doctor --platform=android` (java 17+, adb, Android SDK)"

func sidecarExitedError(address string, exitErr error) error {
	status := "exit status 0"
	if exitErr != nil {
		status = exitErr.Error()
	}
	return fmt.Errorf(
		"sidecar exited before it answered a health check on %s: %s\n%s",
		address, status, sidecarWhatToCheck,
	)
}

// sidecarShutdownGrace bounds how long the sidecar gets to run its shutdown
// hook (terminate the app, stop the XCTest runner) before being killed.
const sidecarShutdownGrace = 15 * time.Second

// stopSidecar terminates the sidecar gracefully so its shutdown hook can stop
// the device-side runner processes, escalating to SIGKILL when it does not
// exit within the grace window.
func stopSidecar(sidecarCommand *exec.Cmd, exited <-chan error) {
	if sidecarCommand.Process == nil {
		return
	}
	if err := sidecarCommand.Process.Signal(syscall.SIGTERM); err != nil {
		_ = sidecarCommand.Process.Kill()
		<-exited
		return
	}
	select {
	case <-exited:
	case <-time.After(sidecarShutdownGrace):
		_ = sidecarCommand.Process.Kill()
		<-exited
	}
}

func pickFreePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}
