// Command campaign sweeps a list of seeds for one experiment cell, writing a
// directory an analysis pipeline can read without re-parsing raw traces.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type config struct {
	specPath        string
	bundleID        string
	platform        string
	arm             string
	generator       string
	maxSteps        int
	duration        time.Duration
	seeds           []int64
	devices         []string
	sanderlingPath  string
	outputDirectory string
	runTimeout      time.Duration
	extraArguments  []string
}

const usage = `campaign sweeps seeds for one experiment cell of a sanderling evaluation.

Usage:
  campaign --spec <path> --bundle-id <id> --platform <android|ios|web>
           --arm <label> --generator <seeded|llm> --max-steps <n>
           --seeds <spec> --output <dir> [flags] [-- <sanderling test flags>]

Everything after a bare -- is appended verbatim to every sanderling test call.
`

func parseArguments(arguments []string, stderr io.Writer) (config, error) {
	flagSet := flag.NewFlagSet("campaign", flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	flagSet.Usage = func() {
		fmt.Fprint(stderr, usage)
		flagSet.PrintDefaults()
	}
	var configuration config
	var seedSpecification string
	var deviceList string
	flagSet.StringVar(&configuration.specPath, "spec", "", "path to the TypeScript spec (required)")
	flagSet.StringVar(&configuration.bundleID, "bundle-id", "", "target app bundle ID (required)")
	flagSet.StringVar(&configuration.platform, "platform", "android", "target platform: android, ios, web")
	flagSet.StringVar(&configuration.arm, "arm", "", "experiment cell label recorded on every run (required)")
	flagSet.StringVar(&configuration.generator, "generator", "seeded", "action generator: seeded or llm")
	flagSet.IntVar(&configuration.maxSteps, "max-steps", 0, "per-run step budget (required, must be positive)")
	flagSet.DurationVar(&configuration.duration, "duration", 5*time.Minute, "per-run wall-clock ceiling")
	flagSet.StringVar(&seedSpecification, "seeds", "", "seeds to run: ranges and lists, e.g. 1-10,20,30-32 (required)")
	flagSet.StringVar(&deviceList, "devices", "", "comma-separated device identifiers; one concurrent worker per device (on web these are worker labels, no device flag is passed)")
	flagSet.DurationVar(&configuration.runTimeout, "run-timeout", 0, "kill a run that outlives this (default: three times --duration). A wedged run holds its worker for the rest of the sweep, and nothing else will send it a signal on an unattended host")
	flagSet.StringVar(&configuration.sanderlingPath, "sanderling", "sanderling", "sanderling binary to invoke")
	flagSet.StringVar(&configuration.outputDirectory, "output", "", "campaign directory to create (required)")
	if err := flagSet.Parse(arguments); err != nil {
		return config{}, err
	}
	configuration.extraArguments = flagSet.Args()

	for name, value := range map[string]string{
		"--spec":      configuration.specPath,
		"--bundle-id": configuration.bundleID,
		"--arm":       configuration.arm,
		"--seeds":     seedSpecification,
		"--output":    configuration.outputDirectory,
	} {
		if value == "" {
			return config{}, fmt.Errorf("%s is required", name)
		}
	}
	switch configuration.platform {
	case "android", "ios", "web":
	default:
		return config{}, fmt.Errorf("unsupported platform: %q (android, ios, web)", configuration.platform)
	}
	switch configuration.generator {
	case "seeded", "llm":
	default:
		return config{}, fmt.Errorf("unsupported generator: %q (seeded, llm)", configuration.generator)
	}
	if configuration.maxSteps <= 0 {
		// Steps to first violation is right-censored at the budget, so a
		// campaign without one has nothing to censor its clean runs at.
		return config{}, fmt.Errorf("--max-steps must be positive: every run needs the same step budget")
	}
	if configuration.duration <= 0 {
		return config{}, fmt.Errorf("--duration must be positive: %s", configuration.duration)
	}
	if configuration.runTimeout < 0 {
		return config{}, fmt.Errorf("--run-timeout must not be negative: %s", configuration.runTimeout)
	}
	if configuration.runTimeout == 0 {
		configuration.runTimeout = 3 * configuration.duration
	}
	if configuration.runTimeout <= configuration.duration {
		return config{}, fmt.Errorf("--run-timeout %s must exceed --duration %s, or every run is killed before it finishes",
			configuration.runTimeout, configuration.duration)
	}
	seeds, err := parseSeeds(seedSpecification)
	if err != nil {
		return config{}, fmt.Errorf("--seeds: %w", err)
	}
	configuration.seeds = seeds
	devices, err := parseDevices(deviceList)
	if err != nil {
		return config{}, fmt.Errorf("--devices: %w", err)
	}
	configuration.devices = devices
	return configuration, nil
}

func parseDevices(list string) ([]string, error) {
	if strings.TrimSpace(list) == "" {
		return nil, nil
	}
	var devices []string
	seen := map[string]bool{}
	for _, part := range strings.Split(list, ",") {
		device := strings.TrimSpace(part)
		if device == "" {
			return nil, fmt.Errorf("empty device in %q", list)
		}
		if seen[device] {
			return nil, fmt.Errorf("duplicate device %q", device)
		}
		seen[device] = true
		devices = append(devices, device)
	}
	return devices, nil
}

// deviceFlag names the `sanderling test` flag that selects a target on this
// platform. Web has no device, so its workers only bound concurrency.
func deviceFlag(platform string) string {
	switch platform {
	case "android":
		return "--device"
	case "ios":
		return "--ios-device"
	default:
		return ""
	}
}

func seedDirectory(configuration config, seed string) string {
	return filepath.Join(configuration.outputDirectory, "seed-"+seed)
}

// runArguments builds one `sanderling test` invocation. seed and device are
// passed as strings so the same code produces both a real command and the
// placeholder template recorded in campaign.json.
func runArguments(configuration config, seed, device string) []string {
	arguments := []string{
		"test",
		"--spec", configuration.specPath,
		"--bundle-id", configuration.bundleID,
		"--platform", configuration.platform,
		"--arm", configuration.arm,
		"--generator", configuration.generator,
		"--max-steps", strconv.Itoa(configuration.maxSteps),
		"--duration", configuration.duration.String(),
		"--seed", seed,
		"--output", seedDirectory(configuration, seed),
	}
	if flagName := deviceFlag(configuration.platform); flagName != "" && device != "" {
		arguments = append(arguments, flagName, device)
	}
	return append(arguments, configuration.extraArguments...)
}

func run(arguments []string, stdout, stderr io.Writer) error {
	configuration, err := parseArguments(arguments, stderr)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return runCampaign(ctx, configuration, executeCommand, stdout)
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
