package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
	"github.com/priyanshujain/sanderling/internal/trace"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

// fixtureSpec counts rows on screen and asserts three things about them: a
// bound a single observation can check, a reachability goal, and a growth step
// that only two consecutive observations can see.
const fixtureSpec = `
const rows = __sanderling__.extract((s) => s.ax.findAll({ text: "row" }).length).named("rows");
globalThis.properties = {
  fewRows: __sanderling__.always(() => rows.current < 3),
  rowsAppear: __sanderling__.eventually(() => rows.current > 0).within(500, "steps"),
  rowsKeepGrowing: __sanderling__.always(
    __sanderling__.now(() => rows.current === 1).implies(
      __sanderling__.next(() => rows.current === 2))),
};
`

func rowTree(t *testing.T, rows int) *hierarchy.Tree {
	t.Helper()
	children := make([]string, 0, rows)
	for range rows {
		children = append(
			children,
			`{"attributes": {"text": "row", "bounds": "[0,0,10,10]"}, "children": []}`,
		)
	}
	source := fmt.Sprintf(
		`{"attributes": {"resource-id": "root", "bounds": "[0,0,100,100]"}, "children": [%s]}`,
		strings.Join(children, ","),
	)
	tree, err := hierarchy.Parse(source)
	if err != nil {
		t.Fatalf("parse tree: %v", err)
	}
	return tree
}

// writeFixtureRun records a run the way internal/runner does: every step's
// hierarchy, the violations that fired at it, their witnesses, and the residual
// each property was left holding.
func writeFixtureRun(t *testing.T, directory string, rowCounts []int) {
	t.Helper()
	engine, err := verifier.New(verifier.WithPlatform("android"))
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	if err := engine.Load(fixtureSpec); err != nil {
		t.Fatalf("load spec: %v", err)
	}
	writer, err := trace.NewWriter(directory)
	if err != nil {
		t.Fatalf("trace writer: %v", err)
	}
	defer writer.Close()

	runStart := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	if err := writer.WriteMeta(trace.Meta{
		Seed:      11,
		SpecPath:  "fixture.ts",
		Platform:  "android",
		BundleID:  "com.example.fixture",
		StartedAt: runStart,
	}); err != nil {
		t.Fatalf("meta: %v", err)
	}

	lastIndex := 0
	for position, rows := range rowCounts {
		index := position + 1
		lastIndex = index
		stepTime := runStart.Add(time.Duration(index) * time.Second)
		tree := rowTree(t, rows)
		if err := engine.PushSnapshot(verifier.SnapshotInput{
			Tree:      tree,
			StepTime:  stepTime,
			StepIndex: index,
			RunStart:  runStart,
		}); err != nil {
			t.Fatalf("push step %d: %v", index, err)
		}
		engine.EvaluateProperties()
		violations := engine.NewlyViolatedProperties()
		step := trace.Step{
			Index:      index,
			Timestamp:  stepTime,
			Hierarchy:  tree,
			Violations: violations,
			Witnesses:  fixtureWitnesses(engine, violations, index),
			Residuals:  fixtureResiduals(t, engine),
		}
		if err := writer.WriteStep(step); err != nil {
			t.Fatalf("write step %d: %v", index, err)
		}
	}
	if ended := engine.Finalize(); len(ended) > 0 {
		if err := writer.WriteStep(trace.Step{
			Index:      lastIndex + 1,
			Timestamp:  runStart.Add(time.Duration(lastIndex+1) * time.Second),
			Violations: ended,
			Witnesses:  fixtureWitnesses(engine, ended, lastIndex+1),
		}); err != nil {
			t.Fatalf("write finalize step: %v", err)
		}
	}
}

func fixtureWitnesses(
	engine *verifier.Verifier,
	properties []string,
	index int,
) map[string]trace.Witness {
	if len(properties) == 0 {
		return nil
	}
	witnesses := map[string]trace.Witness{}
	for _, name := range properties {
		witness := engine.Witness(name)
		if witness == nil {
			continue
		}
		detected := witness.DetectedStep
		if detected == 0 {
			detected = index
		}
		witnesses[name] = trace.Witness{
			Reason:       witness.Reason,
			IsError:      witness.IsError,
			Step:         witness.Step,
			DetectedStep: detected,
			Extractors:   witness.Extractors,
		}
	}
	return witnesses
}

func fixtureResiduals(
	t *testing.T,
	engine *verifier.Verifier,
) map[string]json.RawMessage {
	t.Helper()
	residuals := map[string]json.RawMessage{}
	for name, formula := range engine.Residuals() {
		body, err := json.Marshal(formula)
		if err != nil {
			t.Fatalf("marshal residual %q: %v", name, err)
		}
		residuals[name] = body
	}
	return residuals
}

func replayFixture(t *testing.T, directory string) runReport {
	t.Helper()
	loaded, err := loadRun(directory)
	if err != nil {
		t.Fatalf("load run: %v", err)
	}
	report, err := replay(loaded, fixtureSpec, false)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	return report
}

func propertyByName(
	t *testing.T,
	report runReport,
	name string,
) propertyReport {
	t.Helper()
	for _, property := range report.Properties {
		if property.Property == name {
			return property
		}
	}
	t.Fatalf("property %q missing from the report", name)
	return propertyReport{}
}

func TestReplayReproducesTheRecordedVerdicts(t *testing.T) {
	directory := t.TempDir()
	writeFixtureRun(t, directory, []int{0, 1, 1, 3})

	report := replayFixture(t, directory)

	if !report.Valid {
		t.Fatalf("replay disagreed with the run: %+v", report.Mismatches)
	}
	if report.ResidualMismatches != 0 {
		t.Errorf("residual mismatches: %d", report.ResidualMismatches)
	}
	if report.StepsObserved != 4 {
		t.Errorf("steps observed: got %d, want 4", report.StepsObserved)
	}

	growth := propertyByName(t, report, "rowsKeepGrowing")
	if !growth.Engine.Refuted || growth.Engine.Step != 3 {
		t.Errorf("engine on rowsKeepGrowing: %+v", growth.Engine)
	}
	if growth.SingleState.Refuted {
		t.Errorf(
			"single-state refuted a growth step it cannot see: %+v",
			growth.SingleState,
		)
	}
	if !growth.SingleStep.Refuted {
		t.Error("single-step did not refute a one-step obligation")
	}
	if growth.Weakest != "single-step" {
		t.Errorf("weakest oracle for rowsKeepGrowing: got %q", growth.Weakest)
	}

	bound := propertyByName(t, report, "fewRows")
	if !bound.SingleState.Refuted || bound.Weakest != "single-state" {
		t.Errorf(
			"fewRows: single-state %+v weakest %q",
			bound.SingleState,
			bound.Weakest,
		)
	}
}

func TestReplayReportsAViolationTheRunNeverRecorded(t *testing.T) {
	directory := t.TempDir()
	writeFixtureRun(t, directory, []int{0, 1, 1, 3})
	dropRecordedViolation(t, directory, "rowsKeepGrowing")

	report := replayFixture(t, directory)

	if report.Valid {
		t.Fatal("a trace missing a recorded violation must not replay as valid")
	}
	var found bool
	for _, entry := range report.Mismatches {
		if entry.Property == "rowsKeepGrowing" && entry.Field == "violated" {
			found = true
		}
	}
	if !found {
		t.Errorf(
			"no mismatch named the dropped violation: %+v",
			report.Mismatches,
		)
	}
}

func TestReplayReportsAResidualTheRunNeverHeld(t *testing.T) {
	directory := t.TempDir()
	writeFixtureRun(t, directory, []int{0, 1, 1, 3})
	rewriteResidual(t, directory, 1, "fewRows", `{"op":"false"}`)

	report := replayFixture(t, directory)

	if report.Valid {
		t.Fatal(
			"a trace whose recorded residual differs must not replay as valid",
		)
	}
	if report.ResidualMismatches != 1 {
		t.Errorf(
			"residual mismatches: got %d, want 1",
			report.ResidualMismatches,
		)
	}
}

// dropRecordedViolation removes one property from every step's violations,
// which is what a trace that failed to record a verdict looks like.
func dropRecordedViolation(t *testing.T, directory, property string) {
	t.Helper()
	rewriteSteps(t, directory, func(step *trace.Step) {
		kept := step.Violations[:0]
		for _, name := range step.Violations {
			if name != property {
				kept = append(kept, name)
			}
		}
		step.Violations = kept
		delete(step.Witnesses, property)
	})
}

func rewriteResidual(
	t *testing.T,
	directory string,
	index int,
	property, residual string,
) {
	t.Helper()
	rewriteSteps(t, directory, func(step *trace.Step) {
		if step.Index == index {
			step.Residuals[property] = json.RawMessage(residual)
		}
	})
}

func rewriteSteps(t *testing.T, directory string, edit func(*trace.Step)) {
	t.Helper()
	path := filepath.Join(directory, "trace.jsonl")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	var rewritten strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		var step trace.Step
		if err := json.Unmarshal([]byte(line), &step); err != nil {
			t.Fatalf("decode step: %v", err)
		}
		edit(&step)
		encoded, err := json.Marshal(step)
		if err != nil {
			t.Fatalf("encode step: %v", err)
		}
		rewritten.Write(encoded)
		rewritten.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(rewritten.String()), 0o644); err != nil {
		t.Fatalf("write trace: %v", err)
	}
}

func TestCrashOnlyFiresOnAnErrorSurfaceAndOnLeavingTheForeground(t *testing.T) {
	run := loadedRun{Steps: []trace.Step{
		{Index: 1, Logs: []trace.LogEntry{{Level: "E", Message: "noisy"}}},
		{Index: 2, ActionSkipped: foregroundLossReason},
		{Index: 3, Exceptions: []trace.Exception{{Class: "TypeError"}}},
	}}

	report := crashOnly(run)

	if !report.Fired || report.FirstStep != 2 {
		t.Errorf("crash-only: %+v", report)
	}
	if len(report.ExceptionSteps) != 1 || report.ExceptionSteps[0] != 3 {
		t.Errorf("exception steps: %v", report.ExceptionSteps)
	}
	if len(report.ErrorLogSteps) != 1 || report.ErrorLogSteps[0] != 1 {
		t.Errorf("error log steps: %v", report.ErrorLogSteps)
	}
}

func TestCrashOnlyIgnoresAnErrorLogOnItsOwn(t *testing.T) {
	run := loadedRun{
		Steps: []trace.Step{{Index: 1, Logs: []trace.LogEntry{{Level: "E"}}}},
	}

	if crashOnly(run).Fired {
		t.Error("an error-level log line is not a crash")
	}
}

// A reachability goal the run reaches at the fourth observation is clean under
// the window its author wrote and refuted by the same window shortened to two
// observations. Reporting that shortened refutation would convict every clean
// trace, so the single-step column has to admit it cannot state the property.
func TestSingleStepDoesNotConvictACleanTraceOfATruncatedWindow(t *testing.T) {
	directory := t.TempDir()
	writeFixtureRun(t, directory, []int{0, 0, 0, 1})

	report := replayFixture(t, directory)

	if !report.Valid {
		t.Fatalf("replay disagreed with the run: %+v", report.Mismatches)
	}
	appear := propertyByName(t, report, "rowsAppear")
	if appear.Engine.Refuted {
		t.Fatalf(
			"the trace reaches the goal inside the 500-step window: %+v",
			appear.Engine,
		)
	}
	if appear.SingleStep.Refuted {
		t.Errorf(
			"single-step refuted a clean trace by shortening the window: %+v",
			appear.SingleStep,
		)
	}
	if !appear.SingleStep.CannotExpress {
		t.Error(
			"single-step must record a window it cannot state as inexpressible",
		)
	}
	if !appear.SingleStepTruncatesWindow {
		t.Error("the truncated-window marker must stay visible on the property")
	}
}

func TestSingleStateAdmitsWhatOneObservationCannotState(t *testing.T) {
	directory := t.TempDir()
	writeFixtureRun(t, directory, []int{0, 1, 1, 3})

	report := replayFixture(t, directory)

	growth := propertyByName(t, report, "rowsKeepGrowing")
	if !growth.SingleState.CannotExpress {
		t.Errorf(
			"single-state kept a next obligation it erases to nothing: %+v",
			growth.SingleState,
		)
	}
	appear := propertyByName(t, report, "rowsAppear")
	if !appear.SingleState.CannotExpress {
		t.Errorf(
			"single-state kept a reachability goal it erases to nothing: %+v",
			appear.SingleState,
		)
	}
	bound := propertyByName(t, report, "fewRows")
	if bound.SingleState.CannotExpress {
		t.Error("single-state states a bound read at one observation")
	}
	if !bound.SingleState.Refuted || bound.Weakest != "single-state" {
		t.Errorf(
			"fewRows: single-state %+v weakest %q",
			bound.SingleState,
			bound.Weakest,
		)
	}
}
