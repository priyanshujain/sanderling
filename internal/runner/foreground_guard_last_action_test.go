package runner

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/driver"
	mockdriver "github.com/priyanshujain/sanderling/internal/driver/mock"
)

const guardedBundleID = "app.folio"

// committingDevice is a device whose submit taps commit transactions the next
// hierarchy read shows, and which can say how many it has committed so a test
// can prove the taps landed before reading anything into a verdict.
type committingDevice interface {
	driver.DeviceDriver
	commits() int64
}

// leavesForegroundAfterSubmitDriver is the condition the app-scope guard exists
// for: the submit tap lands and commits, and the app is no longer the
// foreground app by the time the next step looks. Folio's transactions are in
// sqlite, so the commit survives the relaunch and the next reading shows it.
type leavesForegroundAfterSubmitDriver struct {
	*mockdriver.Driver
	commitsPerTap int64
	committed     atomic.Int64
	away          atomic.Bool
}

func (d *leavesForegroundAfterSubmitDriver) Tap(context.Context, int, int) error {
	return d.commitThenLeave()
}

func (d *leavesForegroundAfterSubmitDriver) TapSelector(context.Context, string) error {
	return d.commitThenLeave()
}

func (d *leavesForegroundAfterSubmitDriver) commitThenLeave() error {
	d.committed.Add(d.commitsPerTap)
	d.away.Store(true)
	return nil
}

func (d *leavesForegroundAfterSubmitDriver) commits() int64 { return d.committed.Load() }

func (d *leavesForegroundAfterSubmitDriver) Launch(
	ctx context.Context,
	bundleID string,
	clearState bool,
	env map[string]string,
) error {
	d.away.Store(false)
	return d.Driver.Launch(ctx, bundleID, clearState, env)
}

func (d *leavesForegroundAfterSubmitDriver) ForegroundApp(context.Context) (string, error) {
	if d.away.Load() {
		return "com.android.launcher", nil
	}
	return guardedBundleID, nil
}

func (d *leavesForegroundAfterSubmitDriver) FocusedWindowApp(ctx context.Context) (string, error) {
	return d.ForegroundApp(ctx)
}

func (d *leavesForegroundAfterSubmitDriver) Snapshot(context.Context) (string, driver.Image, error) {
	return fmt.Sprintf(homeWithTxnCount, d.committed.Load()), driver.Image{}, nil
}

// No device answers Snapshot and Hierarchy off different trees, and the runner
// reads both per step, so this one answers them off the same commit count.
func (d *leavesForegroundAfterSubmitDriver) Hierarchy(context.Context) (string, error) {
	return fmt.Sprintf(homeWithTxnCount, d.committed.Load()), nil
}

// obscuredAfterSubmitDriver is the other half of the same guard: the app stays
// the resumed activity, but a system window (the notification shade) owns the
// focused window when the next step looks, and the guard presses back to
// collapse it.
type obscuredAfterSubmitDriver struct {
	*mockdriver.Driver
	commitsPerTap int64
	committed     atomic.Int64
	obscured      atomic.Bool
}

func (d *obscuredAfterSubmitDriver) Tap(context.Context, int, int) error {
	return d.commitThenObscure()
}

func (d *obscuredAfterSubmitDriver) TapSelector(context.Context, string) error {
	return d.commitThenObscure()
}

func (d *obscuredAfterSubmitDriver) commitThenObscure() error {
	d.committed.Add(d.commitsPerTap)
	d.obscured.Store(true)
	return nil
}

func (d *obscuredAfterSubmitDriver) commits() int64 { return d.committed.Load() }

func (d *obscuredAfterSubmitDriver) PressKey(ctx context.Context, key string) error {
	if key == "back" {
		d.obscured.Store(false)
	}
	return d.Driver.PressKey(ctx, key)
}

func (d *obscuredAfterSubmitDriver) ForegroundApp(context.Context) (string, error) {
	return guardedBundleID, nil
}

func (d *obscuredAfterSubmitDriver) FocusedWindowApp(context.Context) (string, error) {
	if d.obscured.Load() {
		return "com.android.systemui", nil
	}
	return guardedBundleID, nil
}

func (d *obscuredAfterSubmitDriver) Snapshot(context.Context) (string, driver.Image, error) {
	return fmt.Sprintf(homeWithTxnCount, d.committed.Load()), driver.Image{}, nil
}

func (d *obscuredAfterSubmitDriver) Hierarchy(context.Context) (string, error) {
	return fmt.Sprintf(homeWithTxnCount, d.committed.Load()), nil
}

// runTwoSubmitSteps drives two steps of the shipped folio counting property
// against a device that commits on every tap, and hands back what the property
// decided. Both steps have to run: the first arms the comparison, the second is
// where the guard fires and the pair is judged.
func runTwoSubmitSteps(
	t *testing.T,
	state *harness,
	device committingDevice,
	commitsPerTap int64,
) []ViolationRecord {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    time.Hour,
		IdleTimeout: 20 * time.Millisecond,
		MaxSteps:    2,
		BundleID:    guardedBundleID,
		Driver:      device,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Steps != 2 {
		t.Fatalf("steps = %d, want 2; the run never reached the step that judges the pair",
			summary.Steps)
	}
	if got := device.commits(); got != commitsPerTap*2 {
		t.Fatalf("the device committed %d transaction(s), want %d; the taps never reached it",
			got, commitsPerTap*2)
	}
	return summary.Violations
}

func countMockActions(state *harness, kind mockdriver.ActionKind, key string) int {
	count := 0
	for _, action := range state.mock.Actions() {
		if action.Kind != kind {
			continue
		}
		if key != "" && action.Key != key {
			continue
		}
		count++
	}
	return count
}

func specWithFolioPredicates(t *testing.T) string {
	t.Helper()
	predicates, err := filepath.Abs("../../examples/folio/sanderling/predicates.ts")
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf(submitCountingSpecTemplate, predicates)
}

// A relaunch is not proof that nothing ran before it. The submit was dispatched
// and confirmed; what the relaunch changed is that the app restarted between
// the two readings the property compares. Reporting "no action" for it hands
// submitCommitsOneTransactionPerAction a transaction rise of one against a
// window of zero submits, which is the conviction #77 fixed for the apply-error
// path, manufactured here out of the scope guard instead.
func TestRunner_ARelaunchDoesNotConvictTheSubmitCountingProperty(t *testing.T) {
	spec := specWithFolioPredicates(t)

	run := func(t *testing.T, commitsPerTap int64) []ViolationRecord {
		t.Helper()
		state := newHarnessWithSpec(t, spec)
		device := &leavesForegroundAfterSubmitDriver{
			Driver:        state.mock,
			commitsPerTap: commitsPerTap,
		}
		violations := runTwoSubmitSteps(t, state, device, commitsPerTap)
		if countMockActions(state, mockdriver.ActionLaunch, "") == 0 {
			t.Fatal("the app was never relaunched, so the guard this test is about never ran")
		}
		return violations
	}

	t.Run("one transaction per tap is not a double submit", func(t *testing.T) {
		if violations := run(t, 1); len(violations) != 0 {
			t.Errorf("the counting property convicted a healthy app: %v\n"+
				"one transaction rose against a submit the runner confirmed, and the "+
				"spec was told no action happened because the app was relaunched",
				violations)
		}
	})

	// The control. Without it a green above proves nothing: a property that
	// never sees a comparable pair is silently vacuous and reports the same
	// empty violation list.
	t.Run("two transactions per tap still convicts", func(t *testing.T) {
		violations := run(t, 2)
		if len(violations) == 0 {
			t.Fatal("the counting property missed a double submit; the harness never " +
				"put the property in a position to fire, so the case above proves nothing")
		}
		if violations[0].Properties[0] != "submitCommitsOneTransactionPerAction" {
			t.Errorf("violated %v, want submitCommitsOneTransactionPerAction", violations[0].Properties)
		}
	})
}

// The same hole through the guard's other branch. A system window holding the
// focus says nothing about whether the tap under it ran: it was dispatched, and
// what nobody can say afterwards is whether the app received it. That is the
// unknown `applied` already carries, and it counts toward the submits a window
// could hold. Reporting no action instead convicts the app of a transaction
// with no cause.
func TestRunner_AnOverlayDoesNotConvictTheSubmitCountingProperty(t *testing.T) {
	spec := specWithFolioPredicates(t)

	run := func(t *testing.T, commitsPerTap int64) []ViolationRecord {
		t.Helper()
		state := newHarnessWithSpec(t, spec)
		device := &obscuredAfterSubmitDriver{
			Driver:        state.mock,
			commitsPerTap: commitsPerTap,
		}
		violations := runTwoSubmitSteps(t, state, device, commitsPerTap)
		if countMockActions(state, mockdriver.ActionPressKey, "back") == 0 {
			t.Fatal("the overlay was never dismissed, so the guard this test is about never ran")
		}
		if countMockActions(state, mockdriver.ActionLaunch, "") != 0 {
			t.Fatal("a resumed-but-obscured app must not be relaunched")
		}
		return violations
	}

	t.Run("one transaction per tap is not a double submit", func(t *testing.T) {
		if violations := run(t, 1); len(violations) != 0 {
			t.Errorf("the counting property convicted a healthy app: %v\n"+
				"one transaction rose against a submit the runner dispatched, and the "+
				"spec was told no action happened because a system window took the focus",
				violations)
		}
	})

	t.Run("two transactions per tap still convicts", func(t *testing.T) {
		violations := run(t, 2)
		if len(violations) == 0 {
			t.Fatal("the counting property missed a double submit; the harness never " +
				"put the property in a position to fire, so the case above proves nothing")
		}
		if violations[0].Properties[0] != "submitCommitsOneTransactionPerAction" {
			t.Errorf("violated %v, want submitCommitsOneTransactionPerAction", violations[0].Properties)
		}
	})
}

// reportedActionSpec puts what the runner told the spec about the last action
// into an extractor, so a test can read it out of the trace. `applied` and
// `relaunched` have no other producer: the runner's two guard writes are the
// only thing that ever sets them, and every spec-side guard built on them (see
// acrossRelaunch and confirmedApplied in the folio predicates) reads nothing
// else. A regression in either write leaves those guards permanently off with
// no property anywhere able to notice.
const reportedActionSpec = `
import { actions, always, extract, Tap } from "@sanderling/spec";
const reportedAction = extract("reportedAction", state => {
  const last = state.lastAction;
  if (last == null) return "none";
  const dispatch = last.applied === true ? "applied" : "unconfirmed";
  const process = last.relaunched === true ? "relaunched" : "same-process";
  return dispatch + "/" + process;
});
globalThis.properties = {
  theGuardTheRunnerRanReachesTheSpec: always(
    () => reportedAction.current !== "applied/same-process",
  ),
};
globalThis.actions = actions(() => [Tap({ on: "id:TxnSubmit" })]);
`

// runReportingTheGuard drives two steps against a device whose submit tap trips
// one of the foreground guards, and hands back what the spec read off
// state.lastAction on the step the guard fired.
func runReportingTheGuard(t *testing.T, device committingDevice, state *harness) string {
	t.Helper()
	if violations := runTwoSubmitSteps(t, state, device, 1); len(violations) != 0 {
		t.Errorf("the spec was told the action ran untouched by any guard: %v", violations)
	}
	steps := traceSteps(t, state.writer.Directory())
	if len(steps) != 2 {
		t.Fatalf("trace holds %d step(s), want 2", len(steps))
	}
	change, ok := steps[1].ExtractorChanges["reportedAction"]
	if !ok {
		t.Fatalf("step 2 recorded no reading of the reported action: %+v", steps[1])
	}
	return string(change.Curr)
}

func TestRunner_TheSpecIsToldTheAppWasRelaunchedUnderTheAction(t *testing.T) {
	state := newHarnessWithSpec(t, reportedActionSpec)
	device := &leavesForegroundAfterSubmitDriver{Driver: state.mock, commitsPerTap: 1}

	reported := runReportingTheGuard(t, device, state)

	if countMockActions(state, mockdriver.ActionLaunch, "") == 0 {
		t.Fatal("the app was never relaunched, so the write this test is about never ran")
	}
	if reported != `"applied/relaunched"` {
		t.Errorf("the spec read %s off state.lastAction, want \"applied/relaunched\"; "+
			"a property relaxed across a relaunch cannot fire on a run that never "+
			"tells it one happened", reported)
	}
}

func TestRunner_TheSpecIsToldAnObscuredActionWasNotConfirmed(t *testing.T) {
	state := newHarnessWithSpec(t, reportedActionSpec)
	device := &obscuredAfterSubmitDriver{Driver: state.mock, commitsPerTap: 1}

	reported := runReportingTheGuard(t, device, state)

	if countMockActions(state, mockdriver.ActionPressKey, "back") == 0 {
		t.Fatal("the overlay was never dismissed, so the write this test is about never ran")
	}
	if reported != `"unconfirmed/same-process"` {
		t.Errorf("the spec read %s off state.lastAction, want "+
			"\"unconfirmed/same-process\"; a system window held the focused window, "+
			"so whether the app received the tap is exactly what nobody can say",
			reported)
	}
}
