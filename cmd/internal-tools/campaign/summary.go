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
	// Actions counts only the steps that both chose an action and dispatched
	// it. A step whose policy declined to act, and a step whose chosen action
	// the runner threw away, changed nothing about the app, so counting either
	// as an action inflates the denominator of every per-action rate. The
	// inflation is policy-dependent, so it does not cancel between arms.
	Actions                    int      `json:"actions"`
	FirstViolationOriginStep   *int     `json:"first_violation_origin_step"`
	FirstViolationDetectedStep *int     `json:"first_violation_detected_step"`
	FirstViolationProperties   []string `json:"first_violation_properties,omitempty"`
	FirstViolationReason       string   `json:"first_violation_reason,omitempty"`
	FirstViolationIsError      bool     `json:"first_violation_is_error,omitempty"`
	ViolatedProperties         []string `json:"violated_properties,omitempty"`
}

type traceLine struct {
	Index int `json:"step"`
	// Hierarchy is read only for its presence: the run-end finalize line is the
	// one line carrying violations without an observed hierarchy.
	Hierarchy json.RawMessage `json:"hierarchy"`
	// NextAction is read only for its presence: a step that chose no action
	// carries none at all.
	NextAction    json.RawMessage          `json:"next_action"`
	ActionSkipped string                   `json:"action_skipped"`
	Violations    []string                 `json:"violations"`
	Witnesses     map[string]trace.Witness `json:"witnesses"`
}

func dispatchedAction(line traceLine) bool {
	if line.ActionSkipped != "" {
		return false
	}
	return len(line.NextAction) > 0 && string(line.NextAction) != "null"
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
	summary, err := summarizeTrace(filepath.Join(seedDirectory, name, "trace.jsonl"))
	if err != nil {
		return name, traceSummary{}, err
	}
	return name, summary, nil
}

func summarizeTrace(tracePath string) (traceSummary, error) {
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
		if !synthetic && dispatchedAction(line) {
			summary.Actions++
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
