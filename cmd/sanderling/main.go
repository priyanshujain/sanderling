// Command sanderling is the CLI entry point for the property-based UI fuzzer.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/priyanshujain/sanderling/internal/testrun"
)

// Version is stamped at build time via goreleaser ldflags.
// Default "dev" marks untagged local builds.
var Version = "dev"

type testOptions struct {
	spec            string
	bundleID        string
	platform        string
	avd             string
	device          string
	iosDevice       string
	iosAppPath      string
	androidAppPath  string
	duration        time.Duration
	maxSteps        int
	arm             string
	seed            int64
	output          string
	clearData       bool
	generator       string
	labelSource     string
	exitOnViolation bool
}

const topUsage = `sanderling is a property-based UI fuzzer for mobile apps.

Usage:
  sanderling <command> [flags]

Commands:
  test     Run a spec against an app for a fixed duration.
  replay   Serve a local web UI for browsing runs/.
  doctor   Check that the host environment is ready to run sanderling.
  version  Print the sanderling version.

Run "sanderling <command> -h" for command-specific flags.
`

func parseTestArgs(args []string, stderr io.Writer) (testOptions, error) {
	flagSet := flag.NewFlagSet("test", flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	var options testOptions
	flagSet.StringVar(&options.spec, "spec", "", "path to the TypeScript spec (required)")
	flagSet.StringVar(&options.bundleID, "bundle-id", "", "target app bundle ID (required)")
	flagSet.StringVar(&options.platform, "platform", "android", "target platform: android, ios, web")
	flagSet.StringVar(&options.avd, "avd", "", "Android AVD name to boot if no device is connected")
	flagSet.StringVar(&options.device, "device", "", "Android device serial (from `adb devices`) to target when several are connected")
	flagSet.StringVar(&options.iosDevice, "ios-device", "", "iOS target: a simulator name/UDID to boot, or a connected device's name, UDID, or CoreDevice id")
	flagSet.StringVar(&options.iosAppPath, "ios-app-path", "", "path to the .app bundle for iOS clear-state reinstall (simulator: simctl; device: devicectl)")
	flagSet.StringVar(&options.androidAppPath, "android-app-path", "", "path to the .apk for Android clear-state reinstall; required to reset apps on OEM builds that deny `pm clear`")
	flagSet.DurationVar(&options.duration, "duration", 5*time.Minute, "total test duration")
	flagSet.IntVar(&options.maxSteps, "max-steps", 0, "stop after this many steps (0 = no cap; the duration deadline governs). A step budget is what makes two generators comparable, since one making a model call per step and one drawing from a PRNG are not comparable per second")
	flagSet.Int64Var(&options.seed, "seed", 0, "RNG seed (0 = random)")
	flagSet.StringVar(&options.output, "output", "./runs", "output directory for traces")
	flagSet.BoolVar(&options.clearData, "clear-data", true, "clear app data before launching so each run starts from a fresh install; pass --clear-data=false to resume prior state")
	flagSet.StringVar(&options.arm, "arm", "", "experiment cell label, recorded in meta.json so a directory of runs can be attributed to a cell")
	flagSet.StringVar(&options.generator, "generator", "seeded", "action generator: seeded (weighted random) or llm (model picks from the same candidate set; requires generator = llm() in the spec)")
	flagSet.StringVar(&options.labelSource, "label-source", "visible-text", "how candidates are named to the llm generator: visible-text (what a user reads) or resource-id (the identifier the app assigned). The seeded generator picks by index and ignores this")
	flagSet.BoolVar(&options.exitOnViolation, "exit-on-violation", false, "stop the run at the first property violation and exit 2, so CI can tell a found bug (2) from a broken harness (1)")
	if err := flagSet.Parse(args); err != nil {
		return testOptions{}, err
	}
	if options.spec == "" {
		return testOptions{}, errors.New("--spec is required")
	}
	if options.bundleID == "" {
		return testOptions{}, errors.New("--bundle-id is required")
	}
	switch options.platform {
	case "android", "ios", "web":
	default:
		return testOptions{}, fmt.Errorf("unsupported platform: %q (android, ios, web)", options.platform)
	}
	if options.maxSteps < 0 {
		return testOptions{}, fmt.Errorf("--max-steps must not be negative: %d", options.maxSteps)
	}
	switch options.generator {
	case "seeded", "llm":
	default:
		return testOptions{}, fmt.Errorf("unsupported generator: %q (seeded, llm)", options.generator)
	}
	// Rejected here rather than defaulted, because a campaign that finishes with
	// the wrong labelling and a plausible output directory is worse than one
	// that never starts.
	switch options.labelSource {
	case "visible-text", "resource-id":
	default:
		return testOptions{}, fmt.Errorf("unsupported label source: %q (visible-text, resource-id)", options.labelSource)
	}
	return options, nil
}

func runTest(options testOptions, stdout io.Writer) error {
	// A signal-aware root context: on Ctrl-C the cancellation propagates into
	// the drivers' process contexts, so spawned children (the iOS companion,
	// the xcodebuild runner session) get their SIGTERM instead of outliving
	// the run as orphans.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return runTestPipeline(ctx, options, stdout)
}

func runDoctor(args []string, stdout, stderr io.Writer) error {
	options, err := parseDoctorArgs(args, stderr)
	if err != nil {
		return err
	}
	checks := doctorChecksFor(options.platform)
	return runDoctorChecks(context.Background(), checks, stdout)
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
	case "replay":
		options, err := parseReplayArgs(args[2:], stderr)
		if err != nil {
			return err
		}
		return runReplay(options, stdout)
	case "doctor":
		return runDoctor(args[2:], stdout, stderr)
	case "version", "-v", "--version":
		fmt.Fprintln(stdout, Version)
		return nil
	default:
		return fmt.Errorf("unknown command: %q (try 'sanderling help')", args[1])
	}
}

func main() {
	if code := exitCode(run(os.Args, os.Stdout, os.Stderr), os.Stderr); code != 0 {
		os.Exit(code)
	}
}

// exitCode maps a command result to the process status and reports it on
// stderr. 2 means the run did its job and found violations under
// --exit-on-violation; 1 stays "something went wrong", so CI can tell a found
// bug from a broken harness. flag.ErrHelp means -h/--help was requested and
// flag already printed usage, so it exits 0 rather than reading as a failure.
func exitCode(err error, stderr io.Writer) int {
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return 0
	}
	var violations testrun.ViolationsError
	if errors.As(err, &violations) {
		fmt.Fprintf(stderr, "violations: %d\n", violations.Count)
		return 2
	}
	fmt.Fprintf(stderr, "error: %v\n", err)
	return 1
}
