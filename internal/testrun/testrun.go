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
	"github.com/priyanshujain/sanderling/internal/runner"
	"github.com/priyanshujain/sanderling/internal/trace"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

const sidecarStartupTimeout = 30 * time.Second

// launchTimeout bounds the pre-run app launch. It happens before the runner
// starts, so --duration does not cover it, and Execute's context is the bare
// signal-aware root with no deadline of its own: a driver wedged here would
// hang the run forever having printed nothing and written no trace. Generous
// enough to sit above every driver's own launch bound (the iOS clear-state path
// reinstalls the app first) so a driver-level error is what a user usually
// sees, and this stays the backstop. A variable so the timeout test can shrink
// it.
var launchTimeout = 3 * time.Minute

// launchApp starts the app under test under a bounded context.
func launchApp(ctx context.Context, activeDriver driver.DeviceDriver, options Options) error {
	launchCtx, cancel := context.WithTimeout(ctx, launchTimeout)
	defer cancel()
	if err := activeDriver.Launch(launchCtx, options.BundleID, options.ClearData, nil); err != nil {
		return fmt.Errorf("launch app: %w", err)
	}
	return nil
}

// Options are the parameters for a single test pipeline run.
type Options struct {
	Spec           string
	BundleID       string
	Platform       string
	AVD            string
	Device         string
	IosDevice      string
	IosAppPath     string
	AndroidAppPath string
	Duration       time.Duration
	MaxSteps       int
	Seed           int64
	Output         string
	ClearData      bool
	// Arm labels the experiment cell this run belongs to and is recorded in
	// meta.json so a directory of runs can be attributed to a cell.
	Arm string
	// ExitOnViolation stops the run at the first violation and reports the
	// recorded violations as a ViolationsError, so a caller (CI) can tell
	// "the run found the bug" from "the run finished clean".
	ExitOnViolation bool
	// Generator selects the action picker: "llm" or the default seeded picker.
	Generator string

	// iosUDID, iosIsSimulator, and iosCoreDeviceID are filled by Execute after
	// resolving the iOS target, then read by buildDriver to choose the simulator
	// companion or the physical-device driver. On the device path iosUDID is the
	// hardware UDID and iosCoreDeviceID is the CoreDevice id.
	iosUDID         string
	iosIsSimulator  bool
	iosCoreDeviceID string
}

// Execute runs the full test pipeline: bundle, launch app, verify properties.
// buildRunMeta assembles the run's meta.json. Model and Instructions are
// recorded only when the LLM picker is the one that will actually run, so a
// spec that declares generator = llm() but is run under the seeded picker does
// not label its trace with a model it never called.
func buildRunMeta(options Options, bundleSHA256 string, seed int64, host string, llmConfig verifier.LLMConfig, hasLLMConfig bool) trace.Meta {
	meta := trace.Meta{
		Seed:              seed,
		SpecPath:          options.Spec,
		BundleSHA256:      bundleSHA256,
		Platform:          options.Platform,
		BundleID:          options.BundleID,
		StartedAt:         time.Now().UTC(),
		SanderlingVersion: "0.0.1",
		Arm:               options.Arm,
		Generator:         options.Generator,
		MaxSteps:          options.MaxSteps,
		DurationMillis:    options.Duration.Milliseconds(),
		Host:              host,
	}
	if options.Generator == "llm" && hasLLMConfig {
		meta.Model = llmConfig.Model
		meta.Instructions = llmConfig.Instructions
	}
	return meta
}

func Execute(ctx context.Context, options Options, stdout io.Writer) error {
	switch options.Platform {
	case "android":
		if err := android.EnsureDevice(ctx, options.Device, options.AVD, stdout); err != nil {
			return err
		}
		if err := android.PrepareDevice(ctx, options.Device, stdout); err != nil {
			return err
		}
		// Switch to 3-button navigation for the run so fuzzer swipes cannot
		// trigger the gesture-nav home/back and fling the app off screen;
		// restore the original mode when the run ends.
		restoreNav := android.ForceThreeButtonNav(ctx, options.Device, stdout)
		defer restoreNav()
	case "ios":
		resolved, err := resolveIOSTarget(ctx, options, stdout)
		if err != nil {
			return err
		}
		options = resolved
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

	if err := launchApp(ctx, activeDriver, options); err != nil {
		return err
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
	hostname, _ := os.Hostname()
	llmConfig, hasLLMConfig := verifierInstance.LLMConfig()
	meta := buildRunMeta(options, bundle.SHA256, seed, hostname, llmConfig, hasLLMConfig)
	if err := traceWriter.WriteMeta(meta); err != nil {
		return fmt.Errorf("trace meta: %w", err)
	}
	defer func() {
		endedAt := time.Now().UTC()
		meta.EndedAt = &endedAt
		_ = traceWriter.WriteMeta(meta)
	}()
	fmt.Fprintf(stdout, "trace dir: %s\n", runDirectory)

	if options.MaxSteps > 0 {
		fmt.Fprintf(stdout, "running for %s or %d steps, whichever comes first (seed=%d)\n", options.Duration, options.MaxSteps, seed)
	} else {
		fmt.Fprintf(stdout, "running for %s (seed=%d)\n", options.Duration, seed)
	}
	summary, err := runner.Run(ctx, runner.Options{
		Duration:        options.Duration,
		MaxSteps:        options.MaxSteps,
		IdleTimeout:     1 * time.Second,
		BundleID:        options.BundleID,
		Driver:          activeDriver,
		Verifier:        verifierInstance,
		TraceWriter:     traceWriter,
		Logger:          newProgressLogger(stdout),
		Generator:       options.Generator,
		StopOnViolation: options.ExitOnViolation,
	})

	terminateCtx, terminateCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = activeDriver.Terminate(terminateCtx)
	terminateCancel()

	if err != nil {
		return fmt.Errorf("runner: %w", err)
	}

	fmt.Fprintf(stdout, "\nelapsed: %s\n", summary.EndTime.Sub(summary.StartTime).Round(time.Millisecond))
	runner.RenderSummary(stdout, summary, options.Platform)
	return runOutcome(options, summary)
}

// runOutcome turns a finished run into the pipeline's result. Without
// --exit-on-violation a run that found violations is still a successful run
// (the summary reports them), which is the behaviour every existing caller
// depends on.
func runOutcome(options Options, summary runner.Summary) error {
	if options.ExitOnViolation && len(summary.Violations) > 0 {
		return ViolationsError{Count: len(summary.Violations)}
	}
	return nil
}

// ViolationsError reports a run that recorded violations under
// --exit-on-violation. It is deliberately distinct from every other error the
// pipeline returns: those mean the harness broke, this one means the run did
// its job and found something.
type ViolationsError struct {
	Count int
}

func (e ViolationsError) Error() string {
	return fmt.Sprintf("%d violation record(s)", e.Count)
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

// resolveSpecAPIPath returns the path to the spec API's index.ts: a sanderling
// source checkout first, searched upward from the spec file and the cwd, then
// an installed node_modules/@sanderling/spec. Aliasing the installed copy is
// what keeps the spec and the runtime entry on one module graph; resolving the
// bare specifier through package.json "exports" would load dist/ alongside the
// runtime's src/ and give sampler-rng.ts two instances.
func resolveSpecAPIPath(specPath string) string {
	var checkout, installed []string
	if absoluteSpec, err := filepath.Abs(specPath); err == nil {
		directory := filepath.Dir(absoluteSpec)
		for {
			checkout = append(checkout, filepath.Join(directory, "pkg/spec/src/index.ts"))
			installed = append(installed,
				filepath.Join(directory, "node_modules/@sanderling/spec/src/index.ts"))
			parent := filepath.Dir(directory)
			if parent == directory {
				break
			}
			directory = parent
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		checkout = append(checkout, filepath.Join(cwd, "pkg/spec/src/index.ts"))
	}
	for _, candidate := range append(checkout, installed...) {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
