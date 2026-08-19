package main

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// These records are the shape a real folio-web campaign produced: an
// `eventually` obligation armed on step 1, never satisfied, and reported when
// the run ended at the step budget. Timing that event by the step that armed it
// puts the first violation on step 1 and reports a median of one step to first
// violation for an arm that spent its whole budget before it could know.
func liveness(seed int64, budget int, properties ...string) map[string]any {
	return map[string]any{
		"seed": seed, "exit_code": 0, "steps": budget, "actions": budget - 1,
		"monotonic_millis":              15000,
		"first_violation_origin_step":   1,
		"first_violation_detected_step": budget,
		"first_violation_reason":        "eventually never satisfied",
		"violated_properties":           properties,
	}
}

func TestClassify_ObligationReportedAtTheRunEndIsTimedAtItsDetection(t *testing.T) {
	detected := 40
	item := classify(runRecord{
		Seed: 2, Steps: 40, Actions: stepPointer(39),
		FirstViolationOriginStep:   stepPointer(1),
		FirstViolationDetectedStep: &detected,
		ViolatedProperties:         []string{"someTransactionExists"},
	}, 40)

	if !item.Violated {
		t.Fatal("run not marked as violated")
	}
	if item.OriginStep != 1 {
		t.Errorf("origin step %d, want the step that armed the obligation", item.OriginStep)
	}
	if item.EventStep != 40 {
		t.Errorf("event step %d, want the step the run could know, 40", item.EventStep)
	}
	current := arm{Budget: 40, Runs: []classifiedRun{item}}
	observations := current.observations()
	if len(observations) != 1 || !observations[0].Event || observations[0].Steps != 40 {
		t.Errorf("observations %+v, want one event at 40", observations)
	}
}

// A safety property that trips under its own action is detected on the step
// that armed it, so nothing about the existing outcome moves.
func TestClassify_SafetyViolationKeepsItsOriginStep(t *testing.T) {
	detected := 12
	item := classify(runRecord{
		Steps: 12, FirstViolationOriginStep: stepPointer(12), FirstViolationDetectedStep: &detected,
	}, 400)
	if item.EventStep != 12 || item.OriginStep != 12 {
		t.Errorf("run %+v, want an event at 12", item)
	}
}

// A campaign written before the detected step was recorded still reads, and
// keeps timing its events at the origin.
func TestClassify_MissingDetectedStepKeepsTheOrigin(t *testing.T) {
	item := classify(runRecord{Steps: 30, FirstViolationOriginStep: stepPointer(18)}, 400)
	if item.EventStep != 18 {
		t.Errorf("event step %d, want the origin step 18", item.EventStep)
	}
}

// The whole-pipeline form of the same thing, on the record shape a real web
// campaign wrote. Before the outcome was timed at detection this reported a
// median of 1 step to first violation.
func TestRun_LivenessFlushedAtTheBudgetDoesNotReportAOneStepMedian(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "seeded-web")
	writeCampaign(t, directory, map[string]any{
		"arm": "seeded-web", "generator": "seeded", "platform": "web",
		"max_steps": 40, "seeds": []int{1, 2, 3, 4, 5},
	}, []map[string]any{
		{"seed": 1, "exit_code": 0, "steps": 40, "actions": 38, "monotonic_millis": 13512},
		liveness(2, 40, "accountCreationReachable", "someTransactionExists"),
		liveness(3, 40, "someTransactionExists"),
		liveness(4, 40, "accountCreationReachable", "someTransactionExists"),
		{"seed": 5, "exit_code": 0, "steps": 40, "actions": 40, "monotonic_millis": 15433},
	})

	summary := armByName(t, analyseCampaigns(t, directory), "seeded-web")
	if summary.MedianStepsToFirstViolation == nil {
		t.Fatal("median undefined, want it at the budget")
	}
	if *summary.MedianStepsToFirstViolation != 40 {
		t.Errorf("median %v steps to first violation, want 40: no run could know before the budget",
			*summary.MedianStepsToFirstViolation)
	}
	if summary.EventsDetectedAfterOrigin != 3 {
		t.Errorf("%d events detected after their origin, want 3", summary.EventsDetectedAfterOrigin)
	}

	var stdout bytes.Buffer
	if err := run([]string{directory}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "timed 3 violation(s) at the step they were detected") {
		t.Errorf("the report does not say the events were timed at detection\n%s", stdout.String())
	}
}
