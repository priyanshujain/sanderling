package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
	"github.com/priyanshujain/sanderling/internal/trace"
)

func observedStep(index int) trace.Step {
	return trace.Step{
		Index:     index,
		Timestamp: time.Date(2026, 8, 12, 9, 0, index, 0, time.UTC),
		Screen:    "Home",
		Hierarchy: &hierarchy.Tree{Elements: []*hierarchy.Element{{ResourceID: "root"}}},
	}
}

func writeRunDirectory(t *testing.T, seedDirectory, name string, steps []trace.Step) string {
	t.Helper()
	directory := filepath.Join(seedDirectory, name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	meta, err := json.Marshal(trace.Meta{Seed: 11, Platform: "web", Arm: "seeded-baseline"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "meta.json"), meta, 0o644); err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	for _, step := range steps {
		if err := encoder.Encode(step); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "trace.jsonl"), buffer.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return directory
}

func TestSummarizeRun_CleanRunIsCensored(t *testing.T) {
	seedDirectory := t.TempDir()
	writeRunDirectory(t, seedDirectory, "20260812-090000", []trace.Step{
		observedStep(1), observedStep(2), observedStep(3), observedStep(4), observedStep(5),
	})

	name, summary, err := summarizeRun(seedDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if name != "20260812-090000" {
		t.Errorf("run directory: got %q", name)
	}
	if summary.Steps != 5 {
		t.Errorf("steps: got %d, want 5", summary.Steps)
	}
	if summary.FirstViolationOriginStep != nil || summary.FirstViolationDetectedStep != nil {
		t.Errorf("clean run reported a violation: %+v", summary)
	}
	if len(summary.ViolatedProperties) != 0 {
		t.Errorf("violated properties: got %v", summary.ViolatedProperties)
	}
}

func TestSummarizeRun_UsesWitnessOriginNotDetectionStep(t *testing.T) {
	seedDirectory := t.TempDir()
	violating := observedStep(9)
	violating.Violations = []string{"balanceNeverNegative"}
	violating.Witnesses = map[string]trace.Witness{
		"balanceNeverNegative": {Reason: "balance went negative", Step: 4, DetectedStep: 9},
	}
	writeRunDirectory(t, seedDirectory, "20260812-090000", []trace.Step{
		observedStep(1), observedStep(2), violating, observedStep(10),
	})

	_, summary, err := summarizeRun(seedDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if summary.FirstViolationOriginStep == nil || *summary.FirstViolationOriginStep != 4 {
		t.Fatalf("origin step: got %v, want 4", summary.FirstViolationOriginStep)
	}
	if summary.FirstViolationDetectedStep == nil || *summary.FirstViolationDetectedStep != 9 {
		t.Fatalf("detected step: got %v, want 9", summary.FirstViolationDetectedStep)
	}
	if summary.FirstViolationReason != "balance went negative" {
		t.Errorf("reason: got %q", summary.FirstViolationReason)
	}
	if !slices.Equal(summary.FirstViolationProperties, []string{"balanceNeverNegative"}) {
		t.Errorf("first violation properties: got %v", summary.FirstViolationProperties)
	}
}

func TestSummarizeRun_EarliestOriginWinsOverEarliestDetection(t *testing.T) {
	seedDirectory := t.TempDir()
	early := observedStep(3)
	early.Violations = []string{"detectedFirst"}
	early.Witnesses = map[string]trace.Witness{"detectedFirst": {Step: 3, DetectedStep: 3}}
	late := observedStep(8)
	late.Violations = []string{"armedFirst"}
	late.Witnesses = map[string]trace.Witness{"armedFirst": {Step: 1, DetectedStep: 8, IsError: true}}
	writeRunDirectory(t, seedDirectory, "20260812-090000", []trace.Step{observedStep(1), early, late})

	_, summary, err := summarizeRun(seedDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if summary.FirstViolationOriginStep == nil || *summary.FirstViolationOriginStep != 1 {
		t.Fatalf("origin step: got %v, want 1", summary.FirstViolationOriginStep)
	}
	if !summary.FirstViolationIsError {
		t.Error("is_error should come from the earliest-origin violation")
	}
	if !slices.Equal(summary.ViolatedProperties, []string{"armedFirst", "detectedFirst"}) {
		t.Errorf("violated properties: got %v", summary.ViolatedProperties)
	}
}

func TestSummarizeRun_FinalizeLineIsNotAStep(t *testing.T) {
	seedDirectory := t.TempDir()
	finalize := trace.Step{
		Index:      4,
		Timestamp:  time.Now().UTC(),
		Violations: []string{"eventuallySettles"},
		Witnesses:  map[string]trace.Witness{"eventuallySettles": {Step: 2, DetectedStep: 4}},
	}
	writeRunDirectory(t, seedDirectory, "20260812-090000", []trace.Step{
		observedStep(1), observedStep(2), observedStep(3), finalize,
	})

	_, summary, err := summarizeRun(seedDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Steps != 3 {
		t.Errorf("steps: got %d, want 3 (the finalize line is synthetic)", summary.Steps)
	}
	if summary.FirstViolationOriginStep == nil || *summary.FirstViolationOriginStep != 2 {
		t.Fatalf("origin step: got %v, want 2", summary.FirstViolationOriginStep)
	}
}

func TestSummarizeRun_FallsBackToStepIndexWithoutWitness(t *testing.T) {
	seedDirectory := t.TempDir()
	violating := observedStep(6)
	violating.Violations = []string{"noWitness"}
	writeRunDirectory(t, seedDirectory, "20260812-090000", []trace.Step{observedStep(5), violating})

	_, summary, err := summarizeRun(seedDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if summary.FirstViolationOriginStep == nil || *summary.FirstViolationOriginStep != 6 {
		t.Fatalf("origin step: got %v, want 6", summary.FirstViolationOriginStep)
	}
	if summary.FirstViolationDetectedStep == nil || *summary.FirstViolationDetectedStep != 6 {
		t.Fatalf("detected step: got %v, want 6", summary.FirstViolationDetectedStep)
	}
}

func TestSummarizeRun_PicksNewestRunDirectory(t *testing.T) {
	seedDirectory := t.TempDir()
	writeRunDirectory(t, seedDirectory, "20260812-090000", []trace.Step{observedStep(1)})
	writeRunDirectory(t, seedDirectory, "20260812-093000", []trace.Step{observedStep(1), observedStep(2)})

	name, summary, err := summarizeRun(seedDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if name != "20260812-093000" || summary.Steps != 2 {
		t.Errorf("got %q with %d steps", name, summary.Steps)
	}
}

func TestSummarizeRun_MissingRunDirectory(t *testing.T) {
	if _, _, err := summarizeRun(t.TempDir()); err == nil {
		t.Fatal("expected an error when no run directory was produced")
	}
}

func TestSummarizeTrace_MalformedLine(t *testing.T) {
	seedDirectory := t.TempDir()
	directory := writeRunDirectory(t, seedDirectory, "20260812-090000", []trace.Step{observedStep(1)})
	if err := os.WriteFile(filepath.Join(directory, "trace.jsonl"), []byte("{\"step\":1}\n{not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := summarizeRun(seedDirectory); err == nil {
		t.Fatal("expected an error for a malformed trace line")
	}
}
