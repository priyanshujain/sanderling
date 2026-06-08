// Package testrun wires together the device, bundler, runner, and verifier into a single test pipeline.
package testrun

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/priyanshujain/sanderling/internal/android"
	"github.com/priyanshujain/sanderling/internal/bundler"
	"github.com/priyanshujain/sanderling/internal/driver"
	"github.com/priyanshujain/sanderling/internal/ios"
	"github.com/priyanshujain/sanderling/internal/runner"
	"github.com/priyanshujain/sanderling/internal/trace"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

const sidecarStartupTimeout = 30 * time.Second

// Options are the parameters for a single test pipeline run.
type Options struct {
	Spec       string
	BundleID   string
	Platform   string
	AVD        string
	IosDevice  string
	IosAppPath string
	Duration   time.Duration
	Seed       int64
	Output     string
	ClearData  bool

	// iosUDID and iosIsSimulator are filled by Execute after resolving the iOS
	// target, then read by buildDriver to choose the companion or sidecar path.
	iosUDID        string
	iosIsSimulator bool
}

// Execute runs the full test pipeline: bundle, launch app, verify properties.
func Execute(ctx context.Context, options Options, stdout io.Writer) error {
	switch options.Platform {
	case "android":
		if err := android.EnsureDevice(ctx, options.AVD, stdout); err != nil {
			return err
		}
	case "ios":
		udid, isSimulator, err := ios.ResolveTarget(ctx, options.IosDevice)
		if err != nil {
			// No query and nothing booted: keep boot-first behavior, then resolve
			// the simulator that EnsureSimulator just brought up.
			if options.IosDevice == "" {
				if bootErr := ios.EnsureSimulator(ctx, "", stdout); bootErr != nil {
					return bootErr
				}
				udid, isSimulator, err = ios.ResolveTarget(ctx, "")
			}
			if err != nil {
				return err
			}
		} else if isSimulator {
			if err := ios.EnsureSimulator(ctx, options.IosDevice, stdout); err != nil {
				return err
			}
			udid, isSimulator, err = ios.ResolveTarget(ctx, options.IosDevice)
			if err != nil {
				return err
			}
		}
		options.iosUDID = udid
		options.iosIsSimulator = isSimulator
	}
	prep, err := prepareBundleInputs(options)
	if err != nil {
		return err
	}
	aliases := prep.aliases
	seed := prep.seed
	defines := prep.defines
	specAPIPath := prep.specAPIPath
	bundle, err := bundler.Bundle(bundler.Options{
		EntryFile:   options.Spec,
		RuntimeFile: prep.gojaRuntimePath,
		Defines:     defines,
		Aliases:     aliases,
	})
	if err != nil {
		return fmt.Errorf("bundle spec: %w", err)
	}
	fmt.Fprintf(stdout, "bundled spec: %d bytes (sha256=%s)\n", len(bundle.JavaScript), bundle.SHA256[:12])

	var webBundle bundler.Result
	if options.Platform == "web" {
		runtimePath := resolveWebRuntimePath(specAPIPath, options.Spec)
		if runtimePath == "" {
			return fmt.Errorf("web-runtime.ts not found near %s; checkout pkg/spec or set @sanderling/spec alias", options.Spec)
		}
		webBundle, err = bundler.BundleWeb(bundler.WebOptions{
			EntryFile:      options.Spec,
			WebRuntimeFile: runtimePath,
			Defines:        defines,
			Aliases:        aliases,
		})
		if err != nil {
			return fmt.Errorf("bundle web spec: %w", err)
		}
		fmt.Fprintf(stdout, "bundled web spec: %d bytes (sha256=%s)\n", len(webBundle.JavaScript), webBundle.SHA256[:12])
	}

	activeDriver, cleanup, err := buildDriver(ctx, options, stdout)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := activeDriver.Launch(ctx, options.BundleID, options.ClearData, nil); err != nil {
		return fmt.Errorf("launch app: %w", err)
	}

	if web, ok := activeDriver.(driver.WebDriver); ok && len(webBundle.JavaScript) > 0 {
		if err := web.InstallBundle(ctx, webBundle.JavaScript); err != nil {
			return fmt.Errorf("install web bundle: %w", err)
		}
	}

	verifierInstance, err := verifier.New(
		verifier.WithSeed(uint64(seed)),
		verifier.WithPlatform(options.Platform),
		verifier.WithAppPackage(options.BundleID),
	)
	if err != nil {
		return fmt.Errorf("verifier: %w", err)
	}
	if err := verifierInstance.Load(string(bundle.JavaScript)); err != nil {
		return fmt.Errorf("load spec: %w", err)
	}
	fmt.Fprintln(stdout, "spec loaded into verifier")

	runDirectory := filepath.Join(options.Output, time.Now().UTC().Format("20060102-150405"))
	traceWriter, err := trace.NewWriter(runDirectory)
	if err != nil {
		return fmt.Errorf("trace writer: %w", err)
	}
	defer traceWriter.Close()
	meta := trace.Meta{
		Seed:              seed,
		SpecPath:          options.Spec,
		BundleSHA256:      bundle.SHA256,
		Platform:          options.Platform,
		BundleID:          options.BundleID,
		StartedAt:         time.Now().UTC(),
		SanderlingVersion: "0.0.1",
	}
	if err := traceWriter.WriteMeta(meta); err != nil {
		return fmt.Errorf("trace meta: %w", err)
	}
	defer func() {
		endedAt := time.Now().UTC()
		meta.EndedAt = &endedAt
		_ = traceWriter.WriteMeta(meta)
	}()
	fmt.Fprintf(stdout, "trace dir: %s\n", runDirectory)

	fmt.Fprintf(stdout, "running for %s (seed=%d)\n", options.Duration, seed)
	summary, err := runner.Run(ctx, runner.Options{
		Duration:    options.Duration,
		IdleTimeout: 1 * time.Second,
		BundleID:    options.BundleID,
		Driver:      activeDriver,
		Verifier:    verifierInstance,
		TraceWriter: traceWriter,
		Logger:      newProgressLogger(stdout),
	})

	terminateCtx, terminateCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = activeDriver.Terminate(terminateCtx)
	terminateCancel()

	if err != nil {
		return fmt.Errorf("runner: %w", err)
	}

	fmt.Fprintf(stdout, "\nelapsed: %s\n", summary.EndTime.Sub(summary.StartTime).Round(time.Millisecond))
	runner.RenderSummary(stdout, summary, options.Platform)
	return nil
}

// bundleInputs holds the pre-driver assembly: alias map, seed, esbuild defines,
// and the resolved spec-API/goja-runtime paths the bundler consumes.
type bundleInputs struct {
	aliases         map[string]string
	seed            int64
	defines         map[string]string
	specAPIPath     string
	gojaRuntimePath string
}

// prepareBundleInputs builds the alias map, defines, seed, and resolves the
// goja runtime path. It is the pure (no driver/JVM) front half of Execute,
// returning the documented error when the runtime entry cannot be located.
func prepareBundleInputs(options Options) (bundleInputs, error) {
	aliases := map[string]string{}
	specAPIPath := resolveSpecAPIPath(options.Spec)
	if specAPIPath != "" {
		aliases["@sanderling/spec"] = specAPIPath
		base := filepath.Dir(specAPIPath)
		aliases["@sanderling/spec/defaults"] = filepath.Join(base, "defaults/index.ts")
		aliases["@sanderling/spec/defaults/properties"] = filepath.Join(base, "defaults/properties.ts")
	}
	seed := resolveSeed(options.Seed)
	defines := map[string]string{
		"SANDERLING_TEST_PHONE": os.Getenv("SANDERLING_TEST_PHONE"),
		"SANDERLING_TEST_OTP":   os.Getenv("SANDERLING_TEST_OTP"),
		"SANDERLING_SEED":       strconv.FormatInt(seed, 10),
	}
	gojaRuntimePath := resolveGojaRuntimePath(specAPIPath, options.Spec)
	if gojaRuntimePath == "" {
		return bundleInputs{}, fmt.Errorf("goja-runtime.ts not found near %s; checkout pkg/spec or set @sanderling/spec alias", options.Spec)
	}
	return bundleInputs{
		aliases:         aliases,
		seed:            seed,
		defines:         defines,
		specAPIPath:     specAPIPath,
		gojaRuntimePath: gojaRuntimePath,
	}, nil
}

// resolveSeed returns the configured seed, or a time-derived one when unset.
// The same value seeds both the goja PRNG and the web bundle's SANDERLING_SEED
// define, so a single run is reproducible across both runtimes.
func resolveSeed(configured int64) int64 {
	if configured != 0 {
		return configured
	}
	return time.Now().UnixNano()
}

// resolveWebRuntimePath returns the path to pkg/spec/src/web-runtime.ts.
func resolveWebRuntimePath(specAPIPath, userSpecPath string) string {
	return resolveRuntimeSibling(specAPIPath, userSpecPath, "web-runtime.ts")
}

// resolveGojaRuntimePath returns the path to pkg/spec/src/goja-runtime.ts, the
// native verifier's runtime entry that installs __sanderlingNextAction__.
func resolveGojaRuntimePath(specAPIPath, userSpecPath string) string {
	return resolveRuntimeSibling(specAPIPath, userSpecPath, "goja-runtime.ts")
}

// resolveRuntimeSibling finds a runtime-entry file that sits beside the spec-API
// index.ts. Tries the spec-API checkout first (so monorepo development works
// without publishing the package), then falls back to a node_modules path.
func resolveRuntimeSibling(specAPIPath, userSpecPath, filename string) string {
	if specAPIPath != "" {
		candidate := filepath.Join(filepath.Dir(specAPIPath), filename)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if absoluteSpec, err := filepath.Abs(userSpecPath); err == nil {
		directory := filepath.Dir(absoluteSpec)
		for {
			candidate := filepath.Join(directory, "node_modules", "@sanderling", "spec", "src", filename)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
			parent := filepath.Dir(directory)
			if parent == directory {
				break
			}
			directory = parent
		}
	}
	return ""
}

// resolveSpecAPIPath returns the path to pkg/spec/src/index.ts inside
// a sanderling source checkout, searched upward from the spec file and the cwd.
// Returns "" when not found, in which case esbuild resolves @sanderling/spec via
// node_modules the way a downstream user's project would.
func resolveSpecAPIPath(specPath string) string {
	var candidates []string
	if absoluteSpec, err := filepath.Abs(specPath); err == nil {
		directory := filepath.Dir(absoluteSpec)
		for {
			candidates = append(candidates, filepath.Join(directory, "pkg/spec/src/index.ts"))
			parent := filepath.Dir(directory)
			if parent == directory {
				break
			}
			directory = parent
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "pkg/spec/src/index.ts"))
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
