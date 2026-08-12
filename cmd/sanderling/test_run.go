package main

import (
	"context"
	"io"

	"github.com/priyanshujain/sanderling/internal/testrun"
)

func runTestPipeline(ctx context.Context, options testOptions, stdout io.Writer) error {
	return testrun.Execute(ctx, testrun.Options{
		Spec:            options.spec,
		BundleID:        options.bundleID,
		Platform:        options.platform,
		AVD:             options.avd,
		Device:          options.device,
		IosDevice:       options.iosDevice,
		IosAppPath:      options.iosAppPath,
		AndroidAppPath:  options.androidAppPath,
		Duration:        options.duration,
		MaxSteps:        options.maxSteps,
		Seed:            options.seed,
		Output:          options.output,
		ClearData:       options.clearData,
		Generator:       options.generator,
		Arm:             options.arm,
		ExitOnViolation: options.exitOnViolation,
	}, stdout)
}
