// Command confusion-matrix cross-tabulates e4's checker verdicts against the
// blind human review, which is the measure model-implementations.md
// pre-registers: an implementation whose own suite passed, scored on whether a
// property fired and on whether the reviewer found a defect.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

const usage = `confusion-matrix cross-tabulates the e4 checker against the blind human review.

Usage:
  confusion-matrix --sweep <dir> --reviews <dir> --assignment <path> --property-clauses <path> [--json <path>]

--sweep is the directory implementation-sweep wrote: sweep.json, implementations.jsonl
and the per-implementation campaign directories under it.

--reviews holds one impl-NN.md per implementation in the shape review-protocol.md
fixes, plus any impl-NN-adjudication.md whose resolved labels replace the first
rater's for the clauses it names.

--assignment is implementations/assignment.md, the blinded implementation-to-model
mapping, opened only after the last verdict is filed.

--property-clauses declares which requirement clauses each property covers and which
locatable surfaces it reads. Without it a fired property cannot be scored against a
clause and a portability miss cannot be told from a clean run.
`

func run(arguments []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("confusion-matrix", flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	flagSet.Usage = func() {
		fmt.Fprint(stderr, usage)
		flagSet.PrintDefaults()
	}
	var sweepDirectory string
	var reviewsDirectory string
	var assignmentPath string
	var mappingPath string
	var jsonPath string
	flagSet.StringVar(&sweepDirectory, "sweep", "", "directory implementation-sweep wrote")
	flagSet.StringVar(&reviewsDirectory, "reviews", "", "directory holding impl-NN.md verdict forms")
	flagSet.StringVar(&assignmentPath, "assignment", "", "implementations/assignment.md, the implementation-to-model mapping")
	flagSet.StringVar(&mappingPath, "property-clauses", "", "the declared property-to-clause and surface mapping")
	flagSet.StringVar(&jsonPath, "json", "", "write the machine-readable summary here, or - for stdout")
	if err := flagSet.Parse(arguments); err != nil {
		return err
	}
	// Every missing flag is named together, in flag order: stopping at the
	// first turns one rerun into one rerun per missing flag.
	var missing []error
	for _, required := range []struct {
		name  string
		value string
	}{
		{"--sweep", sweepDirectory},
		{"--reviews", reviewsDirectory},
		{"--assignment", assignmentPath},
		{"--property-clauses", mappingPath},
	} {
		if required.value == "" {
			missing = append(missing, fmt.Errorf("%s is required", required.name))
		}
	}
	if err := errors.Join(missing...); err != nil {
		return err
	}

	mapping, err := loadMapping(mappingPath)
	if err != nil {
		return err
	}
	assignments, err := loadAssignment(assignmentPath)
	if err != nil {
		return err
	}
	checker, err := loadChecker(sweepDirectory)
	if err != nil {
		return err
	}
	reviews, err := loadReviews(reviewsDirectory)
	if err != nil {
		return err
	}

	result := crossTabulate(checker, reviews, assignments, mapping, time.Now().UTC())
	writeReport(result, stdout)

	if jsonPath == "" {
		return nil
	}
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal summary: %w", err)
	}
	body = append(body, '\n')
	if jsonPath == "-" {
		_, err = stdout.Write(body)
		return err
	}
	return os.WriteFile(jsonPath, body, 0o644)
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
