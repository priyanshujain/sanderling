package testrun

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/priyanshujain/sanderling/internal/android"
)

// Preflight runs platform-specific host checks before sidecar/driver setup.
// On failure it returns a wrapped error pointing the user at the matching
// `sanderling doctor --platform=<p>` command. Web returns nil (no host
// prerequisites beyond a working chromium, which the driver will surface
// itself if missing).
func Preflight(ctx context.Context, platform string) error {
	check := preflightCheck
	return runPreflight(ctx, platform, check)
}

type preflightFunc func(name string) error

// preflightCheck resolves adb through the same helper every adb call in a run
// uses, so a host whose SDK is only reachable through $ANDROID_HOME or a
// standard install location is not turned away here and then driven fine by
// the rest of the pipeline.
func preflightCheck(name string) error {
	if name == "adb" {
		_, err := android.AdbBinary()
		return err
	}
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%s not found on PATH: %w", name, err)
	}
	return nil
}

func runPreflight(ctx context.Context, platform string, check preflightFunc) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch platform {
	case "web":
		return nil
	case "android":
		if err := check("adb"); err != nil {
			return preflightFailure("android", err)
		}
		if err := check("java"); err != nil {
			return preflightFailure("android", err)
		}
		return nil
	case "ios":
		// Neither iOS path needs a JVM: the simulator is driven by the native
		// companion and a physical device runner-only over usbmux.
		if err := check("xcrun"); err != nil {
			return preflightFailure("ios", err)
		}
		return nil
	default:
		return fmt.Errorf("preflight: unknown platform %q", platform)
	}
}

func preflightFailure(platform string, cause error) error {
	return fmt.Errorf(
		"preflight: %w\nrun `sanderling doctor --platform=%s` for full host-readiness checks",
		cause, platform,
	)
}
