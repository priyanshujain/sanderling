package testrun

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/driver"
	"github.com/priyanshujain/sanderling/internal/driver/ioscompanion"
	"github.com/priyanshujain/sanderling/internal/ios"
)

// stubDeviceDriver is a no-op DeviceDriver so routing tests never build or spawn
// a real runner. Only construction wiring is under test.
type stubDeviceDriver struct{ driver.DeviceDriver }

func TestBuildDriverRoutesPhysicalIOSToDeviceDriver(t *testing.T) {
	stubPreflight(t)
	original := newDeviceDriver
	t.Cleanup(func() { newDeviceDriver = original })

	var got ioscompanion.DeviceOptions
	closed := false
	newDeviceDriver = func(_ context.Context, options ioscompanion.DeviceOptions) (driver.DeviceDriver, func(), error) {
		got = options
		return stubDeviceDriver{}, func() { closed = true }, nil
	}

	options := Options{Platform: "ios", BundleID: "app.folio", IosAppPath: "/tmp/iosApp.app"}
	options.iosIsSimulator = false
	options.iosUDID = "00008140-HW"
	options.iosCoreDeviceID = "CORE-1"

	d, cleanup, err := buildDriver(context.Background(), options, io.Discard)
	if err != nil {
		t.Fatalf("buildDriver: %v", err)
	}
	if d == nil {
		t.Fatal("expected a device driver")
	}
	if got.HardwareUDID != "00008140-HW" || got.CoreDeviceID != "CORE-1" {
		t.Fatalf("DeviceOptions ids = %+v, want the resolved hardware/core ids", got)
	}
	if got.BundleID != "app.folio" || got.AppPath != "/tmp/iosApp.app" {
		t.Fatalf("DeviceOptions = %+v, want bundle and app path threaded through", got)
	}
	cleanup()
	if !closed {
		t.Fatal("cleanup must close the device driver")
	}
}

func TestBuildDriverSurfacesDeviceConstructionError(t *testing.T) {
	stubPreflight(t)
	original := newDeviceDriver
	t.Cleanup(func() { newDeviceDriver = original })
	newDeviceDriver = func(context.Context, ioscompanion.DeviceOptions) (driver.DeviceDriver, func(), error) {
		return nil, nil, errors.New("no signing creds")
	}
	options := Options{Platform: "ios"}
	options.iosIsSimulator = false
	if _, _, err := buildDriver(context.Background(), options, io.Discard); err == nil {
		t.Fatal("expected the device construction error to surface")
	}
}

// A sidecar that dies during startup leaves the health poll with nothing to
// talk to, and reporting that as a deadline sends the reader after a gRPC
// timeout instead of the exit that already happened.
func TestAwaitSidecarReportsTheExitItSaw(t *testing.T) {
	exited := make(chan error, 1)
	exited <- errors.New("exit status 1")
	close(exited)

	err := awaitSidecar(
		context.Background(),
		"127.0.0.1:54321",
		30*time.Second,
		func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() },
		exited,
	)
	if err == nil {
		t.Fatal("expected an error when the sidecar exits before it is healthy")
	}
	for _, want := range []string{
		"sidecar exited before it answered a health check on 127.0.0.1:54321: exit status 1",
		"check the sidecar output above",
		"sanderling doctor --platform=android",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestAwaitSidecarTimeoutSaysOnlyWhatItObserved(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := awaitSidecar(
		ctx,
		"127.0.0.1:54321",
		50*time.Millisecond,
		func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() },
		make(chan error, 1),
	)
	if err == nil {
		t.Fatal("expected an error when the sidecar never answers")
	}
	for _, want := range []string{
		"sidecar did not answer a health check on 127.0.0.1:54321 within 50ms and is still running",
		"check the sidecar output above",
		"sanderling doctor --platform=android",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("error %q must not hand the reader a bare gRPC deadline", err)
	}
}

func TestAwaitSidecarHealthyReturnsNil(t *testing.T) {
	err := awaitSidecar(
		context.Background(),
		"127.0.0.1:54321",
		30*time.Second,
		func(context.Context) error { return nil },
		make(chan error, 1),
	)
	if err != nil {
		t.Fatalf("expected a healthy sidecar to pass, got %v", err)
	}
}

// stubPreflight bypasses the host-readiness checks so routing tests exercise
// driver construction on a Linux CI runner that lacks xcrun/java.
func stubPreflight(t *testing.T) {
	t.Helper()
	original := preflight
	t.Cleanup(func() { preflight = original })
	preflight = func(context.Context, string) error { return nil }
}

func swapIOSResolveSeams(t *testing.T) {
	t.Helper()
	origTarget, origDevice, origEnsure := iosResolveTarget, iosResolveDevice, iosEnsureSimulator
	t.Cleanup(func() {
		iosResolveTarget, iosResolveDevice, iosEnsureSimulator = origTarget, origDevice, origEnsure
	})
}

func TestResolveIOSTargetPhysicalDeviceFillsIDs(t *testing.T) {
	swapIOSResolveSeams(t)
	iosResolveTarget = func(_ context.Context, query string) (string, bool, error) {
		return query, false, nil
	}
	resolveDeviceCalled := ""
	iosResolveDevice = func(_ context.Context, query string) (ios.Device, error) {
		resolveDeviceCalled = query
		return ios.Device{Name: "iPhone", HardwareUDID: "00008140-HW", CoreDeviceID: "CORE-1"}, nil
	}
	iosEnsureSimulator = func(context.Context, string, io.Writer) error {
		t.Fatal("must not boot a simulator on the device path")
		return nil
	}

	options := Options{Platform: "ios", IosDevice: "iPhone"}
	resolved, err := resolveIOSTarget(context.Background(), options, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if resolveDeviceCalled != "iPhone" {
		t.Fatalf("ResolveDevice query = %q, want iPhone", resolveDeviceCalled)
	}
	if resolved.iosIsSimulator {
		t.Fatal("device target must not be marked a simulator")
	}
	if resolved.iosUDID != "00008140-HW" || resolved.iosCoreDeviceID != "CORE-1" {
		t.Fatalf("resolved ids = (%q, %q), want hardware/core ids", resolved.iosUDID, resolved.iosCoreDeviceID)
	}
}

func TestResolveIOSTargetSimulatorSkipsDeviceResolution(t *testing.T) {
	swapIOSResolveSeams(t)
	iosResolveTarget = func(context.Context, string) (string, bool, error) {
		return "sim-udid", true, nil
	}
	ensured := false
	iosEnsureSimulator = func(context.Context, string, io.Writer) error { ensured = true; return nil }
	iosResolveDevice = func(context.Context, string) (ios.Device, error) {
		t.Fatal("simulator path must not resolve a physical device")
		return ios.Device{}, nil
	}

	options := Options{Platform: "ios", IosDevice: "iPhone 17 Pro"}
	resolved, err := resolveIOSTarget(context.Background(), options, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !ensured {
		t.Fatal("simulator path must ensure the simulator is booted")
	}
	if !resolved.iosIsSimulator || resolved.iosUDID != "sim-udid" {
		t.Fatalf("resolved = %+v, want the booted simulator", resolved)
	}
}

func TestResolveIOSTargetDeviceResolutionErrorSurfaces(t *testing.T) {
	swapIOSResolveSeams(t)
	iosResolveTarget = func(_ context.Context, query string) (string, bool, error) { return query, false, nil }
	iosResolveDevice = func(context.Context, string) (ios.Device, error) {
		return ios.Device{}, errors.New("no device connected")
	}
	options := Options{Platform: "ios", IosDevice: "iPhone"}
	if _, err := resolveIOSTarget(context.Background(), options, io.Discard); err == nil {
		t.Fatal("expected the device-resolution error to surface")
	}
}
