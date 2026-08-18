// Command defect-identity counts distinct defects across stored runs. A
// property reports at most once per run, so a run-level count is the number of
// properties violated; a defect is identified across runs by the property, the
// action attributed as the origin of the failed obligation, and the screen the
// witness observed.
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
	mode := flag.String(
		"action-key",
		string(bySelector),
		"how much of the origin action identifies it: selector or full",
	)
	flag.Usage = func() {
		fmt.Fprintln(
			os.Stderr,
			"usage: defect-identity [--json] [--action-key selector|full] <run directory> ...",
		)
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	if *mode != string(bySelector) && *mode != string(byFullAction) {
		fmt.Fprintf(os.Stderr, "unknown --action-key %q\n", *mode)
		os.Exit(2)
	}

	runs, err := loadAll(flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(runs) == 0 {
		fmt.Fprintln(os.Stderr, "no run directory found under the given paths")
		os.Exit(1)
	}
	corpus, err := identify(runs, actionKeyMode(*mode))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if *jsonOut {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(corpus); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	render(os.Stdout, corpus)
}

func loadAll(paths []string) ([]tracecorpus.Run, error) {
	var runs []tracecorpus.Run
	for _, path := range paths {
		directories, err := tracecorpus.Discover(path)
		if err != nil {
			return nil, err
		}
		for _, directory := range directories {
			run, err := tracecorpus.Load(directory)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", directory, err)
			}
			runs = append(runs, run)
		}
	}
	return runs, nil
}

func render(out io.Writer, corpus Corpus) {
	writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "property\torigin action\twitness screen\truns\tseeds")
	for _, instance := range corpus.Instances {
		screen := instance.WitnessScreen
		if screen == "" {
			screen = "(unnamed)"
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%d\t%v\n",
			instance.Property, instance.OriginAction, screen,
			len(instance.Runs), instance.Seeds)
	}
	writer.Flush()

	fmt.Fprintf(out, "\n%d distinct defect(s) over %d run(s); %d seen in exactly one run\n",
		len(corpus.Instances), corpus.Runs, corpus.Singletons())
	if degraded := corpus.DegradedIdentities(); degraded > 0 {
		fmt.Fprintf(out,
			"%d identity(ies) rest on the origin selector alone, because the value typed "+
				"there is redacted in the record; two runs that typed different values into "+
				"that field read as one, so the count above is a floor for those\n",
			degraded)
	}
	if corpus.UnnamedScreen > 0 {
		fmt.Fprintf(out,
			"%d violation(s) witnessed on a screen the app does not name, "+
				"so identity rests on property and origin action alone for those\n",
			corpus.UnnamedScreen)
	}
	for _, gap := range corpus.Unattributed {
		fmt.Fprintf(out, "unattributed: %s at step %d of %s (%s)\n",
			gap.Property, gap.Step, gap.Run, gap.Reason)
	}
}
