package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/priyanshujain/sanderling/internal/trace"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

func writeRunFiles(t *testing.T, directory, meta string, steps ...string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, "meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	body := strings.Join(steps, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(directory, "trace.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRunRefusesAStepFromBeforeTheFormatChange(t *testing.T) {
	directory := t.TempDir()
	writeRunFiles(
		t,
		directory,
		`{"seed": 3}`,
		`{"step":1,"timestamp":"2026-08-15T12:00:00Z"}`,
	)

	_, err := loadRun(directory)

	if err == nil {
		t.Fatal("a version-0 step must be refused")
	}
	if !strings.Contains(err.Error(), "trace_version 0") ||
		!strings.Contains(err.Error(), "depths") {
		t.Errorf(
			"the refusal must name the version and why it cannot replay: %v",
			err,
		)
	}
}

func TestLoadRunRefusesARunWithNoSeed(t *testing.T) {
	directory := t.TempDir()
	writeRunFiles(
		t,
		directory,
		`{}`,
		`{"step":1,"trace_version":1,"timestamp":"2026-08-15T12:00:00Z"}`,
	)

	_, err := loadRun(directory)

	if err == nil || !strings.Contains(err.Error(), "seed") {
		t.Errorf("a run without a seed cannot be bundled as it was: %v", err)
	}
}

func TestLoadRunSplitsOffTheEndOfRunRecord(t *testing.T) {
	directory := t.TempDir()
	writeRunFiles(
		t,
		directory,
		`{"seed": 3}`,
		`{"step":1,"trace_version":1,"timestamp":"2026-08-15T12:00:00Z","hierarchy":{"elements":[],"depths":[]}}`,
		`{"step":2,"trace_version":1,"timestamp":"2026-08-15T12:00:01Z","violations":["reachable"]}`,
	)

	run, err := loadRun(directory)
	if err != nil {
		t.Fatal(err)
	}

	if len(run.Steps) != 1 {
		t.Fatalf("observation steps: got %d, want 1", len(run.Steps))
	}
	if run.Finalize == nil || run.Finalize.Index != 2 {
		t.Fatalf("finalize record: %+v", run.Finalize)
	}
	if run.finalizeIndex() != 2 {
		t.Errorf("finalize index: got %d, want 2", run.finalizeIndex())
	}
}

func TestExtractorFoldCarriesAValueForwardUntilItChanges(t *testing.T) {
	steps := []trace.Step{
		{Index: 1, ExtractorChanges: map[string]trace.ExtractorChange{
			"count": {
				Prev: json.RawMessage("null"),
				Curr: json.RawMessage("1"),
			},
		}},
		{Index: 2},
		{Index: 3, ExtractorChanges: map[string]trace.ExtractorChange{
			"count": {Prev: json.RawMessage("1"), Curr: json.RawMessage("4")},
		}},
	}

	folded, err := extractorFold(steps, []string{"count", "unseen"})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"1", "1", "4"}
	for position, expected := range want {
		if got := string(folded[position][0]); got != expected {
			t.Errorf(
				"count at step %d: got %s, want %s",
				position+1,
				got,
				expected,
			)
		}
		if got := string(folded[position][1]); got != "null" {
			t.Errorf(
				"an extractor that never changed must fold to null, got %s",
				got,
			)
		}
	}
}

func TestExtractorFoldRefusesAValueTheSpecCannotPlace(t *testing.T) {
	steps := []trace.Step{
		{Index: 1, ExtractorChanges: map[string]trace.ExtractorChange{
			"gone": {Curr: json.RawMessage("1")},
		}},
	}

	_, err := extractorFold(steps, []string{"count"})

	if err == nil || !strings.Contains(err.Error(), "gone") {
		t.Errorf("an unplaceable extractor value must be refused: %v", err)
	}
}

func TestLastActionIsEmptyWhenTheRunnerNeverDispatchedIt(t *testing.T) {
	dispatched := trace.Step{
		NextAction: &trace.Action{Kind: "Tap", Selector: "id:save", X: 4, Y: 5},
	}
	skipped := trace.Step{
		NextAction:    &trace.Action{Kind: "Tap", Selector: "id:save"},
		ActionSkipped: foregroundLossReason,
	}

	action := lastActionFor(dispatched)
	if action == nil || action.Kind != verifier.ActionKindTap ||
		action.On != "id:save" ||
		action.X != 4 {
		t.Fatalf("dispatched action: %+v", action)
	}
	if lastActionFor(skipped) != nil {
		t.Error(
			"an action the runner threw away never reached state.lastAction",
		)
	}
}
