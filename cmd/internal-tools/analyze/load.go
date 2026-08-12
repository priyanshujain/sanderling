package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	manifestFileName = "campaign.json"
	recordsFileName  = "runs.jsonl"
	maxRecordBytes   = 4 * 1024 * 1024
)

// manifest mirrors the fields analyze reads from campaign.json. The step budget
// lives here rather than in any run, because it is what clean runs are censored
// at and every run in an arm has to share it.
type manifest struct {
	Arm       string  `json:"arm"`
	Generator string  `json:"generator"`
	Platform  string  `json:"platform"`
	MaxSteps  int     `json:"max_steps"`
	Seeds     []int64 `json:"seeds"`
	Host      string  `json:"host"`
}

// runRecord mirrors the fields analyze reads from one line of runs.jsonl.
type runRecord struct {
	Seed                     int64    `json:"seed"`
	ExitCode                 int      `json:"exit_code"`
	LaunchError              string   `json:"launch_error"`
	TimedOut                 bool     `json:"timed_out"`
	DurationMillis           int64    `json:"duration_millis"`
	TraceError               string   `json:"trace_error"`
	Steps                    int      `json:"steps"`
	FirstViolationOriginStep *int     `json:"first_violation_origin_step"`
	ViolatedProperties       []string `json:"violated_properties"`
}

// Exclusion reasons. A run that failed or timed out is missing data, not a
// censored observation: it never ran its budget, so treating it as a clean run
// that survived to the budget would bias the survival estimate downward.
const (
	reasonLaunchError   = "launch error"
	reasonTimedOut      = "timed out"
	reasonNonzeroExit   = "nonzero exit"
	reasonTraceError    = "unreadable trace"
	reasonMalformedStep = "violation step outside the budget"
)

type classifiedRun struct {
	Seed               int64
	Steps              int
	DurationMillis     int64
	OriginStep         int
	Violated           bool
	ClampedToBudget    bool
	ViolatedProperties []string
	ExcludedBecause    string
}

type arm struct {
	Name         string
	Budget       int
	Generator    string
	Platform     string
	Directories  []string
	Runs         []classifiedRun
	MissingSeeds []int64
}

func loadCampaign(directory string) (manifest, []runRecord, error) {
	body, err := os.ReadFile(filepath.Join(directory, manifestFileName))
	if err != nil {
		return manifest{}, nil, fmt.Errorf("read %s: %w", manifestFileName, err)
	}
	var declared manifest
	if err := json.Unmarshal(body, &declared); err != nil {
		return manifest{}, nil, fmt.Errorf("parse %s in %s: %w", manifestFileName, directory, err)
	}
	if declared.Arm == "" {
		return manifest{}, nil, fmt.Errorf("%s in %s has no arm", manifestFileName, directory)
	}
	if declared.MaxSteps <= 0 {
		return manifest{}, nil, fmt.Errorf("%s in %s has max_steps %d: clean runs have nothing to be censored at",
			manifestFileName, directory, declared.MaxSteps)
	}

	file, err := os.Open(filepath.Join(directory, recordsFileName))
	if err != nil {
		return manifest{}, nil, fmt.Errorf("read %s: %w", recordsFileName, err)
	}
	defer file.Close()

	var records []runRecord
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxRecordBytes)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		var record runRecord
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return manifest{}, nil, fmt.Errorf("%s line %d in %s: %w", recordsFileName, lineNumber, directory, err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return manifest{}, nil, fmt.Errorf("read %s in %s: %w", recordsFileName, directory, err)
	}
	return declared, records, nil
}

// classify turns one record into the run the analysis works with, deciding
// whether it is usable and, if it is, whether it is an event or censored.
func classify(record runRecord, budget int) classifiedRun {
	item := classifiedRun{
		Seed:               record.Seed,
		Steps:              record.Steps,
		DurationMillis:     record.DurationMillis,
		ViolatedProperties: slices.Clone(record.ViolatedProperties),
	}
	switch {
	case record.LaunchError != "":
		item.ExcludedBecause = reasonLaunchError
		return item
	case record.TimedOut:
		item.ExcludedBecause = reasonTimedOut
		return item
	case record.ExitCode != 0:
		item.ExcludedBecause = reasonNonzeroExit
		return item
	case record.TraceError != "":
		item.ExcludedBecause = reasonTraceError
		return item
	}
	if record.FirstViolationOriginStep == nil {
		if len(record.ViolatedProperties) > 0 {
			item.ExcludedBecause = reasonMalformedStep
		}
		return item
	}
	origin := *record.FirstViolationOriginStep
	if origin < 1 {
		item.ExcludedBecause = reasonMalformedStep
		return item
	}
	item.Violated = true
	if origin > budget {
		// The run-end finalize line reports obligations that never discharged
		// at an index one past the last executed step. That is a real detection
		// but not a real step, so it is held at the budget and counted.
		origin = budget
		item.ClampedToBudget = true
	}
	item.OriginStep = origin
	return item
}

// groupArms folds every campaign directory into its arm. Two directories with
// the same arm label are pooled, which is how a campaign split across hosts is
// analysed, but they must agree on the step budget.
func groupArms(directories []string) ([]arm, error) {
	byName := map[string]*arm{}
	var order []string
	for _, directory := range directories {
		declared, records, err := loadCampaign(directory)
		if err != nil {
			return nil, err
		}
		current, seen := byName[declared.Arm]
		if !seen {
			current = &arm{
				Name:      declared.Arm,
				Budget:    declared.MaxSteps,
				Generator: declared.Generator,
				Platform:  declared.Platform,
			}
			byName[declared.Arm] = current
			order = append(order, declared.Arm)
		}
		if current.Budget != declared.MaxSteps {
			return nil, fmt.Errorf("arm %q has step budget %d in an earlier campaign and %d in %s: "+
				"runs censored at different budgets cannot be pooled",
				declared.Arm, current.Budget, declared.MaxSteps, directory)
		}
		current.Directories = append(current.Directories, directory)

		present := map[int64]bool{}
		for _, record := range records {
			present[record.Seed] = true
			current.Runs = append(current.Runs, classify(record, declared.MaxSteps))
		}
		for _, seed := range declared.Seeds {
			if !present[seed] {
				current.MissingSeeds = append(current.MissingSeeds, seed)
			}
		}
	}
	slices.Sort(order)
	arms := make([]arm, 0, len(order))
	for _, name := range order {
		arms = append(arms, *byName[name])
	}
	return arms, nil
}

// observations returns the usable runs as survival data: an event at the step
// that armed the first violation, or a censored observation at the step budget.
func (a arm) observations() []observation {
	var result []observation
	for _, item := range a.Runs {
		if item.ExcludedBecause != "" {
			continue
		}
		if item.Violated {
			result = append(result, observation{Steps: float64(item.OriginStep), Event: true})
			continue
		}
		result = append(result, observation{Steps: float64(a.Budget), Event: false})
	}
	return result
}

// stepTimes is the observations flattened to plain numbers, censored runs held
// at the budget. Holding them there rather than dropping them is conservative:
// it can only understate how much sooner a violating arm finds its first
// defect, never overstate it.
func (a arm) stepTimes() []float64 {
	var result []float64
	for _, item := range a.observations() {
		result = append(result, item.Steps)
	}
	return result
}
