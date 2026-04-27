package verifier

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/dop251/goja"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
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

func TestNextAction_SetupTakesPrecedenceWhenYielding(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.setup = __sanderling__.actions(() => [__sanderling__.tap({ on: "id:setup" })]);
		globalThis.actions = __sanderling__.actions(() => [__sanderling__.tap({ on: "id:main" })]);
	`)
	_ = verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}})

	action, err := verifier.NextAction()
	if err != nil {
		t.Fatal(err)
	}
	if action.On != "id:setup" {
		t.Errorf("setup precedence: got %q, want id:setup", action.On)
	}
}

func TestNextAction_FallsThroughToActionsWhenSetupEmpty(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.setup = __sanderling__.actions(() => []);
		globalThis.actions = __sanderling__.actions(() => [__sanderling__.tap({ on: "id:main" })]);
	`)
	_ = verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}})

	action, err := verifier.NextAction()
	if err != nil {
		t.Fatal(err)
	}
	if action.On != "id:main" {
		t.Errorf("fallthrough: got %q, want id:main", action.On)
	}
}

func TestNextAction_SetupReengagesAfterRegression(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.loggedIn = __sanderling__.extract(state => state.snapshots["loggedIn"] === true);
		globalThis.setup = __sanderling__.actions(() => {
			if (loggedIn.current) return [];
			return [__sanderling__.tap({ on: "id:login" })];
		});
		globalThis.actions = __sanderling__.actions(() => [__sanderling__.tap({ on: "id:main" })]);
	`)

	push := func(loggedIn bool) {
		raw := json.RawMessage(`false`)
		if loggedIn {
			raw = json.RawMessage(`true`)
		}
		if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{"loggedIn": raw}}); err != nil {
			t.Fatal(err)
		}
	}

	push(false)
	action, err := verifier.NextAction()
	if err != nil {
		t.Fatal(err)
	}
	if action.On != "id:login" {
		t.Fatalf("step 1 (logged out): got %q, want id:login", action.On)
	}

	push(true)
	action, err = verifier.NextAction()
	if err != nil {
		t.Fatal(err)
	}
	if action.On != "id:main" {
		t.Fatalf("step 2 (logged in): got %q, want id:main", action.On)
	}

	push(false)
	action, err = verifier.NextAction()
	if err != nil {
		t.Fatal(err)
	}
	if action.On != "id:login" {
		t.Fatalf("step 3 (regressed): got %q, want id:login", action.On)
	}
}

func TestNextAction_NoSetupRegistered(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.actions = __sanderling__.actions(() => [__sanderling__.tap({ on: "id:main" })]);
	`)
	_ = verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}})

	if verifier.setupGenerator != nil {
		t.Errorf("setupGenerator should be nil when spec does not export setup")
	}
	action, err := verifier.NextAction()
	if err != nil {
		t.Fatal(err)
	}
	if action.On != "id:main" {
		t.Errorf("got %q, want id:main", action.On)
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

// TestSelectorPath_ScopedDescent ensures the JS-side `find([{...}, {...}])`
// shape walks each segment scoped under the previous match.
func TestSelectorPath_ScopedDescent(t *testing.T) {
	const treeJSON = `{
	  "attributes": {"resource-id": "rootView", "bounds": "[0,0,1080,2340]"},
	  "children": [
	    {
	      "attributes": {"testTag": "HomeScreen", "bounds": "[0,0,540,2340]"},
	      "children": [
	        {
	          "attributes": {"testTag": "AccountCard", "bounds": "[0,0,540,200]"},
	          "children": [
	            {"attributes": {"testTag": "AccountName", "text": "Checking", "bounds": "[10,10,200,40]"}, "children": []}
	          ]
	        }
	      ]
	    },
	    {
	      "attributes": {"testTag": "LedgerScreen", "bounds": "[540,0,1080,2340]"},
	      "children": [
	        {"attributes": {"testTag": "AccountName", "text": "Other", "bounds": "[600,10,800,40]"}, "children": []}
	      ]
	    }
	  ]
	}`
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.found = __sanderling__.extract(state =>
			state.ax.find([{ testTag: "HomeScreen" }, { testTag: "AccountCard" }, { testTag: "AccountName" }])
		);
		globalThis.foundUnreachable = __sanderling__.extract(state =>
			state.ax.find([{ testTag: "LedgerScreen" }, { testTag: "AccountCard" }])
		);
		globalThis.allInHome = __sanderling__.extract(state =>
			state.ax.findAll([{ testTag: "HomeScreen" }, { testTag: "AccountName" }])
		);
	`)
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree}); err != nil {
		t.Fatal(err)
	}
	found := verifier.runtime.GlobalObject().Get("found").ToObject(verifier.runtime).Get("current")
	if found == nil || goja.IsUndefined(found) {
		t.Fatal("expected path lookup to find AccountName under HomeScreen > AccountCard")
	}
	text := found.ToObject(verifier.runtime).Get("text")
	if text.String() != "Checking" {
		t.Fatalf("text = %q, want Checking", text.String())
	}
	unreachable := verifier.runtime.GlobalObject().Get("foundUnreachable").ToObject(verifier.runtime).Get("current")
	if !goja.IsUndefined(unreachable) {
		t.Fatalf("AccountCard is not under LedgerScreen, expected undefined, got %v", unreachable)
	}
	allInHome := verifier.runtime.GlobalObject().Get("allInHome").ToObject(verifier.runtime).Get("current")
	allObject := allInHome.ToObject(verifier.runtime)
	length := allObject.Get("length").ToInteger()
	if length != 1 {
		t.Fatalf("findAll path length = %d, want 1 (Checking only, not Other in LedgerScreen)", length)
	}
}

// TestSelector_BooleanValue ensures a native JS boolean in the selector
// (e.g. `find({ focused: true })`) matches the "true"/"false" string
// serialization the hierarchy uses for boolean state attributes.
func TestSelector_BooleanValue(t *testing.T) {
	const treeJSON = `{
	  "attributes": {"resource-id": "root", "bounds": "[0,0,100,100]"},
	  "children": [
	    {"attributes": {"testTag": "EmailField", "bounds": "[0,0,100,40]"}, "focused": true, "children": []},
	    {"attributes": {"testTag": "PasswordField", "bounds": "[0,40,100,80]"}, "focused": false, "children": []}
	  ]
	}`
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.focusedTag = __sanderling__.extract(state =>
			state.ax.find({ focused: true })?.attrs?.testTag ?? null
		);
	`)
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree}); err != nil {
		t.Fatal(err)
	}
	got := verifier.runtime.GlobalObject().Get("focusedTag").ToObject(verifier.runtime).Get("current")
	if got == nil || got.String() != "EmailField" {
		t.Fatalf("expected EmailField, got %v", got)
	}
}

// TestFrom_SeededReplayIsDeterministic guarantees `from()` over a per-step
// dynamic array picks the same element under the same seed across runs. The
// folio spec relies on this to replace Math.random() in account-card taps.
func TestFrom_SeededReplayIsDeterministic(t *testing.T) {
	pickedSequence := func(seed uint64) []string {
		verifier := newVerifier(t, WithRand(rand.New(rand.NewPCG(seed, 0))))
		mustLoad(t, verifier, `
			globalThis.actions = __sanderling__.actions(() => {
				const cards = ["card_a", "card_b", "card_c", "card_d"];
				return [__sanderling__.tap({ on: __sanderling__.from(cards).generate() })];
			});
		`)
		_ = verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}})
		var picks []string
		for range 20 {
			action, err := verifier.NextAction()
			if err != nil {
				t.Fatal(err)
			}
			picks = append(picks, action.On)
		}
		return picks
	}
	first := pickedSequence(1234)
	second := pickedSequence(1234)
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("step %d: %q != %q (replay not deterministic)", i, first[i], second[i])
		}
	}
	other := pickedSequence(5678)
	identical := true
	for i := range first {
		if first[i] != other[i] {
			identical = false
			break
		}
	}
	if identical {
		t.Fatal("expected different seeds to produce different pick sequences")
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

func TestOverrideExtractorValues_PreservesPrevious(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, helloSpec)

	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{"ledger.balance": json.RawMessage(`100`)}}); err != nil {
		t.Fatal(err)
	}
	if err := verifier.OverrideExtractorValues(map[int]json.RawMessage{1: json.RawMessage(`777`)}); err != nil {
		t.Fatal(err)
	}
	balance := verifier.runtime.GlobalObject().Get("balance").ToObject(verifier.runtime)
	if balance.Get("current").ToInteger() != 777 {
		t.Errorf("override didn't take: current=%v", balance.Get("current"))
	}

	// Next push: previous mirrors the *override*, not the snapshot value.
	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{"ledger.balance": json.RawMessage(`200`)}}); err != nil {
		t.Fatal(err)
	}
	balance = verifier.runtime.GlobalObject().Get("balance").ToObject(verifier.runtime)
	if balance.Get("previous").ToInteger() != 777 {
		t.Errorf("previous should reflect override, got %v", balance.Get("previous"))
	}
}

func TestOverrideExtractorValues_NilIsNoop(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, helloSpec)
	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{"ledger.balance": json.RawMessage(`42`)}}); err != nil {
		t.Fatal(err)
	}
	if err := verifier.OverrideExtractorValues(nil); err != nil {
		t.Fatal(err)
	}
	if err := verifier.OverrideExtractorValues(map[int]json.RawMessage{}); err != nil {
		t.Fatal(err)
	}
	balance := verifier.runtime.GlobalObject().Get("balance").ToObject(verifier.runtime)
	if balance.Get("current").ToInteger() != 42 {
		t.Errorf("expected snapshot-driven current to remain 42, got %v", balance.Get("current"))
	}
}

func TestOverrideExtractorValues_UnknownIndexErrors(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, helloSpec)
	if err := verifier.PushSnapshot(SnapshotInput{}); err != nil {
		t.Fatal(err)
	}
	err := verifier.OverrideExtractorValues(map[int]json.RawMessage{99: json.RawMessage(`1`)})
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Errorf("expected out-of-range error, got %v", err)
	}
}
