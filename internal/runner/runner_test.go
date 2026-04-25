package runner

import (
	"bytes"
	"context"
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
	var hasHierarchy, hasMetrics, hasLogs bool
	for _, a := range actions {
		switch a.Kind {
		case mockdriver.ActionHierarchy:
			hasHierarchy = true
		case mockdriver.ActionMetrics:
			hasMetrics = true
		case mockdriver.ActionRecentLogs:
			hasLogs = true
		}
	}
	if !hasHierarchy {
		t.Error("expected Hierarchy call in mock actions")
	}
	if !hasMetrics {
		t.Error("expected Metrics call in mock actions")
	}
	if !hasLogs {
		t.Error("expected RecentLogs call in mock actions")
	}
}

func TestRunner_PipelinedPostScreenshotWritten(t *testing.T) {
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
		t.Fatalf("need at least 2 steps for pipelining test, got %d", summary.Steps)
	}

	screenshotDir := filepath.Join(state.writer.Directory(), "screenshots")

	preFile := filepath.Join(screenshotDir, "step-00001.png")
	if _, err := os.Stat(preFile); os.IsNotExist(err) {
		t.Errorf("expected pre-screenshot for step 1: %s", preFile)
	}

	postFile := filepath.Join(screenshotDir, "step-00001-after.png")
	if _, err := os.Stat(postFile); os.IsNotExist(err) {
		t.Errorf("expected pipelined post-screenshot for step 1: %s", postFile)
	}

	lastAfter := filepath.Join(screenshotDir, fmt.Sprintf("step-%05d-after.png", summary.Steps))
	if _, err := os.Stat(lastAfter); os.IsNotExist(err) {
		t.Errorf("expected flushed post-screenshot for last step %d: %s", summary.Steps, lastAfter)
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
