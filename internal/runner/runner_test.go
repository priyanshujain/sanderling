package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestApplyAction_DoubleTapDispatchesTwoTapsAtCoordinates(t *testing.T) {
	driverMock := mockdriver.New()
	action := verifier.Action{Kind: verifier.ActionKindDoubleTap, X: 100, Y: 200}

	start := time.Now()
	if err := applyAction(context.Background(), driverMock, action, nil); err != nil {
		t.Fatalf("apply action: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 40*time.Millisecond {
		t.Errorf("expected >= 40ms gap between taps, elapsed %v", elapsed)
	}
	taps := 0
	for _, a := range driverMock.Actions() {
		if a.Kind == mockdriver.ActionTap && a.X == 100 && a.Y == 200 {
			taps++
		}
	}
	if taps != 2 {
		t.Errorf("expected 2 Tap calls at (100,200), got %d in %v", taps, driverMock.Actions())
	}
}

func TestApplyAction_DoubleTapDispatchesTwoSelectorTaps(t *testing.T) {
	driverMock := mockdriver.New()
	action := verifier.Action{Kind: verifier.ActionKindDoubleTap, On: "id:save"}

	if err := applyAction(context.Background(), driverMock, action, nil); err != nil {
		t.Fatalf("apply action: %v", err)
	}
	taps := 0
	for _, a := range driverMock.Actions() {
		if a.Kind == mockdriver.ActionTapSelector && a.Selector == "id:save" {
			taps++
		}
	}
	if taps != 2 {
		t.Errorf("expected 2 TapSelector calls with id:save, got %d in %v", taps, driverMock.Actions())
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

// TestIsTransitionalHierarchy_DetectsMultipleScreens covers the runner-side
// guard that re-fetches when the hierarchy still carries two route-level
// *Screen ids - the NavHost cross-fade signature.
func TestIsTransitionalHierarchy_DetectsMultipleScreens(t *testing.T) {
	multi, err := hierarchy.Parse(`{"attributes":{"resource-id":"root"},"children":[
	  {"attributes":{"resource-id":"AddAccountScreen"},"children":[]},
	  {"attributes":{"resource-id":"HomeScreen"},"children":[]}
	]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !isTransitionalHierarchy(multi) {
		t.Error("expected multi-screen tree to be flagged as transitional")
	}

	single, err := hierarchy.Parse(`{"attributes":{"resource-id":"HomeScreen"},"children":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	if isTransitionalHierarchy(single) {
		t.Error("single-screen tree must not be flagged as transitional")
	}

	if isTransitionalHierarchy(nil) {
		t.Error("nil tree must not be flagged as transitional")
	}
}

// TestRunner_TransitionalSkipsVerifier feeds a driver whose hierarchy stays
// transitional (multiple route-level *Screen ids) on every Snapshot call.
// Every step must be marked transitional in the trace, no violations may be
// emitted (the verifier never ran), and the summary must stay clean even
// though the spec is a guaranteed always-false predicate.
func TestRunner_TransitionalSkipsVerifier(t *testing.T) {
	state := newHarnessWithSpec(t, violationSpec)
	state.mock.HierarchyJSON = `{"attributes":{"resource-id":"root"},"children":[
	  {"attributes":{"resource-id":"AddAccountScreen"},"children":[]},
	  {"attributes":{"resource-id":"HomeScreen"},"children":[]}
	]}`

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
	if len(summary.Violations) != 0 {
		t.Fatalf("verifier must be skipped on transitional steps; got %v", summary.Violations)
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
	if !strings.Contains(logBuf.String(), "transient apply error") {
		t.Errorf("expected transient-apply WARN log, got %q", logBuf.String())
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

// TestIsTransientApplyError_Classification covers the helper's matching rules
// directly so future code changes don't quietly drop a transient case.
func TestIsTransientApplyError_Classification(t *testing.T) {
	cleanCtx := context.Background()
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{"nil error", cleanCtx, nil, false},
		{"deadline exceeded", cleanCtx, status.Error(codes.DeadlineExceeded, "boom"), true},
		{"unavailable", cleanCtx, status.Error(codes.Unavailable, "boom"), true},
		{"internal wrapping deadline", cleanCtx, status.Error(codes.Internal, "io.grpc.StatusRuntimeException: DEADLINE_EXCEEDED: ..."), true},
		{"internal wrapping unavailable", cleanCtx, status.Error(codes.Internal, "io.grpc.StatusRuntimeException: UNAVAILABLE: ..."), true},
		{"internal generic", cleanCtx, status.Error(codes.Internal, "boom"), false},
		{"raw context deadline", cleanCtx, context.DeadlineExceeded, true},
		{"run context cancelled overrides", cancelledCtx, status.Error(codes.DeadlineExceeded, "boom"), false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isTransientApplyError(testCase.ctx, testCase.err); got != testCase.want {
				t.Errorf("got %v, want %v", got, testCase.want)
			}
		})
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
