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

	"github.com/priyanshujain/sanderling/internal/driver"
	mockdriver "github.com/priyanshujain/sanderling/internal/driver/mock"
	"github.com/priyanshujain/sanderling/internal/hierarchy"
	"github.com/priyanshujain/sanderling/internal/trace"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

const fixtureSpec = `
const balance = __sanderling__.extract(state => state.snapshots.balance ?? 0);
globalThis.properties = {
  balanceNonNegative: __sanderling__.always(() => balance.current >= 0),
};
globalThis.actions = __sanderling__.actions(() => [__sanderling__.tap({ on: "id:next" })]);
`

const violationSpec = `
globalThis.properties = {
  balanceNonNegative: __sanderling__.always(() => false),
};
globalThis.actions = __sanderling__.actions(() => []);
`

type harness struct {
	mock     *mockdriver.Driver
	verifier *verifier.Verifier
	writer   *trace.Writer
}

func newHarness(t *testing.T) *harness {
	return newHarnessWithSpec(t, fixtureSpec)
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
	if err := verifierInstance.Load(spec); err != nil {
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
globalThis.properties = {
  broken: __sanderling__.always(() => { throw new Error("bad predicate"); }),
};
globalThis.actions = __sanderling__.actions(() => [__sanderling__.tap({ on: "id:next" })]);
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

// TestRunner_WaitActionSkipsIdle ensures the runner does not call WaitForIdle
// after a Wait action - the action already provides settling time.
func TestRunner_WaitActionSkipsIdle(t *testing.T) {
	const waitSpec = `
globalThis.actions = __sanderling__.actions(() => [__sanderling__.wait({ durationMillis: 5 })]);
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
