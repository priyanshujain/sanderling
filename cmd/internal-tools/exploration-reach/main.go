// Command exploration-reach counts the distinct structural states a stored
// run visited, and compares two runs by the observation at which their
// hierarchies first differ. Both read the trace alone: no device, no replay.
//
// The state is the settle path's structural hash of the recorded hierarchy,
// the same function the drivers wait on, so a state boundary here is the state
// boundary the harness itself uses.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/priyanshujain/sanderling/internal/tracecorpus"
)

func main() {
	jsonOut := flag.Bool("json", false, "emit JSON instead of a table")
	reference := flag.String(
		"reference",
		"",
		"run directory to compare against; reports where each other run's hierarchy first differs",
	)
	flag.Usage = func() {
		fmt.Fprintln(
			os.Stderr,
			"usage: exploration-reach [--json] [--reference RUN] <run directory> ...",
		)
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	reaches, err := measureAll(flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(reaches) == 0 {
		fmt.Fprintln(os.Stderr, "no run directory found under the given paths")
		os.Exit(1)
	}

	report := Report{Runs: reaches, CorpusDistinct: corpusDistinct(reaches)}
	if *reference != "" {
		base, loadErr := load(*reference)
		if loadErr != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", *reference, loadErr)
			os.Exit(1)
		}
		report.Reference = base.Directory
		for _, reach := range reaches {
			if reach.Directory == base.Directory {
				continue
			}
			report.Divergences = append(report.Divergences, diverge(base, reach))
		}
		report.MedianDivergence, report.Censored = medianDivergence(report.Divergences)
	}

	if *jsonOut {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	render(os.Stdout, report)
}

// Report is one invocation's output: reach per run and over the corpus, plus
// the divergence rows when a reference run was named.
type Report struct {
	Runs             []Reach      `json:"runs"`
	CorpusDistinct   int          `json:"corpus_distinct_states"`
	Reference        string       `json:"reference,omitempty"`
	Divergences      []Divergence `json:"divergences,omitempty"`
	MedianDivergence float64      `json:"median_divergence_index,omitempty"`
	Censored         int          `json:"never_diverged,omitempty"`
}

func load(path string) (Reach, error) {
	run, err := tracecorpus.Load(path)
	if err != nil {
		return Reach{}, err
	}
	return measure(run), nil
}

func measureAll(paths []string) ([]Reach, error) {
	var reaches []Reach
	for _, path := range paths {
		directories, err := tracecorpus.Discover(path)
		if err != nil {
			return nil, err
		}
		for _, directory := range directories {
			reach, err := load(directory)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", directory, err)
			}
			reaches = append(reaches, reach)
		}
	}
	return reaches, nil
}

func render(out io.Writer, report Report) {
	writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "run\tseed\tplatform\tobservations\tdistinct states")
	for _, reach := range report.Runs {
		fmt.Fprintf(writer, "%s\t%d\t%s\t%d\t%d\n",
			reach.Directory, reach.Seed, reach.Platform,
			reach.Observations, reach.Distinct)
	}
	writer.Flush()
	fmt.Fprintf(out, "\n%d run(s), %d distinct structural states across the corpus\n",
		len(report.Runs), report.CorpusDistinct)

	if report.Reference == "" {
		return
	}
	fmt.Fprintf(out, "\nreference: %s\n", report.Reference)
	writer = tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "run\tseed\tfirst divergence\tobservations compared")
	for _, divergence := range report.Divergences {
		where := fmt.Sprintf("%d", divergence.Step)
		if !divergence.Diverged {
			where = fmt.Sprintf("none through %d", divergence.Step)
		}
		fmt.Fprintf(writer, "%s\t%d\t%s\t%d\n",
			divergence.Directory, divergence.Seed, where, divergence.Compared)
	}
	writer.Flush()
	fmt.Fprintf(out, "\nmedian first divergence %.1f over %d run(s), %d never diverged\n",
		report.MedianDivergence, len(report.Divergences), report.Censored)
}
