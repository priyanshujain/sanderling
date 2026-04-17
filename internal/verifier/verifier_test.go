package verifier

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/priyanshujain/uatu/internal/ltl"
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
const screen = __uatu__.extract(state => state.snapshots.screen ?? "");
const balance = __uatu__.extract(state => state.snapshots["ledger.balance"] ?? 0);

globalThis.screen = screen;
globalThis.balance = balance;

globalThis.properties = {
  balanceNonNegative: __uatu__.always(() => balance.current >= 0),
};

globalThis.actions = __uatu__.actions(() => [
  __uatu__.tap({ on: "id:home_button" }),
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

	if err := verifier.PushSnapshot(Snapshots{
		"screen":         json.RawMessage(`"customer_ledger"`),
		"ledger.balance": json.RawMessage(`1500`),
	}, nil); err != nil {
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
	if err := verifier.PushSnapshot(Snapshots{"ledger.balance": json.RawMessage(`2000`)}, nil); err != nil {
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
		if err := verifier.PushSnapshot(Snapshots{"ledger.balance": raw}, nil); err != nil {
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
	_ = verifier.PushSnapshot(Snapshots{}, nil)

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
		const tapHome = __uatu__.actions(() => [__uatu__.tap({ on: "id:home" })]);
		const tapAway = __uatu__.actions(() => [__uatu__.tap({ on: "id:away" })]);
		globalThis.actions = __uatu__.weighted(
			[1, tapHome],
			[99, tapAway],
		);
	`)
	_ = verifier.PushSnapshot(Snapshots{}, nil)

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
		globalThis.actions = __uatu__.actions(() => []);
	`)
	_ = verifier.PushSnapshot(Snapshots{}, nil)

	_, err := verifier.NextAction()
	if !errors.Is(err, ErrNoAction) {
		t.Errorf("expected ErrNoAction, got %v", err)
	}
}

func TestInputText_RoundTrip(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.actions = __uatu__.actions(() => [
			__uatu__.inputText({ into: "id:phone", text: "+919876543210" }),
		]);
	`)
	_ = verifier.PushSnapshot(Snapshots{}, nil)

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
		globalThis.captured = __uatu__.extract(state => state.snapshots["k"]);
	`)
	if err := verifier.PushSnapshot(Snapshots{"k": json.RawMessage(`"hello"`)}, nil); err != nil {
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
