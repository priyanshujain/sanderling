package testrun

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"

	"github.com/priyanshujain/sanderling/internal/android"
	"github.com/priyanshujain/sanderling/internal/driver"
	"github.com/priyanshujain/sanderling/internal/driver/chrome"
	driverSidecar "github.com/priyanshujain/sanderling/internal/driver/sidecar"
	"github.com/priyanshujain/sanderling/internal/ios"
	"github.com/priyanshujain/sanderling/internal/sidecar"
)

// buildDriver creates the appropriate DeviceDriver for the platform and returns
// a cleanup function. For web, ChromeDriver is used directly; for android/ios
// the JVM sidecar is extracted, spawned, and dialed.
func buildDriver(ctx context.Context, options Options, stdout io.Writer) (driver.DeviceDriver, func(), error) {
	if options.Platform == "web" {
		d := chrome.New()
		return d, func() { _ = d.Terminate(context.Background()) }, nil
	}

	sidecarDirectory := os.TempDir() + "/sanderling-sidecar"
	jarPath, err := sidecar.Extract(sidecarDirectory)
	if err != nil {
		return nil, nil, fmt.Errorf("extract sidecar: %w", err)
	}
	fmt.Fprintf(stdout, "sidecar JAR: %s (size=%d)\n", jarPath, sidecar.EmbeddedSize())

	sidecarPort, err := pickFreePort()
	if err != nil {
		return nil, nil, err
	}
	sidecarArgs := []string{"-jar", jarPath,
		"--port", strconv.Itoa(sidecarPort),
		"--platform", options.Platform,
	}
	if options.Platform == "ios" {
		if udid := ios.BootedUDID(ctx); udid != "" {
			sidecarArgs = append(sidecarArgs, "--udid", udid)
		}
	}
	sidecarCommand := exec.CommandContext(ctx, "java", sidecarArgs...)
	sidecarCommand.Stdout = stdout
	sidecarCommand.Stderr = stdout
	sidecarCommand.Env = android.EnvWithAndroidPlatformTools(os.Environ())
	if err := sidecarCommand.Start(); err != nil {
		return nil, nil, fmt.Errorf("spawn sidecar: %w", err)
	}
	fmt.Fprintf(stdout, "sidecar pid=%d listening on 127.0.0.1:%d\n", sidecarCommand.Process.Pid, sidecarPort)

	driverClient, err := driverSidecar.Dial(fmt.Sprintf("127.0.0.1:%d", sidecarPort))
	if err != nil {
		_ = sidecarCommand.Process.Kill()
		return nil, nil, fmt.Errorf("dial sidecar: %w", err)
	}
	// WaitForHealth confirms the gRPC sidecar is up. For iOS, the WDA warmup
	// (absorbing the XCUITest startup race) runs inside IosDriverBackend.init
	// in the sidecar - no additional sleep needed here.
	healthCtx, healthCancel := context.WithTimeout(ctx, sidecarStartupTimeout)
	if err := driverClient.WaitForHealth(healthCtx, 250e6); err != nil {
		healthCancel()
		_ = sidecarCommand.Process.Kill()
		_ = driverClient.Close()
		return nil, nil, fmt.Errorf("sidecar health check: %w", err)
	}
	healthCancel()
	fmt.Fprintln(stdout, "sidecar is healthy")

	cleanup := func() {
		_ = driverClient.Close()
		if sidecarCommand.Process != nil {
			_ = sidecarCommand.Process.Kill()
		}
	}
	return driverClient, cleanup, nil
}

func pickFreePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}
