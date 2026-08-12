package main

import (
	"context"
	"io"

	"github.com/priyanshujain/sanderling/internal/testrun"
)

func runTestPipeline(ctx context.Context, options testOptions, stdout io.Writer) error {
	return testrun.Execute(ctx, pipelineOptions(options), stdout)
}

// pipelineOptions maps the parsed flags onto the pipeline's options. A field
// dropped on the way through here is a run that executes one experiment cell
// and records another, which is worth being able to test on its own.
func pipelineOptions(options testOptions) testrun.Options {
	return testrun.Options{
		Spec:           options.spec,
		BundleID:       options.bundleID,
		Platform:       options.platform,
		AVD:            options.avd,
		Device:         options.device,
		IosDevice:      options.iosDevice,
		IosAppPath:     options.iosAppPath,
		AndroidAppPath: options.androidAppPath,
		Duration:       options.duration,
		MaxSteps:       options.maxSteps,
		Seed:           options.seed,
		Output:         options.output,
		ClearData:      options.clearData,
		Generator:      options.generator,
		LabelSource:    options.labelSource,
		Arm:            options.arm,
	}
}
