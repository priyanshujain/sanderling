// Command corpus-sweep runs one specification against every implementation in a
// served corpus of independent implementations of the same requirement. It
// serves each one on its own port, which is what keeps them apart: the corpus
// holds pairs that write the same localStorage key, and one origin shared
// between two of them is one stored record shared between them.
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
	"syscall"
	"time"

	"github.com/priyanshujain/sanderling/internal/seedspec"
)

// The generator and the platform are fixed rather than exposed: the
// pre-registration runs the seeded policy against the served corpus, and a
// sweep that could quietly run something else records a comparison nobody made.
const (
	generator = "seeded"
	platform  = "web"
)

// defaultConcurrency is how many implementations are swept at once. fleet.md
// measured eight concurrent web campaigns clean at about 1.1 GB resident each,
// parallel efficiency 0.83 at eight against 0.87 at six, and said to re-measure
// before trusting anything above eight on a contended host. Six sits at the
// better efficiency and leaves the emulator farm that shares the host its
// slots. Serving costs nothing here: the corpus needs no build and no separate
// server process, so a worker is one browser.
const defaultConcurrency = 6

// defaultBasePort starts above the range the model-implementation sweep hands
// out, so the two can run on one host without either being served the other's
// pages.
const defaultBasePort = 5400

type config struct {
	corpusRoot      string
	specPath        string
	outputDirectory string
	implementations []string
	seeds           []int64
	maxSteps        int
	duration        time.Duration
	concurrency     int
	basePort        int
	campaignPath    string
	sanderlingPath  string
	extraArguments  []string
}

const usage = `corpus-sweep runs one seeded campaign per implementation of a served corpus.

Usage:
  corpus-sweep --corpus <dir> --spec <path> --seeds <spec>
               --max-steps <n> --output <dir> [flags]
               [-- <sanderling test flags>]

Every implementation is served on its own port, so every one is its own origin
and none can read another's stored state, and every one is swept with the same
seeds and the same step budget. Each run's arm is the implementation's name.

Everything after a bare -- reaches every sanderling test call through the
campaign tool.
`

func parseArguments(arguments []string, stderr io.Writer) (config, error) {
	flagSet := flag.NewFlagSet("corpus-sweep", flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	flagSet.Usage = func() {
		fmt.Fprint(stderr, usage)
		flagSet.PrintDefaults()
	}
	var configuration config
	var seedSpecification string
	var selection string
	flagSet.StringVar(
		&configuration.corpusRoot,
		"corpus",
		"",
		"root of the checked-out corpus, holding examples/ (required)",
	)
	flagSet.StringVar(
		&configuration.specPath,
		"spec",
		"",
		"path to the property set every implementation is run against (required)",
	)
	flagSet.StringVar(
		&seedSpecification,
		"seeds",
		"",
		"seeds every implementation runs: ranges and lists, e.g. 1-10,20 (required)",
	)
	flagSet.StringVar(
		&selection,
		"implementations",
		"",
		"comma-separated subset of the population to sweep (default: all of it)",
	)
	flagSet.IntVar(
		&configuration.maxSteps,
		"max-steps",
		0,
		"per-run step budget, identical across implementations (required, must be positive)",
	)
	flagSet.DurationVar(
		&configuration.duration,
		"duration",
		5*time.Minute,
		"per-run wall-clock ceiling passed to each campaign",
	)
	flagSet.IntVar(
		&configuration.concurrency,
		"concurrency",
		defaultConcurrency,
		"implementations swept at once",
	)
	flagSet.IntVar(
		&configuration.basePort,
		"base-port",
		defaultBasePort,
		"first port served; each implementation takes the next one in population order",
	)
	flagSet.StringVar(
		&configuration.outputDirectory,
		"output",
		"",
		"campaign tree to create (required)",
	)
	flagSet.StringVar(
		&configuration.campaignPath,
		"campaign",
		"campaign",
		"campaign binary to invoke per implementation and seed",
	)
	flagSet.StringVar(
		&configuration.sanderlingPath,
		"sanderling",
		"sanderling",
		"sanderling binary each campaign invokes",
	)
	if err := flagSet.Parse(arguments); err != nil {
		return config{}, err
	}
	configuration.extraArguments = flagSet.Args()

	// Every missing flag is named together, in flag order: stopping at the
	// first turns one rerun into one rerun per missing flag.
	var missing []error
	for _, required := range []struct {
		name  string
		value string
	}{
		{"--corpus", configuration.corpusRoot},
		{"--spec", configuration.specPath},
		{"--seeds", seedSpecification},
		{"--output", configuration.outputDirectory},
	} {
		if required.value == "" {
			missing = append(
				missing,
				fmt.Errorf("%s is required", required.name),
			)
		}
	}
	if err := errors.Join(missing...); err != nil {
		return config{}, err
	}
	if configuration.maxSteps <= 0 {
		return config{}, fmt.Errorf(
			"--max-steps must be positive: every implementation needs the same step budget",
		)
	}
	if configuration.duration <= 0 {
		return config{}, fmt.Errorf(
			"--duration must be positive: %s",
			configuration.duration,
		)
	}
	if configuration.concurrency <= 0 {
		return config{}, fmt.Errorf(
			"--concurrency must be positive: %d",
			configuration.concurrency,
		)
	}
	if configuration.basePort < 1024 || configuration.basePort > 65535 {
		return config{}, fmt.Errorf(
			"--base-port %d is outside 1024-65535",
			configuration.basePort,
		)
	}
	seeds, err := seedspec.Parse(seedSpecification)
	if err != nil {
		return config{}, fmt.Errorf("--seeds: %w", err)
	}
	configuration.seeds = seeds
	names, err := selectImplementations(selection)
	if err != nil {
		return config{}, fmt.Errorf("--implementations: %w", err)
	}
	configuration.implementations = names
	for name, value := range map[string]*string{
		"--corpus": &configuration.corpusRoot,
		"--spec":   &configuration.specPath,
		"--output": &configuration.outputDirectory,
	} {
		absolute, err := filepath.Abs(*value)
		if err != nil {
			return config{}, fmt.Errorf("%s: %w", name, err)
		}
		*value = absolute
	}
	return configuration, nil
}

func campaignDirectory(
	configuration config,
	target implementation,
	seed string,
) string {
	return filepath.Join(
		configuration.outputDirectory,
		target.Name,
		"seed-"+seed,
	)
}

// campaignArguments builds one campaign invocation. The arm is the
// implementation's name, so every run in the tree can be attributed to the
// implementation it came from without reading back which port served it.
func campaignArguments(
	configuration config,
	target implementation,
	seed string,
) []string {
	arguments := []string{
		"--spec", configuration.specPath,
		"--bundle-id", target.URL(),
		"--platform", platform,
		"--arm", target.Name,
		"--generator", generator,
		"--max-steps", strconv.Itoa(configuration.maxSteps),
		"--duration", configuration.duration.String(),
		"--seeds", seed,
		"--sanderling", configuration.sanderlingPath,
		"--output", campaignDirectory(configuration, target, seed),
	}
	if len(configuration.extraArguments) > 0 {
		arguments = append(arguments, "--")
		arguments = append(arguments, configuration.extraArguments...)
	}
	return arguments
}

func run(arguments []string, stdout, stderr io.Writer) error {
	configuration, err := parseArguments(arguments, stderr)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()
	return runSweep(ctx, configuration, stdout)
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
