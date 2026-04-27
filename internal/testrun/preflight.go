package testrun

import (
	"context"
	"fmt"
	"os/exec"
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

func preflightCheck(name string) error {
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
		if err := check("xcrun"); err != nil {
			return preflightFailure("ios", err)
		}
		if err := check("java"); err != nil {
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
