package testrun

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"

	"github.com/priyanshujain/sanderling/internal/android"
	"github.com/priyanshujain/sanderling/internal/bundler"
	"github.com/priyanshujain/sanderling/internal/ios"
	"github.com/priyanshujain/sanderling/internal/runner"
	"github.com/priyanshujain/sanderling/internal/trace"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

const sidecarStartupTimeout = 30 * time.Second

// Options are the parameters for a single test pipeline run.
type Options struct {
	Spec      string
	BundleID  string
	Platform  string
	AVD       string
	IosDevice string
	Duration  time.Duration
	Seed      int64
	Output    string
	ClearData bool
}

// Execute runs the full test pipeline: bundle, launch app, verify properties.
func Execute(ctx context.Context, options Options, stdout io.Writer) error {
	switch options.Platform {
	case "android":
		if err := android.EnsureDevice(ctx, options.AVD, stdout); err != nil {
			return err
		}
	case "ios":
		if err := ios.EnsureSimulator(ctx, options.IosDevice, stdout); err != nil {
			return err
		}
	}
	aliases := map[string]string{}
	if specAPIPath := resolveSpecAPIPath(options.Spec); specAPIPath != "" {
		aliases["@sanderling/spec"] = specAPIPath
		base := filepath.Dir(specAPIPath)
		aliases["@sanderling/spec/defaults/properties"] = filepath.Join(base, "defaults/properties.ts")
	}
	bundle, err := bundler.Bundle(bundler.Options{
		EntryFile: options.Spec,
		Defines: map[string]string{
			"SANDERLING_TEST_PHONE": os.Getenv("SANDERLING_TEST_PHONE"),
			"SANDERLING_TEST_OTP":   os.Getenv("SANDERLING_TEST_OTP"),
		},
		Aliases: aliases,
	})
	if err != nil {
		return fmt.Errorf("bundle spec: %w", err)
	}
	fmt.Fprintf(stdout, "bundled spec: %d bytes (sha256=%s)\n", len(bundle.JavaScript), bundle.SHA256[:12])

	activeDriver, cleanup, err := buildDriver(ctx, options, stdout)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := activeDriver.Launch(ctx, options.BundleID, options.ClearData, nil); err != nil {
		return fmt.Errorf("launch app: %w", err)
	}

	seed := options.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	verifierInstance, err := verifier.New(verifier.WithRand(rand.New(rand.NewPCG(uint64(seed), 0))))
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

	fmt.Fprintf(stdout, "\nrun complete: %d steps in %s\n", summary.Steps, summary.EndTime.Sub(summary.StartTime).Round(time.Millisecond))
	if len(summary.Violations) == 0 {
		fmt.Fprintln(stdout, "no violations.")
	} else {
		fmt.Fprintf(stdout, "%d violation record(s):\n", len(summary.Violations))
		for _, violation := range summary.Violations {
			fmt.Fprintf(stdout, "  step %d: %v\n", violation.StepIndex, violation.Properties)
		}
	}
	return nil
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
