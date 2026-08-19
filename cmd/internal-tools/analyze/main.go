// Command analyze reduces campaign directories to the statistics the
// evaluation reports. The primary outcome is steps to first violation, with
// clean runs right-censored where they stopped rather than discarded: defect
// yield per run is a binary that would need on the order of eighty runs an arm
// to separate, while survival analysis uses every run, including the clean ones.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const usage = `analyze reports the statistics of a sanderling evaluation from campaign directories.

Usage:
  analyze [--json <path>] <campaign-dir> [<campaign-dir> ...]

Each directory is one produced by the campaign tool and must hold campaign.json
and runs.jsonl. Directories sharing an arm label are pooled and must agree on
the step budget, and arms compared against each other must agree on it too.

One invocation is one research question: Holm corrects across the comparisons it
produces and across nothing else.
`

type stringList []string

func (list *stringList) String() string { return strings.Join(*list, ",") }

func (list *stringList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("empty campaign directory")
	}
	*list = append(*list, value)
	return nil
}

func run(arguments []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("analyze", flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	flagSet.Usage = func() {
		fmt.Fprint(stderr, usage)
		flagSet.PrintDefaults()
	}
	var directories stringList
	var jsonPath string
	var question string
	var paired bool
	flagSet.Var(&directories, "campaign", "campaign directory to read; repeat for more, or pass them as arguments")
	flagSet.StringVar(&jsonPath, "json", "", "write the machine-readable summary here, or - for stdout")
	flagSet.StringVar(&question, "question", "", "the research question these campaigns answer; Holm corrects within one invocation, and this records which family that was")
	flagSet.BoolVar(&paired, "paired", false, "the two arms ran the same seeds: contrast them seed by seed with the sign test instead of pooling them into two independent samples")
	if err := flagSet.Parse(arguments); err != nil {
		return err
	}
	directories = append(directories, flagSet.Args()...)
	if len(directories) == 0 {
		return errors.New("no campaign directories given")
	}
	seen := map[string]bool{}
	for _, directory := range directories {
		resolved, err := filepath.Abs(directory)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", directory, err)
		}
		if seen[resolved] {
			return fmt.Errorf("campaign directory %s given twice: its runs would be counted twice", directory)
		}
		seen[resolved] = true
	}

	arms, err := groupArms(directories)
	if err != nil {
		return err
	}
	var result analysis
	if paired {
		result, err = analysePaired(arms, time.Now().UTC())
		if err != nil {
			return err
		}
	} else {
		result, err = analyse(arms, time.Now().UTC())
		if err != nil {
			return err
		}
	}
	result.Question = question
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
