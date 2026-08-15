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

const relaunchBundleID = "app.folio"

// leavesForegroundAfterSubmitDriver is the device condition the app-scope guard
// exists for: the submit tap lands and commits, and the app is no longer the
// foreground app by the time the next step looks. Folio's transactions are in
// sqlite, so the commit survives the relaunch and the next hierarchy read shows
// it.
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
	return relaunchBundleID, nil
}

func (d *leavesForegroundAfterSubmitDriver) FocusedWindowApp(ctx context.Context) (string, error) {
	return d.ForegroundApp(ctx)
}

func (d *leavesForegroundAfterSubmitDriver) Snapshot(context.Context) (string, driver.Image, error) {
	return fmt.Sprintf(homeWithTxnCount, d.committed.Load()), driver.Image{}, nil
}

// A relaunch is not proof that nothing ran before it. The submit was dispatched
// and confirmed; what the relaunch changed is that the app restarted between
// the two readings the property compares. Reporting "no action" for it hands
// submitCommitsOneTransactionPerAction a transaction rise of one against a
// window of zero submits, which is the conviction #77 fixed for the apply-error
// path, manufactured here out of the scope guard instead.
func TestRunner_ARelaunchDoesNotConvictTheSubmitCountingProperty(t *testing.T) {
	predicates, err := filepath.Abs("../../examples/folio/sanderling/predicates.ts")
	if err != nil {
		t.Fatal(err)
	}
	spec := fmt.Sprintf(submitCountingSpecTemplate, predicates)

	run := func(t *testing.T, commitsPerTap int64) []ViolationRecord {
		t.Helper()
		state := newHarnessWithSpec(t, spec)
		device := &leavesForegroundAfterSubmitDriver{Driver: state.mock, commitsPerTap: commitsPerTap}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		summary, err := Run(ctx, Options{
			Duration:    time.Hour,
			IdleTimeout: 20 * time.Millisecond,
			MaxSteps:    2,
			BundleID:    relaunchBundleID,
			Driver:      device,
			Verifier:    state.verifier,
			TraceWriter: state.writer,
		})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if summary.Steps != 2 {
			t.Fatalf("steps = %d, want 2; the run never reached the step that judges the pair", summary.Steps)
		}
		if got := device.committed.Load(); got != commitsPerTap*2 {
			t.Fatalf("the device committed %d transaction(s), want %d; the taps never reached it",
				got, commitsPerTap*2)
		}
		relaunches := 0
		for _, action := range state.mock.Actions() {
			if action.Kind == mockdriver.ActionLaunch {
				relaunches++
			}
		}
		if relaunches == 0 {
			t.Fatal("the app was never relaunched, so the guard this test is about never ran")
		}
		return summary.Violations
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
