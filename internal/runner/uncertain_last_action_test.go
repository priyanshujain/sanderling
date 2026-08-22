package runner

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/priyanshujain/sanderling/internal/driver"
	mockdriver "github.com/priyanshujain/sanderling/internal/driver/mock"
)

// An apply error is not proof that nothing landed. An RPC deadline that fires
// after the tap was dispatched leaves the transaction committed, and a runner
// that reports "no action" for it hands
// submitCommitsOneTransactionPerAction a rise of one transaction against a
// window of zero submits: a conviction manufactured out of the runner's own
// uncertainty, on the property carrying most of the detection on android.
//
// The spec below is the real folio predicate pair, imported from the example,
// so what this asserts is the verdict the shipped property reaches.
const submitCountingSpecTemplate = `
import { actions, always, extract, next, Tap } from "@sanderling/spec";
import {
  committedTransactionsExceedSubmits,
  countSubmitsInWindow,
} from "%s";

let submits = 0;
const submitsInWindow = extract("submitsInWindow", state => {
  const window = countSubmitsInWindow({
    previousCount: submits,
    lastAction: state.lastAction,
    fresh: true,
  });
  submits = window.next;
  return window.reported;
});

const counts = extract("counts", state => {
  const text = state.ax.find("id:TxnCount")?.text;
  return text ? { Travel: parseInt(text, 10) } : null;
});

globalThis.properties = {
  submitCommitsOneTransactionPerAction: always(
    next(() =>
      !committedTransactionsExceedSubmits({
        countsBefore: counts.previous ?? null,
        countsAfter: counts.current,
        submitsInWindow: submitsInWindow.current,
      }),
    ),
  ),
};
globalThis.actions = actions(() => [Tap({ on: "id:TxnSubmit" })]);
`

const homeWithTxnCount = `{"attributes":{"resource-id":"HomeScreen"},"children":[
	{"attributes":{"resource-id":"TxnCount","text":"%d"},"children":[]},
	{"attributes":{"resource-id":"TxnSubmit","bounds":"[40,80,240,160]"},"children":[],"clickable":true,"enabled":true}
]}`

// dispatchThenFailDriver is the device condition the runner cannot see through:
// the tap reaches the app and commits, then the call the runner is waiting on
// times out. Every later hierarchy read shows the committed transactions.
type dispatchThenFailDriver struct {
	*mockdriver.Driver
	commitsPerTap int64
	committed     atomic.Int64
}

func (d *dispatchThenFailDriver) Tap(context.Context, int, int) error {
	return d.dispatchThenFail()
}

func (d *dispatchThenFailDriver) TapSelector(context.Context, string) error {
	return d.dispatchThenFail()
}

func (d *dispatchThenFailDriver) dispatchThenFail() error {
	d.committed.Add(d.commitsPerTap)
	return errors.New("rpc error: code = DeadlineExceeded desc = context deadline exceeded")
}

func (d *dispatchThenFailDriver) Snapshot(context.Context) (string, driver.Image, error) {
	return fmt.Sprintf(homeWithTxnCount, d.committed.Load()), driver.Image{}, nil
}

func (d *dispatchThenFailDriver) Hierarchy(context.Context) (string, error) {
	return fmt.Sprintf(homeWithTxnCount, d.committed.Load()), nil
}

func TestRunner_ApplyErrorAfterDispatchDoesNotConvictTheSubmitCountingProperty(t *testing.T) {
	predicates, err := filepath.Abs("../../examples/folio/sanderling/predicates.ts")
	if err != nil {
		t.Fatal(err)
	}
	spec := fmt.Sprintf(submitCountingSpecTemplate, predicates)

	run := func(t *testing.T, commitsPerTap int64) []ViolationRecord {
		t.Helper()
		state := newHarnessWithSpec(t, spec)
		device := &dispatchThenFailDriver{Driver: state.mock, commitsPerTap: commitsPerTap}

		summary := state.run(t, Options{MaxSteps: 2, Driver: device})
		if summary.Steps != 2 {
			t.Fatalf("steps = %d, want 2; the run never reached the step that judges the pair", summary.Steps)
		}
		if got := device.committed.Load(); got != commitsPerTap*2 {
			t.Fatalf("the device committed %d transaction(s), want %d; the taps never reached it",
				got, commitsPerTap*2)
		}
		return summary.Violations
	}

	t.Run("one transaction per tap is not a double submit", func(t *testing.T) {
		if violations := run(t, 1); len(violations) != 0 {
			t.Errorf("the counting property convicted a healthy app: %v\n"+
				"one transaction rose against a submit the runner dispatched but "+
				"could not confirm, and the spec was told no action happened",
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
