package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/priyanshujain/sanderling/internal/trace"
)

// Hierarchy dumps make trace lines large; match the replay server's ceiling.
const maxTraceLineBytes = 16 * 1024 * 1024

// traceSummary is everything the analysis needs from one run, so it never has
// to open trace.jsonl again.
type traceSummary struct {
	Steps int `json:"steps"`
	// Actions counts only the steps on which the action generator both chose an
	// action and dispatched it. A step whose policy declined to act, a step
	// whose chosen action the runner threw away, and a step the spec's setup
	// drove before the generator was ever consulted all explored nothing, so
	// counting any of them as an action inflates the denominator of every
	// per-action rate. The inflation is policy-dependent, so it does not cancel
	// between arms.
	Actions int `json:"actions"`
	// UnattributedActions counts the dispatched steps whose action names no
	// producer, which only a trace recorded before actions carried one can do.
	// Such a run's Actions is the count it was already reported with rather than
	// a setup-excluding one, and this is what says so. It is written even when
	// it is zero, because a record that omits it is one the analysis has to read
	// as unattributable and a run where every action named a producer is the
	// opposite of that.
	UnattributedActions        int      `json:"unattributed_actions"`
	FirstViolationOriginStep   *int     `json:"first_violation_origin_step"`
	FirstViolationDetectedStep *int     `json:"first_violation_detected_step"`
	FirstViolationProperties   []string `json:"first_violation_properties,omitempty"`
	FirstViolationReason       string   `json:"first_violation_reason,omitempty"`
	FirstViolationIsError      bool     `json:"first_violation_is_error,omitempty"`
	ViolatedProperties         []string `json:"violated_properties,omitempty"`
	// PreconditionFailures counts the trace records naming a precondition the
	// run could not meet: the startup gate's verdict at step 0, and every later
	// step the scope guard could not bring the app back for. A run with one of
	// these and no steps never started, and counting it as a run that explored
	// and found nothing puts a harness failure in the same column as evidence.
	PreconditionFailures int `json:"precondition_failures,omitempty"`
}

type traceLine struct {
	Index int `json:"step"`
	// Hierarchy is read only for its presence: the run-end finalize line is the
	// one line carrying violations without an observed hierarchy.
	Hierarchy json.RawMessage `json:"hierarchy"`
	// NextAction is nil on a step that chose no action. Its Source names the
	// backend that chose the action, which is the only thing in the trace
	// separating a step the spec's setup drove from one the generator drove.
	NextAction          *actionLine              `json:"next_action"`
	ActionSkipped       string                   `json:"action_skipped"`
	PreconditionFailure string                   `json:"precondition_failure"`
	Violations          []string                 `json:"violations"`
	Witnesses           map[string]trace.Witness `json:"witnesses"`
}

type actionLine struct {
	Source string `json:"source"`
}

// generatorDispatched reports whether this step drove the app on the action
// generator's behalf, which is the exposure a per-action rate divides by. A
// spec's setup puts the app into its starting position before the generator is
// consulted, so its login taps are dispatched actions that explored nothing.
// Both arms are read the same way, off the source the action names, because a
// denominator that excludes setup on one arm and includes it on the other makes
// the two rates incomparable.
//
// An action naming no source at all is one recorded before the distinction
// existed and cannot be attributed now, so each arm keeps the count it was
// already reported with: everything the seeded picker dispatched, and only what
// the model stamped. summarizeTrace counts those steps separately so a
// pre-source run cannot pass its denominator off as a setup-excluding one.
func generatorDispatched(line traceLine, generator string) bool {
	if line.ActionSkipped != "" || line.NextAction == nil {
		return false
	}
	switch line.NextAction.Source {
	case "":
		return generator != trace.ActionSourceModel
	case trace.ActionSourceSetup:
		return false
	default:
		return true
	}
}

// actionUnattributed reports a dispatched step whose action names no producer.
func actionUnattributed(line traceLine) bool {
	return line.ActionSkipped == "" && line.NextAction != nil && line.NextAction.Source == ""
}

// findRunDirectory returns the run directory `sanderling test` created inside
// seedDirectory. Names are UTC timestamps, so the last one sorted is the newest.
func findRunDirectory(seedDirectory string) (string, error) {
	entries, err := os.ReadDir(seedDirectory)
	if err != nil {
		return "", fmt.Errorf("read seed dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(seedDirectory, entry.Name(), "meta.json")); err != nil {
			continue
		}
		names = append(names, entry.Name())
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no run directory with meta.json under %s", seedDirectory)
	}
	slices.Sort(names)
	return names[len(names)-1], nil
}

// summarizeRun locates the run under seedDirectory and reduces its trace to the
// fields the analysis reads. The returned path is relative to seedDirectory.
func summarizeRun(seedDirectory string) (string, traceSummary, error) {
	name, err := findRunDirectory(seedDirectory)
	if err != nil {
		return "", traceSummary{}, err
	}
	directory := filepath.Join(seedDirectory, name)
	generator, err := runGenerator(directory)
	if err != nil {
		return name, traceSummary{}, err
	}
	summary, err := summarizeTrace(filepath.Join(directory, "trace.jsonl"), generator)
	if err != nil {
		return name, traceSummary{}, err
	}
	return name, summary, nil
}

// runGenerator reads which picker drove the run. A trace recorded before
// actions named their source cannot say whether an unstamped action came from
// the seeded picker or from the spec's setup under the model picker, and the
// two count differently.
func runGenerator(runDirectory string) (string, error) {
	body, err := os.ReadFile(filepath.Join(runDirectory, "meta.json"))
	if err != nil {
		return "", fmt.Errorf("read meta: %w", err)
	}
	var meta trace.Meta
	if err := json.Unmarshal(body, &meta); err != nil {
		return "", fmt.Errorf("decode meta: %w", err)
	}
	return meta.Generator, nil
}

func summarizeTrace(tracePath, generator string) (traceSummary, error) {
	file, err := os.Open(tracePath)
	if err != nil {
		return traceSummary{}, fmt.Errorf("open trace: %w", err)
	}
	defer file.Close()

	var summary traceSummary
	violated := map[string]bool{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxTraceLineBytes)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		var line traceLine
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			return traceSummary{}, fmt.Errorf("trace line %d: %w", lineNumber, err)
		}
		// The finalize line is synthetic: it reports obligations that never
		// discharged, at an index one past the last step actually executed.
		synthetic := len(line.Violations) > 0 && len(line.Hierarchy) == 0
		if !synthetic && line.Index > summary.Steps {
			summary.Steps = line.Index
		}
		if !synthetic && generatorDispatched(line, generator) {
			summary.Actions++
		}
		if !synthetic && actionUnattributed(line) {
			summary.UnattributedActions++
		}
		if line.PreconditionFailure != "" {
			summary.PreconditionFailures++
		}
		for _, property := range line.Violations {
			violated[property] = true
			recordViolation(&summary, line, property)
		}
	}
	if err := scanner.Err(); err != nil {
		return traceSummary{}, fmt.Errorf("read trace: %w", err)
	}
	if len(violated) > 0 {
		summary.ViolatedProperties = slices.Sorted(maps.Keys(violated))
	}
	slices.Sort(summary.FirstViolationProperties)
	return summary, nil
}

// recordViolation folds one violated property into the first-violation fields.
// The origin step (the step that armed the failed obligation) orders the event,
// because that is the step count the survival analysis measures.
func recordViolation(summary *traceSummary, line traceLine, property string) {
	origin, detected := line.Index, line.Index
	witness := line.Witnesses[property]
	if witness.Step > 0 {
		origin = witness.Step
	}
	if witness.DetectedStep > 0 {
		detected = witness.DetectedStep
	}
	switch {
	case summary.FirstViolationOriginStep == nil || origin < *summary.FirstViolationOriginStep:
		summary.FirstViolationOriginStep = &origin
		summary.FirstViolationDetectedStep = &detected
		summary.FirstViolationProperties = []string{property}
		summary.FirstViolationReason = witness.Reason
		summary.FirstViolationIsError = witness.IsError
	case origin == *summary.FirstViolationOriginStep:
		summary.FirstViolationProperties = append(summary.FirstViolationProperties, property)
		if detected < *summary.FirstViolationDetectedStep {
			summary.FirstViolationDetectedStep = &detected
			summary.FirstViolationReason = witness.Reason
			summary.FirstViolationIsError = witness.IsError
		}
	}
}
