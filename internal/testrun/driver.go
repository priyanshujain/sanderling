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
	"github.com/priyanshujain/sanderling/internal/sidecarassets"
)

// buildDriver creates the appropriate DeviceDriver for the platform and returns
// a cleanup function. For web, ChromeDriver is used directly. An iOS simulator
// is driven by the native simulator companion (no JVM). Android uses the JVM
// sidecar, which is extracted, spawned, and dialed. Physical iOS devices are
// not yet supported.
func buildDriver(ctx context.Context, options Options, stdout io.Writer) (driver.DeviceDriver, func(), error) {
	if err := Preflight(ctx, options.Platform); err != nil {
		return nil, nil, err
	}
	if options.Platform == "web" {
		d := chrome.New()
		return d, func() { _ = d.Terminate(context.Background()) }, nil
	}

	if options.Platform == "ios" && options.iosIsSimulator {
		d, err := ioscompanion.New(ctx, ioscompanion.Options{
			UniqueDeviceIdentifier: options.iosUDID,
			BundleID:               options.BundleID,
			AppPath:                options.IosAppPath,
			Output:                 stdout,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("ios simulator driver: %w", err)
		}
		return d, d.Close, nil
	}

	if options.Platform == "ios" {
		return nil, nil, fmt.Errorf("physical-device iOS is not yet supported; run against a simulator instead")
	}

	// Android uses the JVM sidecar, which requires java.
	if err := preflightDevice(options.Platform); err != nil {
		return nil, nil, err
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
	sidecarCommand := exec.CommandContext(ctx, "java", sidecarArgs...)
	sidecarCommand.Stdout = stdout
	sidecarCommand.Stderr = stdout
	sidecarCommand.Env = android.EnvWithAndroidPlatformTools(os.Environ())
	// SIGTERM lets the sidecar's shutdown hook stop the iOS XCTest runner.
	// SIGKILL skips the hook and orphans an xcodebuild session that later
	// restarts its runner and hijacks the simulator mid-run.
	sidecarCommand.Cancel = func() error {
		return sidecarCommand.Process.Signal(syscall.SIGTERM)
	}
	sidecarCommand.WaitDelay = sidecarShutdownGrace
	if err := sidecarCommand.Start(); err != nil {
		return nil, nil, fmt.Errorf("spawn sidecar: %w", err)
	}
	fmt.Fprintf(stdout, "sidecar pid=%d listening on 127.0.0.1:%d\n", sidecarCommand.Process.Pid, sidecarPort)

	driverClient, err := driverSidecar.Dial(fmt.Sprintf("127.0.0.1:%d", sidecarPort))
	if err != nil {
		stopSidecar(sidecarCommand)
		return nil, nil, fmt.Errorf("dial sidecar: %w", err)
	}
	driverClient.SetPlatform(options.Platform)
	// WaitForHealth confirms the gRPC sidecar is up. For iOS, the WDA warmup
	// (absorbing the XCUITest startup race) runs inside IosDriverBackend.init
	// in the sidecar - no additional sleep needed here.
	healthCtx, healthCancel := context.WithTimeout(ctx, sidecarStartupTimeout)
	if err := driverClient.WaitForHealth(healthCtx, 250e6); err != nil {
		healthCancel()
		stopSidecar(sidecarCommand)
		_ = driverClient.Close()
		return nil, nil, fmt.Errorf("sidecar health check: %w", err)
	}
	healthCancel()
	fmt.Fprintln(stdout, "sidecar is healthy")

	cleanup := func() {
		_ = driverClient.Close()
		stopSidecar(sidecarCommand)
	}
	return driverClient, cleanup, nil
}

// sidecarShutdownGrace bounds how long the sidecar gets to run its shutdown
// hook (terminate the app, stop the XCTest runner) before being killed.
const sidecarShutdownGrace = 15 * time.Second

// stopSidecar terminates the sidecar gracefully so its shutdown hook can stop
// the device-side runner processes, escalating to SIGKILL when it does not
// exit within the grace window.
func stopSidecar(sidecarCommand *exec.Cmd) {
	if sidecarCommand.Process == nil {
		return
	}
	if err := sidecarCommand.Process.Signal(syscall.SIGTERM); err != nil {
		_ = sidecarCommand.Process.Kill()
		_ = sidecarCommand.Wait()
		return
	}
	done := make(chan struct{})
	go func() {
		_ = sidecarCommand.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(sidecarShutdownGrace):
		_ = sidecarCommand.Process.Kill()
		<-done
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
