package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/priyanshujain/sanderling/internal/bundler"
	"github.com/priyanshujain/sanderling/internal/driver"
	mockdriver "github.com/priyanshujain/sanderling/internal/driver/mock"
	"github.com/priyanshujain/sanderling/internal/hierarchy"
	"github.com/priyanshujain/sanderling/internal/trace"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

const fixtureSpec = `
import { actions, always, extract, Tap } from "@sanderling/spec";
const balance = extract(state => state.snapshots.balance ?? 0);
globalThis.properties = {
  balanceNonNegative: always(() => balance.current >= 0),
};
globalThis.actions = actions(() => [Tap({ on: "id:next" })]);
`

const violationSpec = `
import { actions, always } from "@sanderling/spec";
globalThis.properties = {
  balanceNonNegative: always(() => false),
};
globalThis.actions = actions(() => []);
`

type harness struct {
	mock     *mockdriver.Driver
	verifier *verifier.Verifier
	writer   *trace.Writer
}

func newHarness(t *testing.T) *harness {
	return newHarnessWithSpec(t, fixtureSpec)
}

func fastFocusSettle(t *testing.T) {
	prev := focusTapSettle
	focusTapSettle = time.Millisecond
	t.Cleanup(func() { focusTapSettle = prev })
}

// bundleSpec compiles an authored TS spec with the goja runtime entry so the
// loaded bundle installs __sanderlingNextAction__ (the shared picker).
func bundleSpec(t *testing.T, specSource string) string {
	t.Helper()
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.ts")
	if err := os.WriteFile(specPath, []byte(specSource), 0o600); err != nil {
		t.Fatal(err)
	}
	apiPath, err := filepath.Abs("../../pkg/spec/src/index.ts")
	if err != nil {
		t.Fatal(err)
	}
	runtimePath, err := filepath.Abs("../../pkg/spec/src/goja-runtime.ts")
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := bundler.Bundle(bundler.Options{
		EntryFile:   specPath,
		RuntimeFile: runtimePath,
		Aliases:     map[string]string{"@sanderling/spec": apiPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(bundle.JavaScript)
}

func newHarnessWithSpec(t *testing.T, spec string) *harness {
	t.Helper()
	directory := t.TempDir()
	writer, err := trace.NewWriter(directory)
	if err != nil {
		t.Fatal(err)
	}
	verifierInstance, err := verifier.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := verifierInstance.Load(bundleSpec(t, spec)); err != nil {
		t.Fatal(err)
	}
	state := &harness{
		mock:     mockdriver.New(),
		verifier: verifierInstance,
		writer:   writer,
	}
	t.Cleanup(func() { _ = writer.Close() })
	return state
}

func TestRunner_HappyPathStepsAndTraces(t *testing.T) {
	state := newHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    100 * time.Millisecond,
		IdleTimeout: 50 * time.Millisecond,
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Steps == 0 {
		t.Errorf("expected at least one step, got 0")
	}
	if len(summary.Violations) != 0 {
		t.Errorf("no violations expected, got %v", summary.Violations)
	}
	// Every builtin verb is supported on every platform, so a clean run must
	// report no unsupported verbs (the runner still wires the field through).
	if len(summary.UnsupportedVerbs) != 0 {
		t.Errorf("expected no unsupported verbs, got %v", summary.UnsupportedVerbs)
	}

	actions := state.mock.Actions()
	if !containsAction(actions, mockdriver.ActionTapSelector, "id:next") {
		t.Errorf("expected TapSelector with id:next, got %v", actions)
	}
}

func TestRunner_MaxStepsStopsAfterExactlyNSteps(t *testing.T) {
	state := newHarness(t)

	const maxSteps = 3
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// A long duration ensures MaxSteps, not the deadline, ends the run.
	summary, err := Run(ctx, Options{
		Duration:    time.Hour,
		IdleTimeout: 10 * time.Millisecond,
		MaxSteps:    maxSteps,
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Steps != maxSteps {
		t.Errorf("expected exactly %d steps, got %d", maxSteps, summary.Steps)
	}
}

// TestRenderSummary_SurfacesUnsupportedVerbs exercises the real path that takes
// verbs the picker requested but the platform cannot dispatch and puts them in
// front of the operator. TestRunner_HappyPath only ever asserts the field stays
// empty (every builtin is supported, so its non-empty arm can never fire), so
// without this the "unsupported on %s: ..." branch could be deleted and every
// unsupported-verb regression would pass silently.
func TestRenderSummary_SurfacesUnsupportedVerbs(t *testing.T) {
	summary := Summary{Steps: 3, UnsupportedVerbs: []string{"longPresses", "scrolls"}}

	var out bytes.Buffer
	RenderSummary(&out, summary, "ios")

	if !strings.Contains(out.String(), "unsupported on ios: longPresses, scrolls") {
		t.Errorf("expected unsupported verbs line for ios, got:\n%s", out.String())
	}
}

// TestRenderSummary_OmitsUnsupportedLineWhenNone guards the inverse: a clean run
// must not print a stray "unsupported on" line, so a future refactor cannot
// start emitting an empty list and alarm the operator on every run.
func TestRenderSummary_OmitsUnsupportedLineWhenNone(t *testing.T) {
	var out bytes.Buffer
	RenderSummary(&out, Summary{Steps: 2}, "android")

	if strings.Contains(out.String(), "unsupported on") {
		t.Errorf("did not expect an unsupported-verbs line, got:\n%s", out.String())
	}
}

func TestRunner_ViolationSurfacesInSummary(t *testing.T) {
	state := newHarnessWithSpec(t, violationSpec)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    100 * time.Millisecond,
		IdleTimeout: 50 * time.Millisecond,
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(summary.Violations) == 0 {
		t.Errorf("expected at least one violation, got %v", summary.Violations)
	}
	if !containsProperty(summary.Violations, "balanceNonNegative") {
		t.Errorf("expected balanceNonNegative in violations: %v", summary.Violations)
	}
}

func TestRunner_ViolationSurfacesOnlyOnOnsetStep(t *testing.T) {
	// violationSpec uses always(() => false): onset fires on step 1 and the
	// residual stays violated forever. The runner must record the violation
	// exactly once (at the onset step) in both summary.Violations and trace
	// lines, not on every subsequent step.
	state := newHarnessWithSpec(t, violationSpec)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    200 * time.Millisecond,
		IdleTimeout: 20 * time.Millisecond,
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Steps < 2 {
		t.Fatalf("need at least 2 steps to prove onset-only behavior, got %d", summary.Steps)
	}
	if len(summary.Violations) != 1 {
		t.Fatalf("expected exactly one ViolationRecord (onset only), got %d: %v",
			len(summary.Violations), summary.Violations)
	}
	if summary.Violations[0].StepIndex != 1 {
		t.Errorf("onset step: got %d, want 1 (always(()=>false) violates immediately)",
			summary.Violations[0].StepIndex)
	}
	if !slices.Equal(summary.Violations[0].Properties, []string{"balanceNonNegative"}) {
		t.Errorf("onset properties: got %v, want [balanceNonNegative]",
			summary.Violations[0].Properties)
	}

	file, err := os.Open(filepath.Join(state.writer.Directory(), "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	type traceLine struct {
		Step       int      `json:"step"`
		Violations []string `json:"violations"`
	}
	linesWithViolations := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var line traceLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("trace line decode: %v", err)
		}
		if len(line.Violations) == 0 {
			continue
		}
		linesWithViolations++
		if line.Step != 1 {
			t.Errorf("step %d unexpectedly emitted violations %v (should be onset-only at step 1)",
				line.Step, line.Violations)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	if linesWithViolations != 1 {
		t.Errorf("expected exactly 1 trace line with violations, got %d", linesWithViolations)
	}
}

func TestRunner_NextViolationAttributedToCausingStep(t *testing.T) {
	// always(next(p)): the obligation spawned at step 2 fails against step 3's
	// state. The summary record and the trace witness must attribute the
	// violation to step 2 (the causing step); the trace line that carries it is
	// still step 3, where the failure was detected.
	const nextViolationSpec = `
import { actions, always, next, extract } from "@sanderling/spec";
let observed = 0;
const tick = extract(() => ++observed);
globalThis.properties = {
  nextHolds: always(next(() => tick.current < 3)),
};
globalThis.actions = actions(() => []);
`
	state := newHarnessWithSpec(t, nextViolationSpec)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    time.Hour,
		IdleTimeout: 20 * time.Millisecond,
		MaxSteps:    5,
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(summary.Violations) != 1 {
		t.Fatalf("expected exactly one ViolationRecord, got %d: %v",
			len(summary.Violations), summary.Violations)
	}
	if summary.Violations[0].StepIndex != 2 {
		t.Errorf("summary step: got %d, want 2 (the step that spawned the next obligation)",
			summary.Violations[0].StepIndex)
	}
	if !slices.Equal(summary.Violations[0].Properties, []string{"nextHolds"}) {
		t.Errorf("properties: got %v, want [nextHolds]", summary.Violations[0].Properties)
	}

	file, err := os.Open(filepath.Join(state.writer.Directory(), "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	type traceLine struct {
		Step       int                      `json:"step"`
		Violations []string                 `json:"violations"`
		Witnesses  map[string]trace.Witness `json:"witnesses"`
	}
	found := false
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var line traceLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("trace line decode: %v", err)
		}
		if len(line.Violations) == 0 {
			continue
		}
		found = true
		if line.Step != 3 {
			t.Errorf("violation detected on step %d, want 3", line.Step)
		}
		witness, ok := line.Witnesses["nextHolds"]
		if !ok {
			t.Fatalf("step %d carries no witness for nextHolds", line.Step)
		}
		if witness.Step != 2 {
			t.Errorf("witness step: got %d, want 2 (causing step)", witness.Step)
		}
		// The two indices the witness spans are recorded separately: the
		// extractor snapshot it carries is step 3's state, not step 2's.
		if witness.DetectedStep != 3 {
			t.Errorf("witness detected step: got %d, want 3", witness.DetectedStep)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	if !found {
		t.Error("no trace line carried the violation")
	}
}

func TestRunner_AlwaysNextLeavesNoEndOfRunViolation(t *testing.T) {
	// always(next(p)) ends every run with a pending deferred check. That
	// residue is vacuous (no successor state to check), so neither the
	// summary nor the trace may report an end-of-run violation.
	const spec = `
import { actions, always, next } from "@sanderling/spec";
globalThis.properties = {
  nextHolds: always(next(() => true)),
};
globalThis.actions = actions(() => []);
`
	state := newHarnessWithSpec(t, spec)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    time.Hour,
		IdleTimeout: 20 * time.Millisecond,
		MaxSteps:    3,
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(summary.Violations) != 0 {
		t.Errorf("expected no violations, got %v", summary.Violations)
	}
}

func TestRunner_FinalizeRecordUsesDistinctStepIndex(t *testing.T) {
	// An eventually that never fires is reported at run end through a
	// synthetic trace record. That record must carry its own step index so no
	// two trace lines share one (duplicate indices made the replay UI select
	// two rows at once).
	const spec = `
import { actions, eventually } from "@sanderling/spec";
globalThis.properties = {
  neverFires: eventually(() => false),
};
globalThis.actions = actions(() => []);
`
	state := newHarnessWithSpec(t, spec)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    time.Hour,
		IdleTimeout: 20 * time.Millisecond,
		MaxSteps:    3,
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !containsProperty(summary.Violations, "neverFires") {
		t.Fatalf("expected neverFires in violations: %v", summary.Violations)
	}

	file, err := os.Open(filepath.Join(state.writer.Directory(), "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	type traceLine struct {
		Step       int      `json:"step"`
		Violations []string `json:"violations"`
	}
	seen := map[int]bool{}
	finalizeStep := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var line traceLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("trace line decode: %v", err)
		}
		if seen[line.Step] {
			t.Errorf("duplicate step index %d in trace", line.Step)
		}
		seen[line.Step] = true
		if slices.Contains(line.Violations, "neverFires") {
			finalizeStep = line.Step
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	if finalizeStep != summary.Steps+1 {
		t.Errorf("finalize record step = %d, want %d (steps+1)", finalizeStep, summary.Steps+1)
	}
}

func TestRunner_ThrowingPredicateIsLoggedNotPanic(t *testing.T) {
	const throwingSpec = `
import { actions, always, Tap } from "@sanderling/spec";
globalThis.properties = {
  broken: always(() => { throw new Error("bad predicate"); }),
};
globalThis.actions = actions(() => [Tap({ on: "id:next" })]);
`
	state := newHarnessWithSpec(t, throwingSpec)

	var buffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelWarn}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    100 * time.Millisecond,
		IdleTimeout: 50 * time.Millisecond,
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
		Logger:      logger,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !containsProperty(summary.Violations, "broken") {
		t.Errorf("expected broken in violations: %v", summary.Violations)
	}
	if !strings.Contains(buffer.String(), "bad predicate") {
		t.Errorf("expected predicate error in log, got %q", buffer.String())
	}
}

func TestRunner_RejectsMissingFields(t *testing.T) {
	_, err := Run(context.Background(), Options{Duration: time.Second})
	if err == nil || !strings.Contains(err.Error(), "Driver") {
		t.Errorf("expected Driver-required error, got %v", err)
	}
}

func TestRunner_RejectsZeroDuration(t *testing.T) {
	_, err := Run(context.Background(), Options{
		Driver:      mockdriver.New(),
		Verifier:    mustNewVerifier(t),
		TraceWriter: mustNewTraceWriter(t),
	})
	if err == nil || !strings.Contains(err.Error(), "Duration") {
		t.Errorf("expected Duration-required error, got %v", err)
	}
}

func TestRunner_StampsHierarchyResolvedBoundsAndResiduals(t *testing.T) {
	state := newHarness(t)
	state.mock.HierarchyJSON = `{"attributes":{"resource-id":"com.fixture:id/next","bounds":"[40,80,240,160]"},"children":[],"clickable":true,"enabled":true}`

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Run(ctx, Options{
		Duration:    100 * time.Millisecond,
		IdleTimeout: 50 * time.Millisecond,
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(state.writer.Directory(), "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `"selector":"id:next"`) {
		t.Errorf("expected selector in trace: %s", text)
	}
	if !strings.Contains(text, `"resolved_bounds":{"x":40,"y":80,"width":200,"height":80}`) {
		t.Errorf("expected resolved_bounds in trace: %s", text)
	}
	if !strings.Contains(text, `"tap_point":{"x":140,"y":120}`) {
		t.Errorf("expected tap_point in trace: %s", text)
	}
	if !strings.Contains(text, `"hierarchy":{"elements":`) {
		t.Errorf("expected hierarchy in trace: %s", text)
	}
	if !strings.Contains(text, `"residuals":{`) {
		t.Errorf("expected residuals in trace: %s", text)
	}
}

// TestTraceActionFor_StaleCoordinatesDoNotOverrideTreeCenter pins the stamp
// to applyAction's resolution rule: when On resolves in the tree, the trace
// tap point must be the tree center even if the action carries stale X/Y
// from an earlier tick.
func TestTraceActionFor_StaleCoordinatesDoNotOverrideTreeCenter(t *testing.T) {
	tree, err := hierarchy.Parse(`{"attributes":{"resource-id":"root","bounds":"[0,0,1080,2340]"},"children":[
		{"attributes":{"resource-id":"next","bounds":"[100,200,300,400]"},"children":[]}
	]}`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	action := verifier.Action{Kind: verifier.ActionKindTap, On: "id:next", X: 50, Y: 60}

	traceAction := traceActionFor(action, tree)
	if traceAction.TapPoint == nil {
		t.Fatal("expected a tap point")
	}
	if traceAction.TapPoint.X != 200 || traceAction.TapPoint.Y != 300 {
		t.Errorf("tap point = (%d,%d), want tree center (200,300)",
			traceAction.TapPoint.X, traceAction.TapPoint.Y)
	}
	if traceAction.ResolvedBounds == nil {
		t.Fatal("expected resolved bounds")
	}
	if traceAction.ResolvedBounds.X != 100 || traceAction.ResolvedBounds.Y != 200 {
		t.Errorf("resolved bounds origin = (%d,%d), want (100,200)",
			traceAction.ResolvedBounds.X, traceAction.ResolvedBounds.Y)
	}
}

// TestTraceActionFor_RecordsKindSpecificFields locks each action kind's trace
// encoding. PressKey must carry its Key and Wait its DurationMillis; if either
// branch of traceActionFor drops the field (or a field rename desyncs from the
// trace.Action struct) the replay UI silently renders a key-less PressKey or a
// zero-duration Wait. Swipe's endpoint encoding is covered separately via
// applyAction (TestApplyAction_ScrollWithPrecomputedEndpointsSwipes).
func TestTraceActionFor_RecordsKindSpecificFields(t *testing.T) {
	cases := []struct {
		name   string
		action verifier.Action
		check  func(*testing.T, *trace.Action)
	}{
		{
			"PressKey records key",
			verifier.Action{Kind: verifier.ActionKindPressKey, Key: "back"},
			func(t *testing.T, a *trace.Action) {
				if a.Key != "back" {
					t.Errorf("Key = %q, want %q", a.Key, "back")
				}
			},
		},
		{
			"Wait records duration",
			verifier.Action{Kind: verifier.ActionKindWait, DurationMillis: 250},
			func(t *testing.T, a *trace.Action) {
				if a.DurationMillis != 250 {
					t.Errorf("DurationMillis = %d, want 250", a.DurationMillis)
				}
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			traceAction := traceActionFor(testCase.action, nil)
			if traceAction.Kind != string(testCase.action.Kind) {
				t.Errorf("Kind = %q, want %q", traceAction.Kind, testCase.action.Kind)
			}
			testCase.check(t, traceAction)
		})
	}
}

func TestRunner_LogsWaitForIdleDriverErrors(t *testing.T) {
	state := newHarness(t)
	state.mock.Failures[mockdriver.ActionWaitForIdle] = errors.New("sidecar lost gRPC stream")

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Run(ctx, Options{
		Duration:    100 * time.Millisecond,
		IdleTimeout: 50 * time.Millisecond,
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
		Logger:      logger,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	output := logBuf.String()
	if !strings.Contains(output, "wait_for_idle failed") {
		t.Errorf("expected wait_for_idle warning, got: %q", output)
	}
	if !strings.Contains(output, "sidecar lost gRPC stream") {
		t.Errorf("expected driver error message in warning, got: %q", output)
	}
}

func TestApplyAction_InputTextErasesExistingTextBeforeTyping(t *testing.T) {
	fastFocusSettle(t)
	tree, err := hierarchy.Parse(`{"attributes":{"resource-id":"root","bounds":"[0,0,1080,2340]"},"children":[
		{"attributes":{"resource-id":"username","text":"stale-value","bounds":"[10,10,500,100]"},"children":[]}
	]}`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	driverMock := mockdriver.New()
	action := verifier.Action{Kind: verifier.ActionKindInputText, On: "id:username", Text: "alice"}

	if err := applyAction(context.Background(), driverMock, action, tree); err != nil {
		t.Fatalf("applyAction: %v", err)
	}
	// The post-tap settle is now a brief internal sleep, not a WaitForIdle RPC,
	// so the recorded driver actions are tap, erase, input.
	actions := driverMock.Actions()
	if len(actions) != 3 {
		t.Fatalf("want tap, erase, input; got %v", actions)
	}
	if actions[0].Kind != mockdriver.ActionTap {
		t.Errorf("first action = %v, want tap", actions[0].Kind)
	}
	if actions[1].Kind != mockdriver.ActionEraseText || actions[1].CharacterCount != len("stale-value") {
		t.Errorf("second action = %+v, want erase_text of %d characters", actions[1], len("stale-value"))
	}
	if actions[2].Kind != mockdriver.ActionInputText || actions[2].Text != "alice" {
		t.Errorf("third action = %+v, want input_text alice", actions[2])
	}
}

// TestApplyAction_InputTextWithoutTargetSkipsFocusTap pins that with no
// resolvable target there is no focus tap (and so no settle), and InputText
// still runs at the cursor.
func TestApplyAction_InputTextWithoutTargetSkipsFocusTap(t *testing.T) {
	driverMock := mockdriver.New()
	action := verifier.Action{Kind: verifier.ActionKindInputText, X: -1, Y: -1, Text: "alice"}

	if err := applyAction(context.Background(), driverMock, action, nil); err != nil {
		t.Fatalf("applyAction: %v", err)
	}
	actions := driverMock.Actions()
	if len(actions) != 1 || actions[0].Kind != mockdriver.ActionInputText {
		t.Errorf("no target: want input_text only (no focus tap), got %v", actions)
	}
}

// TestApplyAction_InputTextSkipsEraseForReplacingDriver pins that a driver
// asserting the TextReplacer capability never pays the pre-erase round-trip:
// its InputText already replaces the field's content.
func TestApplyAction_InputTextSkipsEraseForReplacingDriver(t *testing.T) {
	fastFocusSettle(t)
	tree, err := hierarchy.Parse(`{"attributes":{"resource-id":"root","bounds":"[0,0,1080,2340]"},"children":[
		{"attributes":{"resource-id":"username","text":"stale-value","bounds":"[10,10,500,100]"},"children":[]}
	]}`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	driverMock := mockdriver.New()
	driverMock.ReplacesText = true
	action := verifier.Action{Kind: verifier.ActionKindInputText, On: "id:username", Text: "alice"}

	if err := applyAction(context.Background(), driverMock, action, tree); err != nil {
		t.Fatalf("applyAction: %v", err)
	}
	if containsAction(driverMock.Actions(), mockdriver.ActionEraseText, "") {
		t.Errorf("replacing driver must not be asked to erase: %v", driverMock.Actions())
	}
	if !containsAction(driverMock.Actions(), mockdriver.ActionInputText, "") {
		t.Errorf("expected InputText, got %v", driverMock.Actions())
	}
}

func TestApplyAction_InputTextSkipsEraseWhenTargetEmpty(t *testing.T) {
	fastFocusSettle(t)
	tree, err := hierarchy.Parse(`{"attributes":{"resource-id":"root","bounds":"[0,0,1080,2340]"},"children":[
		{"attributes":{"resource-id":"username","bounds":"[10,10,500,100]"},"children":[]}
	]}`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	driverMock := mockdriver.New()
	action := verifier.Action{Kind: verifier.ActionKindInputText, On: "id:username", Text: "alice"}

	if err := applyAction(context.Background(), driverMock, action, tree); err != nil {
		t.Fatalf("applyAction: %v", err)
	}
	if containsAction(driverMock.Actions(), mockdriver.ActionEraseText, "") {
		t.Errorf("empty field must not be erased: %v", driverMock.Actions())
	}
}

func TestApplyAction_InputTextSurfacesFocusTapError(t *testing.T) {
	t.Run("selector focus tap fails", func(t *testing.T) {
		driverMock := mockdriver.New()
		driverMock.Failures[mockdriver.ActionTapSelector] = errors.New("adb unreachable")
		action := verifier.Action{Kind: verifier.ActionKindInputText, On: "id:username", Text: "alice"}

		err := applyAction(context.Background(), driverMock, action, nil)
		if err == nil {
			t.Fatalf("expected focus tap failure to surface, got nil")
		}
		if containsAction(driverMock.Actions(), mockdriver.ActionInputText, "") {
			t.Errorf("InputText must not run after focus tap failed: %v", driverMock.Actions())
		}
	})
	t.Run("coordinate focus tap fails", func(t *testing.T) {
		driverMock := mockdriver.New()
		driverMock.Failures[mockdriver.ActionTap] = errors.New("tap driver error")
		action := verifier.Action{Kind: verifier.ActionKindInputText, X: 10, Y: 20, Text: "alice"}

		err := applyAction(context.Background(), driverMock, action, nil)
		if err == nil {
			t.Fatalf("expected focus tap failure to surface, got nil")
		}
		if containsAction(driverMock.Actions(), mockdriver.ActionInputText, "") {
			t.Errorf("InputText must not run after focus tap failed: %v", driverMock.Actions())
		}
	})
}

func TestApplyAction_V8InputTextTapsAtCoordinates(t *testing.T) {
	fastFocusSettle(t)
	driverMock := mockdriver.New()
	action := verifier.Action{Kind: verifier.ActionKindInputText, X: 50, Y: 100, Text: "alice"}

	if err := applyAction(context.Background(), driverMock, action, nil); err != nil {
		t.Fatalf("apply action: %v", err)
	}
	actions := driverMock.Actions()
	if !containsAction(actions, mockdriver.ActionTap, "") {
		t.Errorf("expected focus Tap before InputText, got %v", actions)
	}
	if !containsAction(actions, mockdriver.ActionInputText, "") {
		t.Errorf("expected InputText after focus Tap, got %v", actions)
	}
}

func TestApplyAction_V8InputTextAtOriginStillTaps(t *testing.T) {
	fastFocusSettle(t)
	driverMock := mockdriver.New()
	// V8 emits real (0,0) coordinates for an element at viewport top-left
	// (post-#15 the runtime nullifies unresolved actions, so a non-null
	// InputText with (0,0) is a deliberate edge tap, not a sentinel).
	action := verifier.Action{Kind: verifier.ActionKindInputText, X: 0, Y: 0, Text: "alice"}

	if err := applyAction(context.Background(), driverMock, action, nil); err != nil {
		t.Fatalf("apply action: %v", err)
	}
	if !containsAction(driverMock.Actions(), mockdriver.ActionTap, "") {
		t.Errorf("expected focus Tap at (0,0), got %v", driverMock.Actions())
	}
}

func TestApplyAction_DoubleTapDispatchesDoubleTapAtCoordinates(t *testing.T) {
	driverMock := mockdriver.New()
	action := verifier.Action{Kind: verifier.ActionKindDoubleTap, X: 100, Y: 200}

	if err := applyAction(context.Background(), driverMock, action, nil); err != nil {
		t.Fatalf("apply action: %v", err)
	}
	taps := 0
	for _, a := range driverMock.Actions() {
		if a.Kind == mockdriver.ActionDoubleTap && a.X == 100 && a.Y == 200 {
			taps++
		}
	}
	if taps != 1 {
		t.Errorf("expected 1 DoubleTap call at (100,200), got %d in %v", taps, driverMock.Actions())
	}
}

func TestApplyAction_DoubleTapDispatchesDoubleTapSelector(t *testing.T) {
	driverMock := mockdriver.New()
	action := verifier.Action{Kind: verifier.ActionKindDoubleTap, On: "id:save"}

	if err := applyAction(context.Background(), driverMock, action, nil); err != nil {
		t.Fatalf("apply action: %v", err)
	}
	taps := 0
	for _, a := range driverMock.Actions() {
		if a.Kind == mockdriver.ActionDoubleTapSelector && a.Selector == "id:save" {
			taps++
		}
	}
	if taps != 1 {
		t.Errorf("expected 1 DoubleTapSelector call with id:save, got %d in %v", taps, driverMock.Actions())
	}
}

func TestApplyAction_LongPressDispatchesAtResolvedCoordinates(t *testing.T) {
	driverMock := mockdriver.New()
	action := verifier.Action{Kind: verifier.ActionKindLongPress, X: 120, Y: 240}

	if err := applyAction(context.Background(), driverMock, action, nil); err != nil {
		t.Fatalf("apply action: %v", err)
	}
	found := false
	for _, a := range driverMock.Actions() {
		if a.Kind == mockdriver.ActionLongPress && a.X == 120 && a.Y == 240 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected LongPress at (120,240), got %v", driverMock.Actions())
	}
}

func TestApplyAction_ScrollWithPrecomputedEndpointsSwipes(t *testing.T) {
	driverMock := mockdriver.New()
	action := verifier.Action{
		Kind:           verifier.ActionKindScroll,
		Direction:      "down",
		FromX:          100,
		FromY:          500,
		ToX:            100,
		ToY:            300,
		DurationMillis: 300,
	}

	if err := applyAction(context.Background(), driverMock, action, nil); err != nil {
		t.Fatalf("apply action: %v", err)
	}
	found := false
	for _, a := range driverMock.Actions() {
		if a.Kind == mockdriver.ActionSwipe && a.FromX == 100 && a.FromY == 500 && a.ToX == 100 && a.ToY == 300 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Swipe with precomputed endpoints, got %v", driverMock.Actions())
	}
}

func TestApplyAction_ScrollDirectionUsesInversion(t *testing.T) {
	driverMock := mockdriver.New()
	treeJSON := `{"attributes":{"resource-id":"com.fixture:id/list","bounds":"[0,0,400,800]"},"children":[],"enabled":true}`
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatalf("parse tree: %v", err)
	}
	action := verifier.Action{Kind: verifier.ActionKindScroll, Direction: "down", On: "id:list"}

	if err := applyAction(context.Background(), driverMock, action, tree); err != nil {
		t.Fatalf("apply action: %v", err)
	}
	var swipe *mockdriver.Action
	for i := range driverMock.Actions() {
		if driverMock.Actions()[i].Kind == mockdriver.ActionSwipe {
			a := driverMock.Actions()[i]
			swipe = &a
		}
	}
	if swipe == nil {
		t.Fatalf("expected a Swipe, got %v", driverMock.Actions())
	}
	// "down" reveals lower content by dragging the finger up, so toY < fromY.
	if swipe.ToY >= swipe.FromY {
		t.Errorf("expected toY < fromY for scroll down, got from=%d to=%d", swipe.FromY, swipe.ToY)
	}
}

func TestApplyAction_ScrollNearTopKeepsDirectionAfterClamp(t *testing.T) {
	driverMock := mockdriver.New()
	// Full-screen root sets the 1080x2400 screen (marginY=200); the scrollable
	// list sits inside the top margin (y 20..180), where the clamp must fire.
	treeJSON := `{"attributes":{"bounds":"[0,0,1080,2400]"},"children":[
		{"attributes":{"resource-id":"com.fixture:id/toplist","scrollable":"true","bounds":"[0,20,1080,180]"},"children":[],"enabled":true}
	]}`
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatalf("parse tree: %v", err)
	}
	action := verifier.Action{Kind: verifier.ActionKindScroll, Direction: "up", On: "id:toplist"}

	if err := applyAction(context.Background(), driverMock, action, tree); err != nil {
		t.Fatalf("apply action: %v", err)
	}
	var swipe *mockdriver.Action
	for i := range driverMock.Actions() {
		if driverMock.Actions()[i].Kind == mockdriver.ActionSwipe {
			a := driverMock.Actions()[i]
			swipe = &a
		}
	}
	if swipe == nil {
		t.Fatalf("expected a Swipe, got %v", driverMock.Actions())
	}
	if swipe.FromY != 200 {
		t.Errorf("origin not pushed below the shade strip, got fromY=%d want 200", swipe.FromY)
	}
	if swipe.ToY <= swipe.FromY {
		t.Errorf("scroll up reversed by the clamp: from=%d to=%d (want toY > fromY)", swipe.FromY, swipe.ToY)
	}
}

func TestApplyAction_ScrollScreenFallback(t *testing.T) {
	driverMock := mockdriver.New()
	treeJSON := `{"attributes":{"bounds":"[0,0,400,800]"},"children":[],"enabled":true}`
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatalf("parse tree: %v", err)
	}
	// On unset: container falls back to whole-screen (root) bounds.
	action := verifier.Action{Kind: verifier.ActionKindScroll, Direction: "up"}

	if err := applyAction(context.Background(), driverMock, action, tree); err != nil {
		t.Fatalf("apply action: %v", err)
	}
	var swipe *mockdriver.Action
	for i := range driverMock.Actions() {
		if driverMock.Actions()[i].Kind == mockdriver.ActionSwipe {
			a := driverMock.Actions()[i]
			swipe = &a
		}
	}
	if swipe == nil {
		t.Fatalf("expected a Swipe, got %v", driverMock.Actions())
	}
	if swipe.FromX != 200 || swipe.FromY != 400 {
		t.Errorf("expected swipe to start at screen center (200,400), got (%d,%d)", swipe.FromX, swipe.FromY)
	}
	// "up" reveals upper content by dragging the finger down, so toY > fromY.
	if swipe.ToY <= swipe.FromY {
		t.Errorf("expected toY > fromY for scroll up, got from=%d to=%d", swipe.FromY, swipe.ToY)
	}
}

func TestRunner_ParallelFetchCallsAllDriverMethods(t *testing.T) {
	state := newHarness(t)
	state.mock.MetricsData = driver.Metrics{CPUPercent: 5.0, HeapBytes: 1024, TotalMemoryBytes: 4096}
	state.mock.LogEntries = []driver.LogEntry{
		{UnixMillis: 1000, Level: "E", Tag: "test", Message: "boom"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Run(ctx, Options{
		Duration:    100 * time.Millisecond,
		IdleTimeout: 50 * time.Millisecond,
		BundleID:    "com.fixture",
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	actions := state.mock.Actions()
	var hasSnapshot, hasMetrics, hasLogs bool
	for _, a := range actions {
		switch a.Kind {
		case mockdriver.ActionSnapshot:
			hasSnapshot = true
		case mockdriver.ActionMetrics:
			hasMetrics = true
		case mockdriver.ActionRecentLogs:
			hasLogs = true
		}
	}
	if !hasSnapshot {
		t.Error("expected Snapshot call in mock actions")
	}
	if !hasMetrics {
		t.Error("expected Metrics call in mock actions")
	}
	if !hasLogs {
		t.Error("expected RecentLogs call in mock actions")
	}
}

// TestRunner_UsesAtomicSnapshot ensures the runner observes a step's UI
// through the paired Snapshot RPC instead of racing two independent
// hierarchy + screenshot reads. The pair must come from one on-device
// frame; a regression to separate calls is what this test catches.
func TestRunner_UsesAtomicSnapshot(t *testing.T) {
	state := newHarness(t)
	state.mock.ImageData = driver.Image{PNG: []byte("png"), Width: 1, Height: 1}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    100 * time.Millisecond,
		IdleTimeout: 20 * time.Millisecond,
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Steps == 0 {
		t.Fatal("expected at least one step")
	}

	var snapshotCalls, hierarchyCalls, screenshotCalls int
	for _, action := range state.mock.Actions() {
		switch action.Kind {
		case mockdriver.ActionSnapshot:
			snapshotCalls++
		case mockdriver.ActionHierarchy:
			hierarchyCalls++
		case mockdriver.ActionScreenshot:
			screenshotCalls++
		}
	}
	if snapshotCalls == 0 {
		t.Errorf("expected at least one Snapshot call, got %d", snapshotCalls)
	}
	if hierarchyCalls != 0 {
		t.Errorf("expected zero standalone Hierarchy calls (runner must use Snapshot), got %d", hierarchyCalls)
	}
	if screenshotCalls != 0 {
		t.Errorf("expected zero standalone Screenshot calls (runner must use Snapshot), got %d", screenshotCalls)
	}
}

// TestRunner_OneScreenshotPerStep verifies the runner writes a single
// screenshot per step, captured concurrently with hierarchy so the two
// observations describe the same UI moment.
func TestRunner_OneScreenshotPerStep(t *testing.T) {
	state := newHarness(t)
	state.mock.ImageData = driver.Image{PNG: []byte("fakepng"), Width: 100, Height: 200}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    200 * time.Millisecond,
		IdleTimeout: 50 * time.Millisecond,
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Steps < 2 {
		t.Fatalf("need at least 2 steps for screenshot test, got %d", summary.Steps)
	}

	screenshotDir := filepath.Join(state.writer.Directory(), "screenshots")
	for step := 1; step <= summary.Steps; step++ {
		path := filepath.Join(screenshotDir, fmt.Sprintf("step-%05d.png", step))
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected screenshot for step %d at %s", step, path)
		}
	}

	entries, err := os.ReadDir(screenshotDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "-after") {
			t.Errorf("unexpected -after screenshot remains: %s", entry.Name())
		}
	}
}

// TestRunner_StableTransitionalTreeIsVerified feeds a driver whose hierarchy
// constantly carries two route-level *Screen ids but never changes between
// retry attempts. Such a tree is a settled state that merely matches the
// transitional heuristic, so the runner must verify it (the always-false
// predicate's violation surfaces) instead of skipping the verifier forever.
func TestRunner_StableTransitionalTreeIsVerified(t *testing.T) {
	state := newHarnessWithSpec(t, violationSpec)
	state.mock.HierarchyJSON = `{"attributes":{"resource-id":"root"},"children":[
	  {"attributes":{"resource-id":"AddAccountScreen"},"children":[]},
	  {"attributes":{"resource-id":"HomeScreen"},"children":[]}
	]}`
	state.mock.ImageData = driver.Image{PNG: []byte("fakepng"), Width: 100, Height: 200}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    200 * time.Millisecond,
		IdleTimeout: 20 * time.Millisecond,
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Steps == 0 {
		t.Fatal("expected at least one step")
	}
	if !containsProperty(summary.Violations, "balanceNonNegative") {
		t.Fatalf("expected verifier to run on a stable two-screen tree, got %v", summary.Violations)
	}

	type traceLine struct {
		Step         int  `json:"step"`
		Transitional bool `json:"transitional"`
	}
	body, err := os.ReadFile(filepath.Join(state.writer.Directory(), "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range bytes.Split(bytes.TrimSpace(body), []byte("\n")) {
		var line traceLine
		if err := json.Unmarshal(raw, &line); err != nil {
			t.Fatalf("decode trace line: %v", err)
		}
		if line.Transitional {
			t.Errorf("step %d: stable tree must not be marked transitional", line.Step)
		}
	}

	// The early break must still persist the step's screenshot.
	screenshotPath := filepath.Join(state.writer.Directory(), "screenshots", "step-00001.png")
	if _, err := os.Stat(screenshotPath); err != nil {
		t.Errorf("expected screenshot for step 1 at %s: %v", screenshotPath, err)
	}
}

// snapshotCrossFade wraps a mock driver so every Snapshot call returns a
// transitional two-screen tree whose JSON differs from the previous call,
// mimicking a genuine cross-fade in flight.
type snapshotCrossFade struct {
	*mockdriver.Driver
	calls int
}

func (d *snapshotCrossFade) Snapshot(ctx context.Context) (string, driver.Image, error) {
	d.calls++
	_, image, err := d.Driver.Snapshot(ctx)
	hierarchyJSON := fmt.Sprintf(`{"attributes":{"resource-id":"root"},"children":[
	  {"attributes":{"resource-id":"AddAccountScreen","text":"frame-%d"},"children":[]},
	  {"attributes":{"resource-id":"HomeScreen"},"children":[]}
	]}`, d.calls)
	return hierarchyJSON, image, err
}

// TestRunner_GenuineCrossFadeStillRetried pins the existing behavior for real
// transitions: a tree that keeps changing between retry attempts exhausts the
// budget, stays transitional, and the verifier is skipped for the step.
func TestRunner_GenuineCrossFadeStillRetried(t *testing.T) {
	state := newHarnessWithSpec(t, violationSpec)
	wrapped := &snapshotCrossFade{Driver: state.mock}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    200 * time.Millisecond,
		IdleTimeout: 20 * time.Millisecond,
		Driver:      wrapped,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Steps == 0 {
		t.Fatal("expected at least one step")
	}
	if len(summary.Violations) != 0 {
		t.Fatalf("verifier must be skipped on cross-fade steps; got %v", summary.Violations)
	}

	type traceLine struct {
		Step         int      `json:"step"`
		Transitional bool     `json:"transitional"`
		Violations   []string `json:"violations"`
	}
	body, err := os.ReadFile(filepath.Join(state.writer.Directory(), "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, raw := range bytes.Split(bytes.TrimSpace(body), []byte("\n")) {
		var line traceLine
		if err := json.Unmarshal(raw, &line); err != nil {
			t.Fatalf("decode trace line: %v", err)
		}
		lines++
		if !line.Transitional {
			t.Errorf("step %d: expected transitional=true on every step, got false", line.Step)
		}
		if len(line.Violations) != 0 {
			t.Errorf("step %d: verifier must be skipped, got violations %v", line.Step, line.Violations)
		}
	}
	if lines == 0 {
		t.Fatal("expected trace lines, got none")
	}
}

// TestRunner_CleanTreeStillVerified is the control: a single-screen hierarchy
// must not be marked transitional and the verifier must still run, surfacing
// the always-false predicate's violation on the onset step.
func TestRunner_CleanTreeStillVerified(t *testing.T) {
	state := newHarnessWithSpec(t, violationSpec)
	state.mock.HierarchyJSON = `{"attributes":{"resource-id":"HomeScreen"},"children":[]}`

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    200 * time.Millisecond,
		IdleTimeout: 20 * time.Millisecond,
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !containsProperty(summary.Violations, "balanceNonNegative") {
		t.Fatalf("expected verifier to surface balanceNonNegative on a clean tree, got %v", summary.Violations)
	}

	type traceLine struct {
		Step         int  `json:"step"`
		Transitional bool `json:"transitional"`
	}
	body, err := os.ReadFile(filepath.Join(state.writer.Directory(), "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range bytes.Split(bytes.TrimSpace(body), []byte("\n")) {
		var line traceLine
		if err := json.Unmarshal(raw, &line); err != nil {
			t.Fatalf("decode trace line: %v", err)
		}
		if line.Transitional {
			t.Errorf("step %d: clean tree must not be marked transitional", line.Step)
		}
	}
}

// snapshotFailFirst wraps a mock driver so the first Snapshot call returns an
// error (mimicking a sidecar timeout while fetching view hierarchy), then
// delegates every subsequent call back to the mock.
type snapshotFailFirst struct {
	*mockdriver.Driver
	calls int
}

func (d *snapshotFailFirst) Snapshot(ctx context.Context) (string, driver.Image, error) {
	d.calls++
	if d.calls == 1 {
		return "", driver.Image{}, errors.New("Timeout while fetching view hierarchy")
	}
	return d.Driver.Snapshot(ctx)
}

// TestRunner_NilHierarchyMarksTransitional verifies that when the sidecar's
// hierarchy fetch fails (nil tree), the runner marks the step transitional and
// skips the verifier instead of pushing a nil tree that would crash the spec.
// Subsequent steps with a clean tree still drive the verifier normally.
func TestRunner_NilHierarchyMarksTransitional(t *testing.T) {
	state := newHarnessWithSpec(t, violationSpec)
	state.mock.HierarchyJSON = `{"attributes":{"resource-id":"HomeScreen"},"children":[]}`
	wrapped := &snapshotFailFirst{Driver: state.mock}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    200 * time.Millisecond,
		IdleTimeout: 20 * time.Millisecond,
		Driver:      wrapped,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Steps < 2 {
		t.Fatalf("need at least 2 steps to verify the first is skipped and the second runs, got %d", summary.Steps)
	}
	// violationSpec always() => false fires on the first verifier push. With
	// step 1's verifier skipped, onset moves to step 2.
	if len(summary.Violations) != 1 {
		t.Fatalf("expected exactly one onset record, got %d: %v", len(summary.Violations), summary.Violations)
	}
	if summary.Violations[0].StepIndex != 2 {
		t.Errorf("onset step: got %d, want 2 (step 1 verifier skipped due to nil tree)", summary.Violations[0].StepIndex)
	}

	type traceLine struct {
		Step         int      `json:"step"`
		Transitional bool     `json:"transitional"`
		Violations   []string `json:"violations"`
	}
	body, err := os.ReadFile(filepath.Join(state.writer.Directory(), "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var first traceLine
	if err := json.Unmarshal(bytes.SplitN(bytes.TrimSpace(body), []byte("\n"), 2)[0], &first); err != nil {
		t.Fatalf("decode first trace line: %v", err)
	}
	if first.Step != 1 {
		t.Fatalf("first trace line step: got %d, want 1", first.Step)
	}
	if !first.Transitional {
		t.Error("first step must be marked transitional when the hierarchy fetch failed")
	}
	if len(first.Violations) != 0 {
		t.Errorf("step 1 must skip the verifier; got violations %v", first.Violations)
	}
}

// tapSelectorFailFirst wraps a mock driver so the first TapSelector call
// returns a gRPC DeadlineExceeded error (mimicking a sidecar-side RPC hang),
// then delegates every subsequent call back to the mock.
type tapSelectorFailFirst struct {
	*mockdriver.Driver
	calls int
}

func (d *tapSelectorFailFirst) TapSelector(ctx context.Context, selector string) error {
	d.calls++
	if d.calls == 1 {
		return status.Error(codes.DeadlineExceeded, "boom")
	}
	return d.Driver.TapSelector(ctx, selector)
}

// TestRunner_TransientApplyErrorMarksTransitional verifies that a transient
// gRPC error from applyAction (e.g. sidecar RPC deadline) does not kill the
// run: the step is marked transitional, the verifier is skipped for it, and
// the loop continues with the next step running cleanly.
func TestRunner_TransientApplyErrorMarksTransitional(t *testing.T) {
	state := newHarness(t)
	wrapped := &tapSelectorFailFirst{Driver: state.mock}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    300 * time.Millisecond,
		IdleTimeout: 20 * time.Millisecond,
		Driver:      wrapped,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
		Logger:      logger,
	})
	if err != nil {
		t.Fatalf("Run must not return on transient apply error, got %v", err)
	}
	if summary.Steps < 2 {
		t.Fatalf("need at least 2 steps to prove the loop continued past the failed apply, got %d", summary.Steps)
	}
	if len(summary.Violations) != 0 {
		t.Errorf("transient apply error must not surface as a violation, got %v", summary.Violations)
	}
	if !strings.Contains(logBuf.String(), "apply error; marking step transitional") {
		t.Errorf("expected apply-error WARN log, got %q", logBuf.String())
	}

	type traceLine struct {
		Step         int      `json:"step"`
		Transitional bool     `json:"transitional"`
		Violations   []string `json:"violations"`
	}
	body, err := os.ReadFile(filepath.Join(state.writer.Directory(), "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(body), []byte("\n"))
	var first, second traceLine
	if err := json.Unmarshal(lines[0], &first); err != nil {
		t.Fatalf("decode first trace line: %v", err)
	}
	if err := json.Unmarshal(lines[1], &second); err != nil {
		t.Fatalf("decode second trace line: %v", err)
	}
	if first.Step != 1 || !first.Transitional {
		t.Errorf("step 1 must be transitional after transient apply error, got step=%d transitional=%v", first.Step, first.Transitional)
	}
	if len(first.Violations) != 0 {
		t.Errorf("transient apply step must have no violations, got %v", first.Violations)
	}
	if second.Step != 2 || second.Transitional {
		t.Errorf("step 2 must run cleanly after the transient step, got step=%d transitional=%v", second.Step, second.Transitional)
	}
}

// internalApplyErrorFailFirst wraps a mock driver so the first InputText call
// fails with the bare Internal error the iOS runner's input handler emits
// when it chokes (HTTP 500 with an empty body), then recovers.
type internalApplyErrorFailFirst struct {
	*mockdriver.Driver
	calls int
}

func (d *internalApplyErrorFailFirst) TapSelector(ctx context.Context, selector string) error {
	d.calls++
	if d.calls == 1 {
		return status.Error(codes.Internal, "UnknownFailure(errorResponse=Request for inputText failed, code: 500, body: )")
	}
	return d.Driver.TapSelector(ctx, selector)
}

// TestRunner_InternalApplyErrorMarksTransitional pins the policy that a
// one-off device-side failure (e.g. the iOS input handler's bare 500) is
// absorbed as a transitional step instead of killing the run. Persistent
// failure is covered by the consecutive-failure cap.
func TestRunner_InternalApplyErrorMarksTransitional(t *testing.T) {
	state := newHarness(t)
	wrapped := &internalApplyErrorFailFirst{Driver: state.mock}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    300 * time.Millisecond,
		IdleTimeout: 20 * time.Millisecond,
		Driver:      wrapped,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run must not return on a one-off internal apply error, got %v", err)
	}
	if summary.Steps < 2 {
		t.Fatalf("need at least 2 steps to prove the loop continued, got %d", summary.Steps)
	}
}

// TestIsWDADrop_Classification pins the fatal-vs-recoverable boundary: only
// the sidecar's explicit reconnect-failure signal is a drop. A structured
// UNAVAILABLE that happens to embed raw exception text (e.g. ConnectException
// from the original failure) means the sidecar already recovered and the run
// must continue.
// TestIsWDADrop_Classification pins isWDADrop to the exact phrase the sidecar
// throws at its reconnect site (sidecar DriverBackend.kt):
//
//	throw IllegalStateException("WDA reconnect failed: $restartErr", cause)
//
// If that message is reworded on the Kotlin side without updating the Go
// matcher, a fatal, unrecoverable WDA drop is misclassified as transient and
// the run burns its budget retrying a dead channel instead of aborting.
func TestIsWDADrop_Classification(t *testing.T) {
	// sidecarReconnectFailedMessage mirrors the literal the sidecar emits; the
	// matcher's contract is keyed on this exact prefix.
	const sidecarReconnectFailedMessage = "WDA reconnect failed"
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			"unavailable with embedded ConnectException is recovered",
			status.Error(codes.Unavailable, "connection dropped mid-action; the action may have applied: java.net.ConnectException: Failed to connect to /127.0.0.1:22161"),
			false,
		},
		{
			"sidecar reconnect-failed message is a drop",
			status.Error(codes.Internal, "java.lang.IllegalStateException: "+sidecarReconnectFailedMessage+": IOSDriverTimeoutException"),
			true,
		},
		{"generic internal is not a drop", status.Error(codes.Internal, "boom"), false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isWDADrop(testCase.err); got != testCase.want {
				t.Errorf("got %v, want %v", got, testCase.want)
			}
		})
	}
}

// tapSelectorAlwaysUnavailable wraps a mock driver so every TapSelector call
// fails with a transient Unavailable error, mimicking a device whose channel
// never recovers between steps.
type tapSelectorAlwaysUnavailable struct {
	*mockdriver.Driver
}

func (d *tapSelectorAlwaysUnavailable) TapSelector(ctx context.Context, selector string) error {
	return status.Error(codes.Unavailable, "connection dropped mid-action; the action may have applied")
}

// TestRunner_ConsecutiveTransientApplyFailuresAbort verifies the run fails
// fast once transient apply errors form an unbroken streak instead of burning
// the whole budget on a wedged device.
func TestRunner_ConsecutiveTransientApplyFailuresAbort(t *testing.T) {
	state := newHarness(t)
	wrapped := &tapSelectorAlwaysUnavailable{Driver: state.mock}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    5 * time.Second,
		IdleTimeout: 20 * time.Millisecond,
		Driver:      wrapped,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err == nil {
		t.Fatal("Run must abort after consecutive transient apply failures")
	}
	if !strings.Contains(err.Error(), "consecutive failures") {
		t.Errorf("expected consecutive-failure abort, got %v", err)
	}
	// The aborting step returns before it is recorded, so the summary holds
	// the steps before the cap-hitting one.
	if summary.Steps != maxConsecutiveApplyFailures-1 {
		t.Errorf("run must stop at the failure cap, got %d recorded steps", summary.Steps)
	}
}

// TestRunner_WaitActionSkipsIdle ensures the runner does not call WaitForIdle
// after a Wait action - the action already provides settling time.
func TestRunner_WaitActionSkipsIdle(t *testing.T) {
	const waitSpec = `
import { actions, Wait } from "@sanderling/spec";
globalThis.actions = actions(() => [Wait({ durationMillis: 5 })]);
`
	state := newHarnessWithSpec(t, waitSpec)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Run(ctx, Options{
		Duration:    150 * time.Millisecond,
		IdleTimeout: 50 * time.Millisecond,
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, action := range state.mock.Actions() {
		if action.Kind == mockdriver.ActionWaitForIdle {
			t.Fatalf("Wait action must skip WaitForIdle, got: %v", action)
		}
	}
}

func mustNewVerifier(t *testing.T) *verifier.Verifier {
	t.Helper()
	verifierInstance, err := verifier.New()
	if err != nil {
		t.Fatal(err)
	}
	return verifierInstance
}

func mustNewTraceWriter(t *testing.T) *trace.Writer {
	t.Helper()
	writer, err := trace.NewWriter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	return writer
}

func containsAction(actions []mockdriver.Action, kind mockdriver.ActionKind, payload string) bool {
	for _, action := range actions {
		if action.Kind != kind {
			continue
		}
		switch kind {
		case mockdriver.ActionLaunch:
			if action.BundleID == payload {
				return true
			}
		case mockdriver.ActionTapSelector:
			if action.Selector == payload {
				return true
			}
		case mockdriver.ActionTerminate:
			return true
		default:
			return true
		}
	}
	return false
}

func containsProperty(records []ViolationRecord, property string) bool {
	for _, record := range records {
		if slices.Contains(record.Properties, property) {
			return true
		}
	}
	return false
}

func TestRunner_RelaunchesWhenAppLeavesForeground(t *testing.T) {
	state := newHarness(t)
	// Always report a foreign app, so every step's guard must relaunch.
	state.mock.ForegroundResults = []string{"com.android.chrome"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Run(ctx, Options{
		Duration:    100 * time.Millisecond,
		IdleTimeout: 20 * time.Millisecond,
		BundleID:    "app.folio",
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	relaunches := 0
	for _, a := range state.mock.Actions() {
		if a.Kind == mockdriver.ActionLaunch && a.BundleID == "app.folio" && !a.ClearState {
			relaunches++
		}
	}
	if relaunches == 0 {
		t.Fatal("expected runner to relaunch app.folio when foreground escaped, got none")
	}
}

func TestRunner_NoRelaunchWhenAppInForeground(t *testing.T) {
	state := newHarness(t)
	state.mock.ForegroundResults = []string{"app.folio"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Run(ctx, Options{
		Duration:    100 * time.Millisecond,
		IdleTimeout: 20 * time.Millisecond,
		BundleID:    "app.folio",
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, a := range state.mock.Actions() {
		if a.Kind == mockdriver.ActionLaunch {
			t.Fatalf("expected no relaunch while app in foreground, got %v", a)
		}
	}
}

// TestRunner_WaitsForForegroundBeforeFirstAction verifies the startup gate
// brings the app forward (back-press + relaunch) before any tap fires when the
// device boots showing a system dialog.
func TestRunner_WaitsForForegroundBeforeFirstAction(t *testing.T) {
	state := newHarness(t)
	// First the device shows a system setup screen, then the app is on top.
	state.mock.ForegroundResults = []string{"com.google.android.setupwizard", "app.folio"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Run(ctx, Options{
		Duration:    100 * time.Millisecond,
		IdleTimeout: 20 * time.Millisecond,
		BundleID:    "app.folio",
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	actions := state.mock.Actions()
	firstLaunch, firstTap := -1, -1
	backPressed := false
	for i, a := range actions {
		switch {
		case a.Kind == mockdriver.ActionLaunch && firstLaunch < 0:
			firstLaunch = i
		case a.Kind == mockdriver.ActionTap && firstTap < 0:
			firstTap = i
		case a.Kind == mockdriver.ActionPressKey && a.Key == "back":
			backPressed = true
		}
	}
	if firstLaunch < 0 {
		t.Fatal("expected a relaunch to bring the app forward, got none")
	}
	if !backPressed {
		t.Fatal("expected a back-press to dismiss the system dialog, got none")
	}
	if firstTap >= 0 && firstLaunch > firstTap {
		t.Fatalf("expected the foreground gate (launch at %d) before the first tap (at %d)", firstLaunch, firstTap)
	}
}

// TestRunner_WaitsForWindowDrawnBeforeFirstAction verifies the startup gate
// keeps waiting while the app is the resumed activity but its window has not
// drawn yet (a leftover screen still focused). It must poll the focused-window
// signal rather than relaunching, and only proceed once the window names the
// app.
func TestRunner_WaitsForWindowDrawnBeforeFirstAction(t *testing.T) {
	state := newHarness(t)
	// The app is resumed immediately, but its window lags: the outgoing
	// settings screen stays focused for two checks before the app draws.
	state.mock.ForegroundResults = []string{"app.folio"}
	state.mock.FocusedWindowResults = []string{"com.android.settings", "com.android.settings", "app.folio"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Run(ctx, Options{
		Duration:    100 * time.Millisecond,
		IdleTimeout: 20 * time.Millisecond,
		BundleID:    "app.folio",
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The gate must have polled the focused window until it named the app,
	// i.e. at least the three queued results were consumed.
	if calls := state.mock.FocusedWindowCalls(); calls < 3 {
		t.Fatalf("expected the gate to poll the focused window until drawn (>=3 calls), got %d", calls)
	}
	// The resumed app was never a foreign app, so the gate must not relaunch
	// or back-press to "fix" a window that simply had not drawn yet.
	for _, a := range state.mock.Actions() {
		if a.Kind == mockdriver.ActionPressKey && a.Key == "back" {
			t.Fatal("expected no back-press while waiting for the window to draw")
		}
	}
}

// TestAwaitForeground_RelaunchesThenWaitsForWindow locks the per-step scope
// guard's recovery: after the app leaves to the launcher, it must relaunch AND
// keep polling the focused window until it names the app, so the step never
// observes or acts while the launcher is on screen (where InputText would land
// in the launcher's type-to-search filter). A single fire-and-forget relaunch,
// which returns before the window draws on a slow physical device, is the bug
// this guards against.
func TestAwaitForeground_RelaunchesThenWaitsForWindow(t *testing.T) {
	m := mockdriver.New()
	// Foreground: launcher on the first poll (still gone), then the app. Focus:
	// the launcher window lingers one extra poll before the app's window draws.
	m.ForegroundResults = []string{"com.android.launcher", "app.folio"}
	m.FocusedWindowResults = []string{"com.android.launcher", "app.folio"}

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn}))
	options := Options{BundleID: "app.folio", Driver: m, IdleTimeout: 10 * time.Millisecond}

	awaitForeground(context.Background(), options, logger, 7)

	relaunches, backs := 0, 0
	for _, a := range m.Actions() {
		switch {
		case a.Kind == mockdriver.ActionLaunch && a.BundleID == "app.folio" && !a.ClearState:
			relaunches++
		case a.Kind == mockdriver.ActionPressKey && a.Key == "back":
			backs++
		}
	}
	if relaunches != 1 {
		t.Fatalf("expected exactly one relaunch while the app was gone, got %d", relaunches)
	}
	if backs != 1 {
		t.Fatalf("expected one back-press to dismiss a possible dialog before relaunch, got %d", backs)
	}
	// The window lagged one poll behind the resumed activity, so the focused
	// window must have been queried at least twice before the gate returned.
	if calls := m.FocusedWindowCalls(); calls < 2 {
		t.Fatalf("expected the guard to poll the focused window until drawn (>=2), got %d", calls)
	}
}

func TestClampGestureToSafeArea_KeepsOriginBelowShadeStrip(t *testing.T) {
	screen := hierarchy.Bounds{Left: 0, Top: 0, Right: 1080, Bottom: 2400} // marginY = 200

	// Origin in the shade strip: the whole segment shifts down by 138, so the
	// downward gesture stays downward (447 -> 585) instead of reversing.
	fromX, fromY, toX, toY := clampGestureToSafeArea(802, 62, 802, 447, screen)
	if fromX != 802 || fromY != 200 || toX != 802 || toY != 585 {
		t.Errorf("segment not translated below the shade strip: from=(%d,%d) to=(%d,%d), want from=(802,200) to=(802,585)", fromX, fromY, toX, toY)
	}

	fromX, fromY, _, _ = clampGestureToSafeArea(5, 1200, 540, 1200, screen)
	if fromX != 5 || fromY != 1200 {
		t.Errorf("side origin must pass through, got (%d,%d), want (5,1200)", fromX, fromY)
	}

	fromX, fromY, _, _ = clampGestureToSafeArea(540, 2399, 540, 1200, screen)
	if fromX != 540 || fromY != 2399 {
		t.Errorf("bottom origin must pass through, got (%d,%d), want (540,2399)", fromX, fromY)
	}

	_, _, toX, toY = clampGestureToSafeArea(540, 1200, -50, 9999, screen)
	if toX != 0 || toY != 2400 {
		t.Errorf("off-screen destination not clamped to screen edges: got (%d,%d), want (0,2400)", toX, toY)
	}

	fromX, _, _, _ = clampGestureToSafeArea(-30, 1200, 540, 1200, screen)
	if fromX != 0 {
		t.Errorf("off-screen origin x must clamp to 0, got %d", fromX)
	}

	fromX, fromY, toX, toY = clampGestureToSafeArea(802, 62, 802, 447, hierarchy.Bounds{})
	if fromX != 802 || fromY != 62 || toX != 802 || toY != 447 {
		t.Error("coordinates must pass through unchanged when screen size is unknown")
	}
}

// TestScreenBounds_UsesMaxExtentNotRoot guards the screen-size source: the
// Android hierarchy root reports zero bounds, so the screen rectangle must come
// from the maximum element extent or the gesture clamp silently no-ops.
func TestScreenBounds_UsesMaxExtentNotRoot(t *testing.T) {
	tree := &hierarchy.Tree{
		Root: &hierarchy.Node{Element: hierarchy.Element{Bounds: hierarchy.Bounds{}}},
		Elements: []*hierarchy.Element{
			{Bounds: hierarchy.Bounds{}},
			{Bounds: hierarchy.Bounds{Left: 0, Top: 0, Right: 1080, Bottom: 2160}},
			{Bounds: hierarchy.Bounds{Left: 0, Top: 2268, Right: 1080, Bottom: 2400}},
		},
	}
	got := screenBounds(tree)
	if got.Right != 1080 || got.Bottom != 2400 {
		t.Fatalf("screenBounds = %+v, want right=1080 bottom=2400", got)
	}
}

// TestEnsureForeground_DismissesSystemOverlay locks the shade fix: when the app
// is still the resumed activity but a system overlay (notification shade) holds
// the focused window, the guard must dismiss it with back rather than relaunch
// or act on the obscured app.
func TestEnsureForeground_DismissesSystemOverlay(t *testing.T) {
	m := mockdriver.New()
	// Resumed activity stays the app; the focused window is the shade.
	m.ForegroundResults = []string{"app.folio"}
	m.FocusedWindowResults = []string{"com.android.systemui"}

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn}))
	options := Options{BundleID: "app.folio", Driver: m, IdleTimeout: 10 * time.Millisecond}

	got := ensureForeground(context.Background(), options, logger, 5)
	if got != foregroundOverlayDismissed {
		t.Fatalf("the guard reported %v, want foregroundOverlayDismissed; "+
			"an obscured app is not a relaunched one", got)
	}
	backs, relaunches := 0, 0
	for _, a := range m.Actions() {
		switch {
		case a.Kind == mockdriver.ActionPressKey && a.Key == "back":
			backs++
		case a.Kind == mockdriver.ActionLaunch:
			relaunches++
		}
	}
	if backs != 1 {
		t.Fatalf("expected one back-press to collapse the shade, got %d", backs)
	}
	if relaunches != 0 {
		t.Fatalf("a resumed-but-obscured app must not be relaunched, got %d relaunches", relaunches)
	}
}

func TestAppIsForeground(t *testing.T) {
	readErr := errors.New("adb read failed")
	cases := []struct {
		name       string
		bundleID   string
		foreground []string
		foregErr   error
		focused    []string
		focusErr   error
		want       bool
	}{
		{name: "no bundle id", bundleID: "", foreground: []string{"app.folio"}, want: true},
		{name: "foreground unknown", bundleID: "app.folio", foreground: nil, want: true},
		{name: "foreground read error", bundleID: "app.folio", foregErr: readErr, want: true},
		{name: "foreign foreground", bundleID: "app.folio", foreground: []string{"com.android.chrome"}, want: false},
		{name: "app resumed and focused", bundleID: "app.folio", foreground: []string{"app.folio"}, focused: []string{"app.folio"}, want: true},
		{name: "app resumed but overlay focused", bundleID: "app.folio", foreground: []string{"app.folio"}, focused: []string{"com.android.systemui"}, want: false},
		{name: "app resumed, focus unknown", bundleID: "app.folio", foreground: []string{"app.folio"}, focused: []string{""}, want: true},
		{name: "app resumed, focus read error", bundleID: "app.folio", foreground: []string{"app.folio"}, focusErr: readErr, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mockdriver.New()
			m.ForegroundResults = tc.foreground
			m.ForegroundErr = tc.foregErr
			m.FocusedWindowResults = tc.focused
			m.FocusedWindowErr = tc.focusErr
			options := Options{BundleID: tc.bundleID, Driver: m}
			if got := appIsForeground(context.Background(), options); got != tc.want {
				t.Errorf("appIsForeground = %v, want %v", got, tc.want)
			}
		})
	}
}

// A system overlay holds focus while the app stays resumed every step, so the
// fixture's id:next tap must never reach the driver.
func TestRunner_SkipsActionWhenOverlayStealsFocusAtApplyTime(t *testing.T) {
	state := newHarness(t)
	state.mock.ForegroundResults = []string{"app.folio"}
	state.mock.FocusedWindowResults = []string{"app.folio", "com.android.systemui"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    100 * time.Millisecond,
		IdleTimeout: 20 * time.Millisecond,
		BundleID:    "app.folio",
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Steps == 0 {
		t.Fatal("expected the loop to run steps")
	}
	if containsAction(state.mock.Actions(), mockdriver.ActionTapSelector, "id:next") {
		t.Error("apply-time guard failed: a tap fired while a system overlay held focus")
	}
}

// TestRunner_StopOnViolationEndsAtTheFirstViolation pins the gate CI runs on:
// a step budget of 8 against a spec that only violates on the third step must
// end on step 3 and write nothing after it, so the trace's last state is the
// one that produced the violation.
func TestRunner_StopOnViolationEndsAtTheFirstViolation(t *testing.T) {
	const thirdStepViolationSpec = `
import { actions, always, extract } from "@sanderling/spec";
let observed = 0;
const tick = extract(() => ++observed);
globalThis.properties = {
  staysUnderThree: always(() => tick.current < 3),
};
globalThis.actions = actions(() => []);
`
	state := newHarnessWithSpec(t, thirdStepViolationSpec)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:        time.Hour,
		IdleTimeout:     20 * time.Millisecond,
		MaxSteps:        8,
		StopOnViolation: true,
		Driver:          state.mock,
		Verifier:        state.verifier,
		TraceWriter:     state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !containsProperty(summary.Violations, "staysUnderThree") {
		t.Fatalf("expected staysUnderThree to fire, got %v", summary.Violations)
	}
	if summary.Steps != 3 {
		t.Errorf("steps: got %d, want 3 (the run must stop at the violating step, not run the 8-step budget)",
			summary.Steps)
	}
	for _, step := range traceStepIndices(t, state.writer.Directory()) {
		if step > summary.Steps {
			t.Errorf("trace kept stepping after the violation: found step %d past step %d",
				step, summary.Steps)
		}
	}
}

// TestRunner_WithoutStopOnViolationRunsTheWholeBudget is the other half: the
// default must stay a full-budget fuzz run, so turning the flag on is the only
// thing that shortens a run.
func TestRunner_WithoutStopOnViolationRunsTheWholeBudget(t *testing.T) {
	state := newHarnessWithSpec(t, violationSpec)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    time.Hour,
		IdleTimeout: 20 * time.Millisecond,
		MaxSteps:    4,
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Steps != 4 {
		t.Errorf("steps: got %d, want 4; a violation must not shorten a default run", summary.Steps)
	}
}

// traceStepIndices reads every step index the trace recorded, so a test can
// assert on what the run actually wrote rather than on the summary alone.
func traceStepIndices(t *testing.T, directory string) []int {
	t.Helper()
	file, err := os.Open(filepath.Join(directory, "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var steps []int
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var line struct {
			Step int `json:"step"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("trace line decode: %v", err)
		}
		steps = append(steps, line.Step)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	return steps
}
