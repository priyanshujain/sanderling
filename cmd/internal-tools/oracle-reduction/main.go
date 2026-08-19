// Command oracle-reduction re-evaluates stored traces offline under four
// oracles and reports what each one refutes: the full engine, a crash-only
// detector, a single-state check, and a single-step property triple. The
// oracles vary while the traces stay fixed, which is what separates a defect an
// oracle cannot express from one an explorer never reached.
//
// The offline engine has to reproduce the verdicts each run recorded. A
// disagreement is a bug here or a gap in the trace, so it is reported as a
// mismatch and exits nonzero rather than being counted as a finding.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/priyanshujain/sanderling/internal/testrun"
)

const usage = `oracle-reduction replays stored traces under reduced oracles.

Usage:
  oracle-reduction --runs <dir> [--output <path>] [--spec <path>]

--runs is scanned recursively; every directory holding meta.json and
trace.jsonl is one trace. Each trace's spec is bundled from the path its
meta.json records unless --spec overrides it.

Exit status is 2 when the offline engine disagreed with any run's recorded
verdicts, which blocks the experiment rather than reporting the difference as
noise.

A reduced oracle whose rewrite no longer states a property reports
cannot_express for it rather than a verdict, and the property is temporal-only
when the other reductions also fail to state or refute it. A property whose
window is longer than the two observations a triple spans is reported with
single_step_truncates_window, which is the form that decides it.
`

type config struct {
	runsRoot   string
	outputPath string
	specPath   string
	// hostExtractors re-runs the spec's extractor getters over each stored
	// hierarchy instead of replaying the values a web run's page computed. It
	// asks a different question of the same trace: whether the stored tree
	// alone carries what the properties read.
	hostExtractors bool
}

func parseArguments(arguments []string, stderr io.Writer) (config, error) {
	flagSet := flag.NewFlagSet("oracle-reduction", flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	flagSet.Usage = func() {
		fmt.Fprint(stderr, usage)
		flagSet.PrintDefaults()
	}
	var configuration config
	flagSet.StringVar(
		&configuration.runsRoot,
		"runs",
		"",
		"directory tree holding the run directories to replay (required)",
	)
	flagSet.StringVar(
		&configuration.outputPath,
		"output",
		"",
		"file to write the per-trace JSONL to (default stdout)",
	)
	flagSet.StringVar(
		&configuration.specPath,
		"spec",
		"",
		"spec to bundle instead of the one each meta.json records",
	)
	flagSet.BoolVar(
		&configuration.hostExtractors,
		"host-extractors",
		false,
		"re-run the spec's extractors over each stored hierarchy instead of replaying the values a web run's page computed",
	)
	if err := flagSet.Parse(arguments); err != nil {
		return config{}, err
	}
	if configuration.runsRoot == "" {
		return config{}, fmt.Errorf("--runs is required")
	}
	return configuration, nil
}

func main() {
	configuration, err := parseArguments(os.Args[1:], os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oracle-reduction: %v\n", err)
		os.Exit(1)
	}
	code, err := run(configuration, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oracle-reduction: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(configuration config, stdout, stderr io.Writer) (int, error) {
	directories, err := discoverRuns(configuration.runsRoot)
	if err != nil {
		return 1, err
	}
	if len(directories) == 0 {
		return 1, fmt.Errorf(
			"no run directories under %s",
			configuration.runsRoot,
		)
	}

	output := stdout
	if configuration.outputPath != "" {
		file, createErr := os.Create(configuration.outputPath)
		if createErr != nil {
			return 1, createErr
		}
		defer file.Close()
		output = file
	}
	encoder := json.NewEncoder(output)

	var reports []runReport
	rejected := 0
	for _, directory := range directories {
		loaded, loadErr := loadRun(directory)
		if loadErr != nil {
			rejected++
			fmt.Fprintf(stderr, "skipped %s: %v\n", directory, loadErr)
			continue
		}
		specPath := loaded.Meta.SpecPath
		if configuration.specPath != "" {
			specPath = configuration.specPath
		}
		bundle, bundleErr := testrun.BundleSpec(specPath, loaded.Meta.Seed)
		if bundleErr != nil {
			return 1, fmt.Errorf(
				"%s: bundle %s: %w",
				directory,
				specPath,
				bundleErr,
			)
		}
		if loaded.Meta.BundleSHA256 != "" &&
			bundle.SHA256 != loaded.Meta.BundleSHA256 {
			// The bundler writes each module's path into the output relative to
			// the working directory, so this differs whenever the replay is
			// invoked from somewhere else than the run was. What a changed spec
			// would actually break is caught by the property set, the extractor
			// names and the residual comparison below.
			fmt.Fprintf(stderr,
				"note: %s bundles to %s here and the run recorded %s\n",
				directory, bundle.SHA256[:12], loaded.Meta.BundleSHA256[:12])
		}
		report, replayErr := replay(
			loaded,
			string(bundle.JavaScript),
			configuration.hostExtractors,
		)
		if replayErr != nil {
			return 1, fmt.Errorf("%s: %w", directory, replayErr)
		}
		if err := encoder.Encode(report); err != nil {
			return 1, err
		}
		reports = append(reports, report)
	}

	summarize(reports, rejected, stderr)
	for _, report := range reports {
		if !report.Valid {
			return 2, nil
		}
	}
	if len(reports) == 0 {
		return 1, fmt.Errorf(
			"every run directory was rejected; nothing was replayed",
		)
	}
	return 0, nil
}

func summarize(reports []runReport, rejected int, out io.Writer) {
	invalid := 0
	crashed := 0
	weakest := map[string]int{}
	byClass := map[string]map[string]int{}
	inexpressible := map[string]map[string]bool{}
	unmatched := 0
	engineRefutations := 0
	for _, report := range reports {
		if !report.Valid {
			invalid++
		}
		if report.CrashOnly.Fired {
			crashed++
		}
		for _, property := range report.Properties {
			recordInexpressible(
				inexpressible,
				"single-state",
				property.SingleState,
				property.Property,
			)
			recordInexpressible(
				inexpressible,
				"single-step",
				property.SingleStep,
				property.Property,
			)
			if !property.Engine.Refuted {
				if property.SingleState.Refuted || property.SingleStep.Refuted {
					unmatched++
				}
				continue
			}
			engineRefutations++
			weakest[property.Weakest]++
			if byClass[property.Class] == nil {
				byClass[property.Class] = map[string]int{}
			}
			byClass[property.Class][property.Weakest]++
		}
	}

	fmt.Fprintf(
		out,
		"\ntraces replayed: %d (rejected: %d)\n",
		len(reports),
		rejected,
	)
	fmt.Fprintf(
		out,
		"validity: %d of %d reproduced the recorded verdicts exactly\n",
		len(reports)-invalid,
		len(reports),
	)
	fmt.Fprintf(out, "traces where crash-only fired: %d\n", crashed)
	fmt.Fprintf(out, "engine refutations: %d\n", engineRefutations)
	if engineRefutations > 0 {
		fmt.Fprintf(out, "weakest refuting oracle: %s\n", counts(weakest))
		for _, class := range sortedKeys(byClass) {
			fmt.Fprintf(out, "  %s: %s\n", class, counts(byClass[class]))
		}
		fmt.Fprintf(out, "temporal-only fraction: %.3f\n",
			float64(weakest["temporal-only"])/float64(engineRefutations))
	}
	fmt.Fprintf(
		out,
		"properties a reduced oracle cannot express: %s\n",
		counts(distinct(inexpressible)),
	)
	fmt.Fprintf(
		out,
		"reduced-oracle refutations the engine did not make: %d\n",
		unmatched,
	)
}

// recordInexpressible counts a property once per oracle however many traces it
// appears on, because whether an oracle can state a property is a fact about
// the property and not about the run.
func recordInexpressible(
	seen map[string]map[string]bool,
	oracle string,
	finding refutation,
	property string,
) {
	if !finding.CannotExpress {
		return
	}
	if seen[oracle] == nil {
		seen[oracle] = map[string]bool{}
	}
	seen[oracle][property] = true
}

func distinct(seen map[string]map[string]bool) map[string]int {
	sizes := map[string]int{}
	for oracle, properties := range seen {
		sizes[oracle] = len(properties)
	}
	return sizes
}

func counts(values map[string]int) string {
	parts := make([]string, 0, len(values))
	for _, key := range sortedKeys(values) {
		parts = append(parts, fmt.Sprintf("%s=%d", key, values[key]))
	}
	return strings.Join(parts, " ")
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
