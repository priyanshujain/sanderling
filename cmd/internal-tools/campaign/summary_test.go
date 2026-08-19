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

func actingStep(index int) trace.Step {
	step := observedStep(index)
	step.NextAction = &trace.Action{Kind: "tap", X: 12, Y: 34}
	return step
}

func skippedActionStep(index int, reason string) trace.Step {
	step := actingStep(index)
	step.ActionSkipped = reason
	return step
}

func setupStep(index int) trace.Step {
	step := observedStep(index)
	step.NextAction = &trace.Action{
		Kind:     "InputText",
		Selector: "testTag:LoginScreen > testTag:LoginEmail",
		Text:     "demo@folio.app",
		Source:   trace.ActionSourceSetup,
	}
	return step
}

func seededStep(index int) trace.Step {
	step := actingStep(index)
	step.NextAction.Source = trace.ActionSourceSeeded
	return step
}

func modelStep(index int) trace.Step {
	step := actingStep(index)
	step.NextAction.Source = trace.ActionSourceModel
	return step
}

func skippedModelStep(index int, reason string) trace.Step {
	step := modelStep(index)
	step.ActionSkipped = reason
	return step
}

func writeRunDirectory(t *testing.T, seedDirectory, name string, steps []trace.Step) string {
	t.Helper()
	return writeRunDirectoryWithMeta(
		t,
		seedDirectory,
		name,
		trace.Meta{Seed: 11, Platform: "web", Arm: "seeded-baseline"},
		steps,
	)
}

func writeRunDirectoryWithMeta(
	t *testing.T,
	seedDirectory, name string,
	declared trace.Meta,
	steps []trace.Step,
) string {
	t.Helper()
	directory := filepath.Join(seedDirectory, name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	meta, err := json.Marshal(declared)
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

func TestSummarizeRun_CountsOnlyStepsThatDispatchedAnAction(t *testing.T) {
	seedDirectory := t.TempDir()
	writeRunDirectory(t, seedDirectory, "20260812-090000", []trace.Step{
		actingStep(1),
		observedStep(2),
		actingStep(3),
		skippedActionStep(4, "no_target"),
		skippedActionStep(5, "unresolved_selector"),
		skippedActionStep(6, "missing_key"),
		skippedActionStep(7, "zero_duration_wait"),
		skippedActionStep(8, "app_left_foreground"),
		skippedActionStep(9, "apply_error"),
		actingStep(10),
	})

	_, summary, err := summarizeRun(seedDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Steps != 10 {
		t.Errorf("steps: got %d, want 10", summary.Steps)
	}
	if summary.Actions != 3 {
		t.Errorf("actions: got %d, want 3 (one step chose nothing and six were never dispatched)", summary.Actions)
	}
}

func TestSummarizeRun_ModelRunLeavesTheSetupsLoginOutOfTheActionCount(t *testing.T) {
	seedDirectory := t.TempDir()
	writeRunDirectoryWithMeta(t, seedDirectory, "20260812-090000",
		trace.Meta{Seed: 11, Platform: "web", Arm: "llm-identifier", Generator: "llm"},
		[]trace.Step{
			setupStep(1),
			setupStep(2),
			setupStep(3),
			modelStep(4),
			skippedModelStep(5, "unresolved_selector"),
			modelStep(6),
		})

	_, summary, err := summarizeRun(seedDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Steps != 6 {
		t.Errorf("steps: got %d, want 6", summary.Steps)
	}
	if summary.Actions != 2 {
		t.Errorf("actions: got %d, want 2 (three login steps were setup's and one generator action was thrown away)", summary.Actions)
	}
}

func TestSummarizeRun_ModelRunWithoutSetupCountsEveryDispatchedStep(t *testing.T) {
	seedDirectory := t.TempDir()
	writeRunDirectoryWithMeta(t, seedDirectory, "20260812-090000",
		trace.Meta{Seed: 11, Platform: "web", Arm: "llm-identifier", Generator: "llm"},
		[]trace.Step{modelStep(1), modelStep(2), modelStep(3), modelStep(4)})

	_, summary, err := summarizeRun(seedDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Actions != 4 {
		t.Errorf("actions: got %d, want 4", summary.Actions)
	}
}

func TestSummarizeRun_SeededRunLeavesTheSetupsLoginOutOfTheActionCount(t *testing.T) {
	seedDirectory := t.TempDir()
	writeRunDirectoryWithMeta(t, seedDirectory, "20260812-090000",
		trace.Meta{Seed: 11, Platform: "web", Arm: "seeded-baseline", Generator: "seeded"},
		[]trace.Step{
			setupStep(1),
			setupStep(2),
			setupStep(3),
			seededStep(4),
			seededStep(5),
		})

	_, summary, err := summarizeRun(seedDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Actions != 2 {
		t.Errorf("actions: got %d, want 2 (three login steps were setup's), which is the same "+
			"rule the model arm is counted by", summary.Actions)
	}
	if summary.UnattributedActions != 0 {
		t.Errorf("unattributed actions: got %d, want 0: every step named its source",
			summary.UnattributedActions)
	}
}

func TestSummarizeRun_SeededRunWithoutSetupCountsEveryDispatchedStep(t *testing.T) {
	seedDirectory := t.TempDir()
	writeRunDirectoryWithMeta(t, seedDirectory, "20260812-090000",
		trace.Meta{Seed: 11, Platform: "web", Arm: "seeded-baseline", Generator: "seeded"},
		[]trace.Step{seededStep(1), seededStep(2), seededStep(3), seededStep(4)})

	_, summary, err := summarizeRun(seedDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Actions != 4 {
		t.Errorf("actions: got %d, want 4", summary.Actions)
	}
}

// Traces recorded before actions named their source cannot be re-attributed
// after the fact, so each arm keeps the count it was already reported with: the
// seeded arm counts every dispatched step, the model arm counts only what the
// model stamped. UnattributedActions is how such a run says so rather than
// passing its old denominator off as a setup-excluding one.
func TestSummarizeRun_TraceWithoutSourcesKeepsTheCountItWasReportedWith(t *testing.T) {
	for _, testCase := range []struct {
		generator           string
		actions             int
		unattributedActions int
	}{
		{generator: "seeded", actions: 5, unattributedActions: 5},
		{generator: "llm", actions: 0, unattributedActions: 5},
	} {
		t.Run(testCase.generator, func(t *testing.T) {
			seedDirectory := t.TempDir()
			writeRunDirectoryWithMeta(t, seedDirectory, "20260812-090000",
				trace.Meta{Seed: 11, Platform: "web", Generator: testCase.generator},
				[]trace.Step{
					actingStep(1), actingStep(2), actingStep(3), actingStep(4), actingStep(5),
				})

			_, summary, err := summarizeRun(seedDirectory)
			if err != nil {
				t.Fatal(err)
			}
			if summary.Actions != testCase.actions {
				t.Errorf("actions: got %d, want %d", summary.Actions, testCase.actions)
			}
			if summary.UnattributedActions != testCase.unattributedActions {
				t.Errorf("unattributed actions: got %d, want %d",
					summary.UnattributedActions, testCase.unattributedActions)
			}
		})
	}
}

func TestSummarizeRun_SkipReasonsEachSuppressTheAction(t *testing.T) {
	for _, reason := range []string{
		"no_target", "unresolved_selector", "missing_key",
		"zero_duration_wait", "app_left_foreground", "apply_error",
	} {
		seedDirectory := t.TempDir()
		writeRunDirectory(t, seedDirectory, "20260812-090000", []trace.Step{
			actingStep(1), skippedActionStep(2, reason),
		})
		_, summary, err := summarizeRun(seedDirectory)
		if err != nil {
			t.Fatal(err)
		}
		if summary.Actions != 1 {
			t.Errorf("%s: actions got %d, want 1", reason, summary.Actions)
		}
	}
}

func TestSummarizeTrace_NullActionIsNoAction(t *testing.T) {
	seedDirectory := t.TempDir()
	directory := writeRunDirectory(t, seedDirectory, "20260812-090000", []trace.Step{observedStep(1)})
	lines := "{\"step\":1,\"hierarchy\":{},\"next_action\":null}\n" +
		"{\"step\":2,\"hierarchy\":{},\"next_action\":{\"kind\":\"tap\"},\"action_skipped\":\"\"}\n"
	if err := os.WriteFile(filepath.Join(directory, "trace.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	_, summary, err := summarizeRun(seedDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Actions != 1 {
		t.Errorf("actions: got %d, want 1", summary.Actions)
	}
}

func TestSummarizeRun_FinalizeLineIsNotAnAction(t *testing.T) {
	seedDirectory := t.TempDir()
	finalize := trace.Step{
		Index:      3,
		Timestamp:  time.Now().UTC(),
		NextAction: &trace.Action{Kind: "tap"},
		Violations: []string{"eventuallySettles"},
	}
	writeRunDirectory(t, seedDirectory, "20260812-090000", []trace.Step{actingStep(1), actingStep(2), finalize})

	_, summary, err := summarizeRun(seedDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Actions != 2 {
		t.Errorf("actions: got %d, want 2", summary.Actions)
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

// A run whose app never came to the foreground has to be countable off the
// summary. Without it the campaign row for a run that never started is a row of
// zero steps and no violations, which is what a clean short run looks like too.
func TestSummarizeRun_CountsPreconditionFailures(t *testing.T) {
	seedDirectory := t.TempDir()
	writeRunDirectory(t, seedDirectory, "20260812-090000", []trace.Step{
		{Index: 0, PreconditionFailure: "app_not_in_foreground"},
	})

	_, summary, err := summarizeRun(seedDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if summary.PreconditionFailures != 1 {
		t.Errorf("precondition failures: got %d, want 1", summary.PreconditionFailures)
	}
	if summary.Steps != 0 {
		t.Errorf("steps: got %d, want 0; the run never observed anything", summary.Steps)
	}
}

// The same fact mid-run: steps the scope guard could not bring the app back for
// are still steps, and they are counted separately from the ones that explored.
func TestSummarizeRun_CountsMidRunPreconditionFailures(t *testing.T) {
	seedDirectory := t.TempDir()
	outsideApp := func(index int) trace.Step {
		step := actingStep(index)
		step.PreconditionFailure = "app_not_in_foreground"
		return step
	}
	writeRunDirectory(t, seedDirectory, "20260812-090000", []trace.Step{
		actingStep(1), outsideApp(2), outsideApp(3), actingStep(4),
	})

	_, summary, err := summarizeRun(seedDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if summary.PreconditionFailures != 2 {
		t.Errorf("precondition failures: got %d, want 2", summary.PreconditionFailures)
	}
	if summary.Steps != 4 {
		t.Errorf("steps: got %d, want 4", summary.Steps)
	}
}
