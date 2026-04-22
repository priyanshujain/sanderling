package main

import (
	"context"
	"io"

	"github.com/priyanshujain/sanderling/internal/testrun"
)

func runTestPipeline(ctx context.Context, options testOptions, stdout io.Writer) error {
	return testrun.Execute(ctx, testrun.Options{
		Spec:     options.spec,
		BundleID: options.bundleID,
		Platform: options.platform,
		AVD:      options.avd,
		Duration: options.duration,
		Seed:     options.seed,
		Output:   options.output,
	}, stdout)
}
