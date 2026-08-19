// Command implementation-sweep runs one identical campaign against every model
// implementation of a single requirement. It installs, builds and serves each
// implementation on its own port, then hands the campaign tool the same seed
// slice, the same step budget and the same generator for all of them, so a
// difference between implementations is not a difference in exploration.
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
// pre-registration runs the seeded policy against a served web build, and a
// sweep that could quietly run something else records a comparison nobody made.
const (
	generator = "seeded"
	platform  = "web"
)

// defaultConcurrency is how many implementations are built, served and swept at
// once. fleet.md measured eight concurrent web campaigns clean at about 1.1 GB
// resident each, parallel efficiency 0.83 at eight against 0.87 at six, and a
// knee at twelve to sixteen, on a contended laptop it says to re-measure before
// trusting anything above eight. Six sits at the better efficiency, costs about
// 7 GB of a 64 GB host, and leaves the Android emulator farm that shares that
// host its four to six slots. Each worker here also carries a vite server the
// fleet measurement did not include.
const defaultConcurrency = 6

// defaultBasePort is the first port handed out. Vite's own defaults are 5173
// and 4173, so a sweep starting here does not collide with a dev server someone
// left running.
const defaultBasePort = 5300

type config struct {
	implementationsDirectory string
	specPath                 string
	outputDirectory          string
	seeds                    []int64
	maxSteps                 int
	duration                 time.Duration
	concurrency              int
	basePort                 int
	bunPath                  string
	campaignPath             string
	sanderlingPath           string
	extraArguments           []string
}

const usage = `implementation-sweep runs one seeded campaign per model implementation.

Usage:
  implementation-sweep --implementations <dir> --spec <path> --seeds <spec>
                       --max-steps <n> --output <dir> [flags]
                       [-- <sanderling test flags>]

Each impl-* directory under --implementations is installed, built and served on
its own port, and every one is swept with the same seeds and the same step
budget. An implementation that fails to install, build or serve is recorded and
the sweep moves on to the next one.

Everything after a bare -- reaches every sanderling test call through the
campaign tool.
`

func parseArguments(arguments []string, stderr io.Writer) (config, error) {
	flagSet := flag.NewFlagSet("implementation-sweep", flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	flagSet.Usage = func() {
		fmt.Fprint(stderr, usage)
		flagSet.PrintDefaults()
	}
	var configuration config
	var seedSpecification string
	flagSet.StringVar(
		&configuration.implementationsDirectory,
		"implementations",
		"",
		"directory holding impl-01 to impl-NN (required)",
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
		"implementations built, served and swept at once",
	)
	flagSet.IntVar(
		&configuration.basePort,
		"base-port",
		defaultBasePort,
		"first port served; each implementation takes the next one in name order",
	)
	flagSet.StringVar(
		&configuration.outputDirectory,
		"output",
		"",
		"campaign tree to create (required)",
	)
	flagSet.StringVar(
		&configuration.bunPath,
		"bun",
		"bun",
		"bun binary that installs, builds and serves each implementation",
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
		{"--implementations", configuration.implementationsDirectory},
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
	for name, value := range map[string]*string{
		"--implementations": &configuration.implementationsDirectory,
		"--spec":            &configuration.specPath,
		"--output":          &configuration.outputDirectory,
	} {
		absolute, err := filepath.Abs(*value)
		if err != nil {
			return config{}, fmt.Errorf("%s: %w", name, err)
		}
		*value = absolute
	}
	return configuration, nil
}

// servedURL is the one place a seed becomes a URL. The same seed is also handed
// to the campaign as --seeds, which reaches sanderling as --seed and fixes the
// exploration, while the scaffold reads ?seed= and fixes the latency and the
// outcome of every send. A violation replays only when both carry the same
// number, so both come from the seed argument here and never from two flags.
func servedURL(port int, seed string) string {
	return fmt.Sprintf("http://localhost:%d/?seed=%s", port, seed)
}

func readinessURL(port int) string {
	return fmt.Sprintf("http://localhost:%d/", port)
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

// campaignArguments builds one campaign invocation. The seed is a string
// because it lands in two arguments, --seeds and the ?seed= of --bundle-id,
// and passing it once keeps them from drifting apart.
func campaignArguments(
	configuration config,
	target implementation,
	seed string,
) []string {
	arguments := []string{
		"--spec", configuration.specPath,
		"--bundle-id", servedURL(target.Port, seed),
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
