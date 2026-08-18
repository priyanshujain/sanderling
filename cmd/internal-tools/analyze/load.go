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
// lives here rather than in any run, because it is the exposure every run in an
// arm was given and the ceiling a clean run can be censored at.
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
	Seed        int64  `json:"seed"`
	ExitCode    int    `json:"exit_code"`
	LaunchError string `json:"launch_error"`
	TimedOut    bool   `json:"timed_out"`
	// MonotonicMillis is how long the run worked, and it is what every
	// per-hour rate here divides by: a host asleep mid-run tested nothing, so
	// charging that time to the arm would report it as slower for a reason
	// that has nothing to do with the arm. The wall clock the campaign also
	// records answers the other question, how much time passed.
	MonotonicMillis int64 `json:"monotonic_millis"`
	// DurationMillis is the name campaigns written before the two clocks were
	// split gave the same monotonic reading, so those files still read.
	DurationMillis           int64  `json:"duration_millis"`
	TraceError               string `json:"trace_error"`
	Steps                    int    `json:"steps"`
	FirstViolationOriginStep *int   `json:"first_violation_origin_step"`
	// FirstViolationDetectedStep is the step the violation was reported on,
	// which is the origin step for a safety property tripping under its own
	// action and the end of the budget for an obligation that never discharged.
	// It is what the survival analysis times the event by; see eventStep.
	FirstViolationDetectedStep *int     `json:"first_violation_detected_step"`
	ViolatedProperties         []string `json:"violated_properties"`
	// Actions is the count of steps that dispatched an action, and it is a
	// pointer so that a runs.jsonl written before the campaign tool counted
	// them is refused rather than read as an arm that acted zero times. The
	// campaign tool always emits the field, so its absence dates the file.
	Actions *int `json:"actions"`
}

// Exclusion reasons. A run that failed or timed out is missing data, not a
// censored observation: it broke off, so its step count is not exposure the app
// survived and counting it as one would bias the survival estimate downward.
const (
	reasonLaunchError   = "launch error"
	reasonTimedOut      = "timed out"
	reasonNonzeroExit   = "nonzero exit"
	reasonTraceError    = "unreadable trace"
	reasonMalformedStep = "violation step outside the budget"
)

type classifiedRun struct {
	Seed            int64
	Steps           int
	Actions         int
	MonotonicMillis int64
	OriginStep      int
	// EventStep is when the run could know, and it is what the survival
	// analysis measures. It is the origin step whenever the two agree.
	EventStep          int
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
		if record.Actions == nil {
			return manifest{}, nil, fmt.Errorf("%s line %d in %s has no actions count: it was written before "+
				"dispatched actions were counted, and reading the missing count as zero would report every "+
				"per-action rate wrongly; re-run the campaign to produce it",
				recordsFileName, lineNumber, directory)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return manifest{}, nil, fmt.Errorf("read %s in %s: %w", recordsFileName, directory, err)
	}
	return declared, records, nil
}

func (r runRecord) workingMillis() int64 {
	if r.MonotonicMillis != 0 {
		return r.MonotonicMillis
	}
	return r.DurationMillis
}

// classify turns one record into the run the analysis works with, deciding
// whether it is usable and, if it is, whether it is an event or censored.
func classify(record runRecord, budget int) classifiedRun {
	item := classifiedRun{
		Seed:               record.Seed,
		Steps:              record.Steps,
		MonotonicMillis:    record.workingMillis(),
		ViolatedProperties: slices.Clone(record.ViolatedProperties),
	}
	if record.Actions != nil {
		item.Actions = *record.Actions
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
	item.OriginStep = origin
	item.EventStep = eventStep(record, origin)
	if item.EventStep > budget {
		// The run-end finalize line reports obligations that never discharged
		// at an index one past the last executed step. That is a real detection
		// but not a real step, so it is held at the budget and counted.
		item.EventStep = budget
		item.ClampedToBudget = true
	}
	return item
}

// eventStep is the step at which the run could know it had violated. A safety
// property tripping under its own action is detected on the step that armed it
// and the two agree. An obligation that never discharges is reported when the
// run ends, and timing that at the step that armed it would record a liveness
// failure flushed at the budget as a violation found on the first step, which
// is a number the run cannot support and which no censored run can be compared
// against: the end of the run is the clock the clean runs are censored on, so
// the events have to be on it too. A campaign written before the field existed carries no
// detected step and keeps the origin.
func eventStep(record runRecord, origin int) int {
	if record.FirstViolationDetectedStep == nil {
		return origin
	}
	if detected := *record.FirstViolationDetectedStep; detected > origin {
		return detected
	}
	return origin
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
// that armed the first violation, or a censored observation at the last step
// the run reached.
func (a arm) observations() []observation {
	var result []observation
	for _, item := range a.Runs {
		if item.ExcludedBecause != "" {
			continue
		}
		result = append(result, observationOf(item, a.Budget))
	}
	return result
}

func observationOf(item classifiedRun, budget int) observation {
	if item.Violated {
		return observation{Steps: float64(item.EventStep), Event: true}
	}
	// A run that hit the campaign's wall clock stopped short of the budget, and
	// the steps it never ran are not exposure it survived.
	return observation{Steps: float64(min(item.Steps, budget)), Event: false}
}

// stepTimes is the observations flattened to plain numbers, censored runs held
// at the steps they ran. Holding them there rather than dropping them is
// conservative: it can only understate how much sooner a violating arm finds
// its first defect, never overstate it.
func (a arm) stepTimes() []float64 {
	var result []float64
	for _, item := range a.observations() {
		result = append(result, item.Steps)
	}
	return result
}
