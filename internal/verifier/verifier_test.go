package verifier

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/priyanshujain/sanderling/internal/ltl"
)

func newVerifier(t *testing.T, options ...Option) *Verifier {
	t.Helper()
	verifier, err := New(options...)
	if err != nil {
		t.Fatal(err)
	}
	return verifier
}

func mustLoad(t *testing.T, verifier *Verifier, source string) {
	t.Helper()
	if err := verifier.Load(source); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

const helloSpec = `
const screen = __sanderling__.extract(state => state.snapshots.screen ?? "");
const balance = __sanderling__.extract(state => state.snapshots["ledger.balance"] ?? 0);

globalThis.screen = screen;
globalThis.balance = balance;

globalThis.properties = {
  balanceNonNegative: __sanderling__.always(() => balance.current >= 0),
};

globalThis.actions = __sanderling__.actions(() => [
  __sanderling__.tap({ on: "id:home_button" }),
]);
`

func TestLoad_ExposesRuntimeBindings(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, helloSpec)
	if len(verifier.extractors) != 2 {
		t.Errorf("extractors registered: got %d, want 2", len(verifier.extractors))
	}
	if len(verifier.formulas) != 1 {
		t.Errorf("formulas registered: got %d, want 1", len(verifier.formulas))
	}
	if _, ok := verifier.properties["balanceNonNegative"]; !ok {
		t.Errorf("balanceNonNegative property missing: %+v", verifier.properties)
	}
}

func TestPushSnapshot_UpdatesExtractorCurrentAndPrevious(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, helloSpec)

	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{
		"screen":         json.RawMessage(`"customer_ledger"`),
		"ledger.balance": json.RawMessage(`1500`),
	}}); err != nil {
		t.Fatal(err)
	}

	screenValue := verifier.runtime.GlobalObject().Get("screen").ToObject(verifier.runtime)
	if screenValue.Get("current").String() != "customer_ledger" {
		t.Errorf("screen.current wrong: %v", screenValue.Get("current"))
	}

	balanceValue := verifier.runtime.GlobalObject().Get("balance").ToObject(verifier.runtime)
	if balanceValue.Get("current").ToInteger() != 1500 {
		t.Errorf("balance.current wrong: %v", balanceValue.Get("current"))
	}

	// Push again: previous should mirror the prior current.
	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{"ledger.balance": json.RawMessage(`2000`)}}); err != nil {
		t.Fatal(err)
	}
	balanceValue = verifier.runtime.GlobalObject().Get("balance").ToObject(verifier.runtime)
	if balanceValue.Get("previous").ToInteger() != 1500 {
		t.Errorf("balance.previous wrong: %v", balanceValue.Get("previous"))
	}
	if balanceValue.Get("current").ToInteger() != 2000 {
		t.Errorf("balance.current wrong: %v", balanceValue.Get("current"))
	}
}

func TestEvaluateProperties_HoldsThenViolates(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, helloSpec)

	cases := []struct {
		balance int
		want    ltl.Verdict
	}{
		{1500, ltl.VerdictHolds},
		{0, ltl.VerdictHolds},
		{-1, ltl.VerdictViolated},
		{500, ltl.VerdictViolated}, // sticky
	}
	for index, testCase := range cases {
		raw, _ := json.Marshal(testCase.balance)
		if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{"ledger.balance": raw}}); err != nil {
			t.Fatal(err)
		}
		verdicts := verifier.EvaluateProperties()
		if got := verdicts["balanceNonNegative"]; got != testCase.want {
			t.Errorf("step %d (balance=%d): got %v, want %v", index, testCase.balance, got, testCase.want)
		}
	}
}

func TestNextAction_FromActionsGenerator(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, helloSpec)
	_ = verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}})

	action, err := verifier.NextAction()
	if err != nil {
		t.Fatal(err)
	}
	if action.Kind != ActionKindTap {
		t.Errorf("kind: got %v, want Tap", action.Kind)
	}
	if action.On != "id:home_button" {
		t.Errorf("selector: got %q, want id:home_button", action.On)
	}
}

func TestNextAction_WeightedSelectsByWeight(t *testing.T) {
	verifier := newVerifier(t, WithRand(rand.New(rand.NewPCG(42, 0))))
	mustLoad(t, verifier, `
		const tapHome = __sanderling__.actions(() => [__sanderling__.tap({ on: "id:home" })]);
		const tapAway = __sanderling__.actions(() => [__sanderling__.tap({ on: "id:away" })]);
		globalThis.actions = __sanderling__.weighted(
			[1, tapHome],
			[99, tapAway],
		);
	`)
	_ = verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}})

	awayCount := 0
	homeCount := 0
	for range 200 {
		action, err := verifier.NextAction()
		if err != nil {
			t.Fatal(err)
		}
		switch action.On {
		case "id:home":
			homeCount++
		case "id:away":
			awayCount++
		}
	}
	if awayCount <= homeCount {
		t.Errorf("expected away-skewed distribution, got home=%d away=%d", homeCount, awayCount)
	}
}

func TestNextAction_EmptyGeneratorReturnsErrNoAction(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.actions = __sanderling__.actions(() => []);
	`)
	_ = verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}})

	_, err := verifier.NextAction()
	if !errors.Is(err, ErrNoAction) {
		t.Errorf("expected ErrNoAction, got %v", err)
	}
}

func TestInputText_RoundTrip(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.actions = __sanderling__.actions(() => [
			__sanderling__.inputText({ into: "id:phone", text: "+919876543210" }),
		]);
	`)
	_ = verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}})

	action, err := verifier.NextAction()
	if err != nil {
		t.Fatal(err)
	}
	if action.Kind != ActionKindInputText {
		t.Errorf("kind: %v", action.Kind)
	}
	if action.On != "id:phone" || action.Text != "+919876543210" {
		t.Errorf("payload wrong: %+v", action)
	}
}

func TestPushSnapshot_FeedsSnapshotsToExtractorState(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.captured = __sanderling__.extract(state => state.snapshots["k"]);
	`)
	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{"k": json.RawMessage(`"hello"`)}}); err != nil {
		t.Fatal(err)
	}
	value := verifier.runtime.GlobalObject().Get("captured").ToObject(verifier.runtime).Get("current")
	if value.String() != "hello" {
		t.Errorf("snapshot value not propagated: %v", value)
	}
}

func TestLoad_PropagatesSyntaxError(t *testing.T) {
	verifier := newVerifier(t)
	err := verifier.Load(`const x = ;`)
	if err == nil || !strings.Contains(err.Error(), "run spec") {
		t.Errorf("expected run-spec error, got %v", err)
	}
}

func TestEvaluateProperties_ThrowingPredicateDoesNotPanic(t *testing.T) {
	const spec = `
globalThis.properties = {
  broken: __sanderling__.always(() => { throw new Error("bad predicate"); }),
};
`
	verifier := newVerifier(t)
	mustLoad(t, verifier, spec)

	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}}); err != nil {
		t.Fatal(err)
	}

	verdicts := verifier.EvaluateProperties()
	if got := verdicts["broken"]; got != ltl.VerdictViolated {
		t.Errorf("verdict: got %v, want %v", got, ltl.VerdictViolated)
	}

	predicateErr := verifier.PredicateError("broken")
	if predicateErr == nil {
		t.Fatal("PredicateError: got nil, want non-nil")
	}
	if !strings.Contains(predicateErr.Error(), "bad predicate") {
		t.Errorf("PredicateError message: got %q, want to contain %q", predicateErr.Error(), "bad predicate")
	}
}

func TestLoad_AcceptsSpecWithoutPropertiesOrActions(t *testing.T) {
	verifier := newVerifier(t)
	if err := verifier.Load(`const noop = 1;`); err != nil {
		t.Fatal(err)
	}
	if got := verifier.EvaluateProperties(); len(got) != 0 {
		t.Errorf("no properties expected, got %v", got)
	}
	if _, err := verifier.NextAction(); !errors.Is(err, ErrNoAction) {
		t.Errorf("expected ErrNoAction, got %v", err)
	}
}

// PredicateError must reflect the most recent step's predicate result, not a
// latched first-step error. The runner logs PredicateError once per step; if it
// stays pinned to step 1 forever, downstream debugging looks frozen even though
// the underlying state is changing.
func TestPredicateError_ReflectsCurrentStepNotFirstStep(t *testing.T) {
	const spec = `
globalThis.counter = __sanderling__.extract(state => state.snapshots["count"]);
globalThis.properties = {
  reportsCounter: __sanderling__.always(() => { throw new Error("count=" + counter.current); }),
};
`
	verifier := newVerifier(t)
	mustLoad(t, verifier, spec)

	for step := 1; step <= 3; step++ {
		raw := json.RawMessage([]byte{'"', byte('0' + step), '"'})
		if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{"count": raw}}); err != nil {
			t.Fatal(err)
		}
		_ = verifier.EvaluateProperties()

		got := verifier.PredicateError("reportsCounter")
		if got == nil {
			t.Fatalf("step %d: PredicateError = nil, want non-nil", step)
		}
		want := "count=" + string(rune('0'+step))
		if !strings.Contains(got.Error(), want) {
			t.Errorf("step %d: PredicateError = %q, want to contain %q", step, got.Error(), want)
		}
	}
}
