package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

// Version is stamped at build time via goreleaser ldflags.
// Default "dev" marks untagged local builds.
var Version = "dev"

type testOptions struct {
	spec             string
	bundleID         string
	launcherActivity string
	platform         string
	avd              string
	duration         time.Duration
	seed             int64
	output           string
}

const topUsage = `uatu is a property-based UI fuzzer for mobile apps.

Usage:
  uatu <command> [flags]

Commands:
  test     Run a spec against an app for a fixed duration.
  doctor   Check that the host environment is ready to run uatu.
  version  Print the uatu version.

Run "uatu <command> -h" for command-specific flags.
`

func parseTestArgs(args []string, stderr io.Writer) (testOptions, error) {
	flagSet := flag.NewFlagSet("test", flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	var options testOptions
	flagSet.StringVar(&options.spec, "spec", "", "path to the TypeScript spec (required)")
	flagSet.StringVar(&options.bundleID, "bundle-id", "", "target app bundle ID (required)")
	flagSet.StringVar(&options.launcherActivity, "launcher-activity", "", "optional <pkg>/<activity> to launch (overrides default resolution)")
	flagSet.StringVar(&options.platform, "platform", "android", "target platform: android (ios deferred)")
	flagSet.StringVar(&options.avd, "avd", "", "Android AVD name to boot if no device is connected")
	flagSet.DurationVar(&options.duration, "duration", 5*time.Minute, "total test duration")
	flagSet.Int64Var(&options.seed, "seed", 0, "RNG seed (0 = random)")
	flagSet.StringVar(&options.output, "output", "./runs", "output directory for traces")
	if err := flagSet.Parse(args); err != nil {
		return testOptions{}, err
	}
	if options.spec == "" {
		return testOptions{}, errors.New("--spec is required")
	}
	if options.bundleID == "" {
		return testOptions{}, errors.New("--bundle-id is required")
	}
	if options.platform != "android" {
		return testOptions{}, fmt.Errorf("unsupported platform: %q (only android in v0.1)", options.platform)
	}
	return options, nil
}

func runTest(options testOptions, stdout io.Writer) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	return runTestPipeline(ctx, options, stdout)
}

func runDoctor(stdout io.Writer) error {
	return runDoctorChecks(context.Background(), defaultDoctorChecks(), stdout)
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) < 2 || args[1] == "-h" || args[1] == "--help" || args[1] == "help" {
		fmt.Fprint(stdout, topUsage)
		return nil
	}
	switch args[1] {
	case "test":
		options, err := parseTestArgs(args[2:], stderr)
		if err != nil {
			return err
		}
		return runTest(options, stdout)
	case "doctor":
		return runDoctor(stdout)
	case "version", "-v", "--version":
		fmt.Fprintln(stdout, Version)
		return nil
	default:
		return fmt.Errorf("unknown command: %q (try 'uatu help')", args[1])
	}
}

func main() {
	if err := run(os.Args, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
