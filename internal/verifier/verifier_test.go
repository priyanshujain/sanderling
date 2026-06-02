package verifier

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dop251/goja"

	"github.com/priyanshujain/sanderling/internal/bundler"
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

// bundleActionSpec bundles an inline TS spec authored against @sanderling/spec
// together with the goja runtime entry, so loading it installs
// __sanderlingNextAction__ (the shared picker). Action targets must be resolved
// ax elements (carrying x/y) or builtins; raw selector strings no longer
// resolve to coordinates in the unified contract.
func bundleActionSpec(t *testing.T, specSource string) string {
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

// loadActionSpec bundles and loads an inline authored spec into the verifier.
func loadActionSpec(t *testing.T, verifier *Verifier, specSource string) {
	t.Helper()
	mustLoad(t, verifier, bundleActionSpec(t, specSource))
}

const helloSpec = `
const screen = __sanderling__.extract(state => state.snapshots.screen ?? "");
const balance = __sanderling__.extract(state => state.snapshots["ledger.balance"] ?? 0);

globalThis.screen = screen;
globalThis.balance = balance;

globalThis.properties = {
  balanceNonNegative: __sanderling__.always(() => balance.current >= 0),
};
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

func TestNewlyViolatedProperties_OnsetOnly(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, helloSpec)

	// Balance trajectory: holds, holds, violates, stays violated, stays violated.
	// Onset must appear only on step 3 even though the residual stays false
	// through steps 4 and 5 (LTL `always` sticky semantics).
	balances := []int{1500, 1500, -1, 500, 500}
	for index, balance := range balances {
		raw, _ := json.Marshal(balance)
		if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{"ledger.balance": raw}}); err != nil {
			t.Fatal(err)
		}
		_ = verifier.EvaluateProperties()
		got := verifier.NewlyViolatedProperties()
		step := index + 1
		if step == 3 {
			want := []string{"balanceNonNegative"}
			if !slices.Equal(got, want) {
				t.Errorf("step %d (onset): got %v, want %v", step, got, want)
			}
		} else if len(got) != 0 {
			t.Errorf("step %d: expected empty onset set, got %v", step, got)
		}
	}
}

func TestNewlyViolatedProperties_FirstStepViolation(t *testing.T) {
	const spec = `
globalThis.properties = {
  alwaysFalse: __sanderling__.always(() => false),
};
`
	verifier := newVerifier(t)
	mustLoad(t, verifier, spec)

	for step := 1; step <= 3; step++ {
		if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}}); err != nil {
			t.Fatal(err)
		}
		_ = verifier.EvaluateProperties()
		got := verifier.NewlyViolatedProperties()
		if step == 1 {
			want := []string{"alwaysFalse"}
			if !slices.Equal(got, want) {
				t.Errorf("step 1 (onset): got %v, want %v", got, want)
			}
		} else if len(got) != 0 {
			t.Errorf("step %d: expected empty onset set after first-step violation, got %v", step, got)
		}
	}
}

func TestNewlyViolatedProperties_MultipleProperties(t *testing.T) {
	const spec = `
const a = __sanderling__.extract(state => state.snapshots["a"] ?? 0);
const b = __sanderling__.extract(state => state.snapshots["b"] ?? 0);
globalThis.properties = {
  propA: __sanderling__.always(() => a.current >= 0),
  propB: __sanderling__.always(() => b.current >= 0),
};
`
	verifier := newVerifier(t)
	mustLoad(t, verifier, spec)

	// propA violates at step 2, propB violates at step 4. Each must surface
	// only on its own onset step.
	aValues := []int{1, -1, -1, -1}
	bValues := []int{1, 1, 1, -1}
	expectOnset := map[int][]string{
		2: {"propA"},
		4: {"propB"},
	}
	for index := range aValues {
		aRaw, _ := json.Marshal(aValues[index])
		bRaw, _ := json.Marshal(bValues[index])
		if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{"a": aRaw, "b": bRaw}}); err != nil {
			t.Fatal(err)
		}
		_ = verifier.EvaluateProperties()
		got := verifier.NewlyViolatedProperties()
		step := index + 1
		want := expectOnset[step]
		if !slices.Equal(got, want) {
			t.Errorf("step %d: got %v, want %v", step, got, want)
		}
	}
}

func TestNewlyViolatedProperties_DeterministicOrder(t *testing.T) {
	const spec = `
globalThis.properties = {
  zebra:  __sanderling__.always(() => false),
  apple:  __sanderling__.always(() => false),
  mango:  __sanderling__.always(() => false),
};
`
	verifier := newVerifier(t)
	mustLoad(t, verifier, spec)

	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}}); err != nil {
		t.Fatal(err)
	}
	_ = verifier.EvaluateProperties()
	got := verifier.NewlyViolatedProperties()
	want := []string{"apple", "mango", "zebra"}
	if !slices.Equal(got, want) {
		t.Errorf("onset order: got %v, want %v (sorted lexicographically)", got, want)
	}
}

func TestNextAction_FromActionsGenerator(t *testing.T) {
	const treeJSON = `{
	  "attributes": {"resource-id": "root", "bounds": "[0,0,100,100]"},
	  "children": [
	    {"attributes": {"resource-id": "home_button", "bounds": "[0,40,100,80]"}, "clickable": true, "enabled": true, "children": []}
	  ]
	}`
	verifier := newVerifier(t)
	loadActionSpec(t, verifier, `
		import { actions, Tap } from "@sanderling/spec";
		globalThis.actions = actions(() => {
			const home = state.ax.find("id:home_button");
			return home ? [Tap({ on: home })] : [];
		});
	`)
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatal(err)
	}
	_ = verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree})

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
	const treeJSON = `{
	  "attributes": {"resource-id": "root", "bounds": "[0,0,100,100]"},
	  "children": [
	    {"attributes": {"resource-id": "home", "bounds": "[0,0,100,40]"}, "clickable": true, "enabled": true, "children": []},
	    {"attributes": {"resource-id": "away", "bounds": "[0,40,100,80]"}, "clickable": true, "enabled": true, "children": []}
	  ]
	}`
	verifier := newVerifier(t, WithSeed(42))
	loadActionSpec(t, verifier, `
		import { actions, weighted, Tap } from "@sanderling/spec";
		const tapHome = actions(() => {
			const home = state.ax.find("id:home");
			return home ? [Tap({ on: home })] : [];
		});
		const tapAway = actions(() => {
			const away = state.ax.find("id:away");
			return away ? [Tap({ on: away })] : [];
		});
		globalThis.actions = weighted([1, tapHome], [99, tapAway]);
	`)
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatal(err)
	}
	_ = verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree})

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
	loadActionSpec(t, verifier, `
		import { actions } from "@sanderling/spec";
		globalThis.actions = actions(() => []);
	`)
	_ = verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}})

	_, err := verifier.NextAction()
	if !errors.Is(err, ErrNoAction) {
		t.Errorf("expected ErrNoAction, got %v", err)
	}
}

// setupTree carries the three targets the setup-precedence tests resolve.
const setupTree = `{
  "attributes": {"resource-id": "root", "bounds": "[0,0,100,120]"},
  "children": [
    {"attributes": {"resource-id": "setup", "bounds": "[0,0,100,40]"}, "clickable": true, "enabled": true, "children": []},
    {"attributes": {"resource-id": "main", "bounds": "[0,40,100,80]"}, "clickable": true, "enabled": true, "children": []},
    {"attributes": {"resource-id": "login", "bounds": "[0,80,100,120]"}, "clickable": true, "enabled": true, "children": []}
  ]
}`

func TestNextAction_SetupTakesPrecedenceWhenYielding(t *testing.T) {
	verifier := newVerifier(t)
	loadActionSpec(t, verifier, `
		import { actions, Tap } from "@sanderling/spec";
		globalThis.setup = actions(() => {
			const target = state.ax.find("id:setup");
			return target ? [Tap({ on: target })] : [];
		});
		globalThis.actions = actions(() => {
			const target = state.ax.find("id:main");
			return target ? [Tap({ on: target })] : [];
		});
	`)
	tree, err := hierarchy.Parse(setupTree)
	if err != nil {
		t.Fatal(err)
	}
	_ = verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree})

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
	loadActionSpec(t, verifier, `
		import { actions, Tap } from "@sanderling/spec";
		globalThis.setup = actions(() => []);
		globalThis.actions = actions(() => {
			const target = state.ax.find("id:main");
			return target ? [Tap({ on: target })] : [];
		});
	`)
	tree, err := hierarchy.Parse(setupTree)
	if err != nil {
		t.Fatal(err)
	}
	_ = verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree})

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
	loadActionSpec(t, verifier, `
		import { actions, extract, Tap } from "@sanderling/spec";
		const loggedIn = extract(state => state.snapshots["loggedIn"] === true);
		globalThis.setup = actions(() => {
			if (loggedIn.current) return [];
			const target = state.ax.find("id:login");
			return target ? [Tap({ on: target })] : [];
		});
		globalThis.actions = actions(() => {
			const target = state.ax.find("id:main");
			return target ? [Tap({ on: target })] : [];
		});
	`)
	tree, err := hierarchy.Parse(setupTree)
	if err != nil {
		t.Fatal(err)
	}

	push := func(loggedIn bool) {
		raw := json.RawMessage(`false`)
		if loggedIn {
			raw = json.RawMessage(`true`)
		}
		if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{"loggedIn": raw}, Tree: tree}); err != nil {
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
	loadActionSpec(t, verifier, `
		import { actions, Tap } from "@sanderling/spec";
		globalThis.actions = actions(() => {
			const target = state.ax.find("id:main");
			return target ? [Tap({ on: target })] : [];
		});
	`)
	tree, err := hierarchy.Parse(setupTree)
	if err != nil {
		t.Fatal(err)
	}
	_ = verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree})

	action, err := verifier.NextAction()
	if err != nil {
		t.Fatal(err)
	}
	if action.On != "id:main" {
		t.Errorf("got %q, want id:main", action.On)
	}
}

// TestDoubleTapsBuiltin_TargetsClickable verifies the doubleTaps builtin emits
// a DoubleTap action targeting a clickable, enabled element's center.
func TestDoubleTapsBuiltin_TargetsClickable(t *testing.T) {
	const treeJSON = `{
	  "attributes": {"resource-id": "root", "bounds": "[0,0,100,100]"},
	  "children": [
	    {"attributes": {"testTag": "SubmitButton", "bounds": "[0,40,100,80]"}, "clickable": true, "enabled": true, "children": []}
	  ]
	}`
	verifier := newVerifier(t)
	loadActionSpec(t, verifier, `
		import { doubleTaps } from "@sanderling/spec";
		globalThis.actions = doubleTaps;
	`)
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree}); err != nil {
		t.Fatal(err)
	}
	action, err := verifier.NextAction()
	if err != nil {
		t.Fatal(err)
	}
	if action.Kind != ActionKindDoubleTap {
		t.Fatalf("kind = %v, want DoubleTap", action.Kind)
	}
	if action.X != 50 || action.Y != 60 {
		t.Errorf("coords = (%d,%d), want (50,60) at SubmitButton center", action.X, action.Y)
	}
	// Action-gated properties read lastAction.on to tell which target the
	// chooser hit. An empty On reduces those properties to vacuously-true and
	// they never fire on the real tap event.
	if action.On == "" {
		t.Fatal("On must be populated so action-gated properties can identify the target")
	}
	if !strings.Contains(action.On, "SubmitButton") {
		t.Errorf("On = %q, want a selector containing SubmitButton", action.On)
	}
	resolved := tree.Find(action.On)
	if resolved == nil {
		t.Fatalf("On = %q does not resolve in the same tree", action.On)
	}
	rx, ry := resolved.Bounds.Center()
	if rx != action.X || ry != action.Y {
		t.Errorf("On %q resolves to (%d,%d), want the picked element's center (%d,%d)",
			action.On, rx, ry, action.X, action.Y)
	}
}

func TestDoubleTap_RoundTrip(t *testing.T) {
	const treeJSON = `{
	  "attributes": {"resource-id": "root", "bounds": "[0,0,100,100]"},
	  "children": [
	    {"attributes": {"resource-id": "save", "bounds": "[0,0,100,40]"}, "clickable": true, "enabled": true, "children": []}
	  ]
	}`
	verifier := newVerifier(t)
	loadActionSpec(t, verifier, `
		import { actions, DoubleTap } from "@sanderling/spec";
		globalThis.actions = actions(() => {
			const save = state.ax.find("id:save");
			return save ? [DoubleTap({ on: save })] : [];
		});
	`)
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatal(err)
	}
	_ = verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree})

	action, err := verifier.NextAction()
	if err != nil {
		t.Fatal(err)
	}
	if action.Kind != ActionKindDoubleTap {
		t.Errorf("kind: got %v, want DoubleTap", action.Kind)
	}
	if action.On != "id:save" {
		t.Errorf("selector: got %q, want id:save", action.On)
	}
}

func TestInputText_RoundTrip(t *testing.T) {
	const treeJSON = `{
	  "attributes": {"resource-id": "root", "bounds": "[0,0,100,100]"},
	  "children": [
	    {"attributes": {"resource-id": "phone", "bounds": "[0,0,100,40]"}, "editable": true, "enabled": true, "children": []}
	  ]
	}`
	verifier := newVerifier(t)
	loadActionSpec(t, verifier, `
		import { actions, InputText } from "@sanderling/spec";
		globalThis.actions = actions(() => {
			const phone = state.ax.find("id:phone");
			return phone ? [InputText({ into: phone, text: "+919876543210" })] : [];
		});
	`)
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatal(err)
	}
	_ = verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree})

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

// TestTypingBuiltin_TargetsEditableField verifies the typing generator emits an
// InputText action aimed at an editable, enabled element's center and fills it
// with a corpus value, ignoring clickable-but-not-editable elements.
func TestTypingBuiltin_TargetsEditableField(t *testing.T) {
	const treeJSON = `{
	  "attributes": {"resource-id": "root", "bounds": "[0,0,100,100]"},
	  "children": [
	    {"attributes": {"testTag": "EmailField", "bounds": "[0,0,100,40]"}, "editable": true, "enabled": true, "children": []},
	    {"attributes": {"testTag": "SubmitButton", "bounds": "[0,40,100,80]"}, "clickable": true, "enabled": true, "children": []}
	  ]
	}`
	verifier := newVerifier(t)
	loadActionSpec(t, verifier, `
		import { typing } from "@sanderling/spec";
		globalThis.actions = typing;
	`)
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree}); err != nil {
		t.Fatal(err)
	}
	action, err := verifier.NextAction()
	if err != nil {
		t.Fatal(err)
	}
	if action.Kind != ActionKindInputText {
		t.Fatalf("kind = %v, want InputText", action.Kind)
	}
	if action.X != 50 || action.Y != 20 {
		t.Errorf("coords = (%d,%d), want (50,20) at EmailField center", action.X, action.Y)
	}
	// The typing builtin draws from corpus.ts INPUT_CORPUS; the single editable
	// field means any draw lands here, so the text must be a corpus member.
	if !slices.Contains(testInputCorpus, action.Text) {
		t.Errorf("text %q not drawn from the input corpus", action.Text)
	}
}

// testInputCorpus mirrors pkg/spec/src/corpus.ts INPUT_CORPUS so the typing
// builtin's emitted text can be asserted to come from the shared pool.
var testInputCorpus = []string{
	"", "a", strings.Repeat("a", 4096), "🙂🔥💸", "  ", "\t\n", "-1",
	"999999999999999999999", "0.0000001", "1e10", "'; DROP TABLE--",
	"<script>alert(1)</script>", "../../etc/passwd", "%s%n", "NaN",
}

// TestTypingBuiltin_NoEditableYieldsErrNoAction verifies the typing generator
// declines (ErrNoAction) when no editable element is present, so a weighted
// layer falls through to another generator.
func TestTypingBuiltin_NoEditableYieldsErrNoAction(t *testing.T) {
	const treeJSON = `{
	  "attributes": {"resource-id": "root", "bounds": "[0,0,100,100]"},
	  "children": [
	    {"attributes": {"testTag": "SubmitButton", "bounds": "[0,0,100,40]"}, "clickable": true, "enabled": true, "children": []}
	  ]
	}`
	verifier := newVerifier(t)
	loadActionSpec(t, verifier, `
		import { typing } from "@sanderling/spec";
		globalThis.actions = typing;
	`)
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree}); err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.NextAction(); !errors.Is(err, ErrNoAction) {
		t.Fatalf("err = %v, want ErrNoAction", err)
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

	witness := verifier.Witness("broken")
	if witness == nil {
		t.Fatal("Witness: got nil, want non-nil")
	}
	if !witness.IsError {
		t.Errorf("Witness.IsError = false, want true for a thrown predicate")
	}
	if !strings.Contains(witness.Reason, "bad predicate") {
		t.Errorf("Witness.Reason: got %q, want to contain %q", witness.Reason, "bad predicate")
	}
}

// An unbounded eventually that never fires stays pending during the run and is
// only reported by Finalize at run end. The witness records why it failed.
func TestFinalize_UnmetEventuallyReportedWithWitness(t *testing.T) {
	const spec = `
globalThis.flag = __sanderling__.extract(state => state.snapshots["flag"] ?? false, "flag");
globalThis.properties = {
  flagEventuallyTrue: __sanderling__.always(__sanderling__.eventually(() => flag.current === true)),
};
`
	verifier := newVerifier(t)
	mustLoad(t, verifier, spec)

	for step := 0; step < 3; step++ {
		if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{"flag": json.RawMessage(`false`)}}); err != nil {
			t.Fatal(err)
		}
		verdicts := verifier.EvaluateProperties()
		if got := verdicts["flagEventuallyTrue"]; got != ltl.VerdictPending {
			t.Fatalf("step %d: verdict = %v, want pending", step, got)
		}
	}

	ended := verifier.Finalize()
	if !slices.Contains(ended, "flagEventuallyTrue") {
		t.Fatalf("Finalize ended = %v, want to contain flagEventuallyTrue", ended)
	}
	witness := verifier.Witness("flagEventuallyTrue")
	if witness == nil {
		t.Fatal("Witness = nil after Finalize, want non-nil")
	}
	if !strings.Contains(witness.Reason, "eventually") {
		t.Errorf("Witness.Reason = %q, want to mention eventually", witness.Reason)
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
	const treeJSON = `{
	  "attributes": {"resource-id": "root", "bounds": "[0,0,100,200]"},
	  "children": [
	    {"attributes": {"resource-id": "card_a", "bounds": "[0,0,100,40]"}, "clickable": true, "enabled": true, "children": []},
	    {"attributes": {"resource-id": "card_b", "bounds": "[0,40,100,80]"}, "clickable": true, "enabled": true, "children": []},
	    {"attributes": {"resource-id": "card_c", "bounds": "[0,80,100,120]"}, "clickable": true, "enabled": true, "children": []},
	    {"attributes": {"resource-id": "card_d", "bounds": "[0,120,100,160]"}, "clickable": true, "enabled": true, "children": []}
	  ]
	}`
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatal(err)
	}
	pickedSequence := func(seed uint64) []string {
		verifier := newVerifier(t, WithSeed(seed))
		loadActionSpec(t, verifier, `
			import { actions, from, Tap } from "@sanderling/spec";
			const cards = ["id:card_a", "id:card_b", "id:card_c", "id:card_d"];
			globalThis.actions = actions(() => {
				const target = state.ax.find(from(cards).generate());
				return target ? [Tap({ on: target })] : [];
			});
		`)
		_ = verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree})
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

// A thrown predicate is a witnessed violation at the step it first throws, and
// the property latches violated thereafter (sticky always semantics). The
// witness captures the onset step's error text (count=1) and the extractor
// snapshot at that step; it does not keep updating to later counts because the
// verdict has latched.
func TestThrowingPredicate_WitnessCapturesOnsetStep(t *testing.T) {
	const spec = `
globalThis.counter = __sanderling__.extract(state => state.snapshots["count"], "counter");
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
		verdicts := verifier.EvaluateProperties()
		if got := verdicts["reportsCounter"]; got != ltl.VerdictViolated {
			t.Fatalf("step %d: verdict = %v, want violated", step, got)
		}
	}

	witness := verifier.Witness("reportsCounter")
	if witness == nil {
		t.Fatal("Witness = nil, want non-nil")
	}
	if witness.Step != 1 {
		t.Errorf("Witness.Step = %d, want 1 (onset)", witness.Step)
	}
	if !strings.Contains(witness.Reason, "count=1") {
		t.Errorf("Witness.Reason = %q, want to contain %q", witness.Reason, "count=1")
	}
	if got := string(witness.Extractors["counter"]); got != `"1"` {
		t.Errorf("Witness.Extractors[counter] = %s, want %q", got, `"1"`)
	}
}

func TestOverrideExtractorValues_PreservesPrevious(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, helloSpec)

	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{"ledger.balance": json.RawMessage(`100`)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.OverrideExtractorValues(map[int]json.RawMessage{1: json.RawMessage(`777`)}); err != nil {
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
	if _, err := verifier.OverrideExtractorValues(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.OverrideExtractorValues(map[int]json.RawMessage{}); err != nil {
		t.Fatal(err)
	}
	balance := verifier.runtime.GlobalObject().Get("balance").ToObject(verifier.runtime)
	if balance.Get("current").ToInteger() != 42 {
		t.Errorf("expected snapshot-driven current to remain 42, got %v", balance.Get("current"))
	}
}

func TestOverrideExtractorValues_UnknownIndexSkipped(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, helloSpec)
	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{"ledger.balance": json.RawMessage(`42`)}}); err != nil {
		t.Fatal(err)
	}
	skipped, err := verifier.OverrideExtractorValues(map[int]json.RawMessage{
		1:  json.RawMessage(`777`),
		99: json.RawMessage(`1`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skipped != 1 {
		t.Errorf("expected 1 skipped entry, got %d", skipped)
	}
	balance := verifier.runtime.GlobalObject().Get("balance").ToObject(verifier.runtime)
	if balance.Get("current").ToInteger() != 777 {
		t.Errorf("valid override should still apply alongside skipped one, got current=%v", balance.Get("current"))
	}
}

const objectExtractorSpec = `
const card = __sanderling__.extract(state => ({attrs: {testTag: "default"}, balance: 0}));
globalThis.card = card;

globalThis.properties = {
  hasTestTag: __sanderling__.always(() => typeof card.current.attrs.testTag === "string"),
};
`

// TestSelectorStringFromJS_CanonicalGrammar guarantees the selector tag stamped
// on returned AX nodes round-trips back to a parseable selector string when the
// node is later used as an action target. Without this, an action emitted from
// `tap({ on: state.ax.find({ testTag: "LoginEmail" }) })` ends up with
// `action.selector = "[object Object]"` in the trace.
func TestSelectorStringFromJS_CanonicalGrammar(t *testing.T) {
	const treeJSON = `{
	  "attributes": {"resource-id": "root", "bounds": "[0,0,100,100]"},
	  "children": [
	    {"attributes": {"testTag": "LoginScreen", "bounds": "[0,0,100,40]"},
	     "children": [
	       {"attributes": {"testTag": "LoginEmail", "bounds": "[0,0,100,20]"}, "editable": true, "enabled": true, "children": []}
	     ]}
	  ]
	}`
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.objectSelector = __sanderling__.extract(state =>
			state.ax.find({ testTag: "LoginScreen" })
		);
		globalThis.chainSelector = __sanderling__.extract(state =>
			state.ax.find([{ testTag: "LoginScreen" }, { testTag: "LoginEmail" }])
		);
		globalThis.stringSelector = __sanderling__.extract(state =>
			state.ax.find("testTag:LoginScreen")
		);
	`)
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree}); err != nil {
		t.Fatal(err)
	}

	read := func(name string) string {
		handle := verifier.runtime.GlobalObject().Get(name).ToObject(verifier.runtime)
		current := handle.Get("current")
		if goja.IsUndefined(current) || goja.IsNull(current) {
			return ""
		}
		object := current.ToObject(verifier.runtime)
		return object.Get(tagSelector).String()
	}
	cases := []struct {
		name string
		want string
	}{
		{"objectSelector", "testTag:LoginScreen"},
		{"chainSelector", "testTag:LoginScreen > testTag:LoginEmail"},
		{"stringSelector", "testTag:LoginScreen"},
	}
	for _, testCase := range cases {
		got := read(testCase.name)
		if got != testCase.want {
			t.Errorf("%s: got %q, want %q", testCase.name, got, testCase.want)
		}
		if strings.Contains(got, "[object") {
			t.Errorf("%s: selector contains garbage %q", testCase.name, got)
		}
	}
}

// TestChangedExtractors_DiffsBetweenSnapshots verifies the per-step diff
// surfaces only extractors whose value actually changed, keyed by name (or
// extractor_N fallback when unnamed).
func TestChangedExtractors_DiffsBetweenSnapshots(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		__sanderling__.extract(state => state.snapshots["a"] ?? 0, "alpha");
		__sanderling__.extract(state => state.snapshots["b"] ?? 0);
	`)

	push := func(a, b int) {
		raw := func(n int) json.RawMessage {
			body, _ := json.Marshal(n)
			return body
		}
		if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{"a": raw(a), "b": raw(b)}}); err != nil {
			t.Fatal(err)
		}
	}

	push(1, 1)
	first := verifier.ChangedExtractors()
	if _, ok := first["alpha"]; !ok {
		t.Errorf("step 1: expected alpha in diff (initial value), got %+v", first)
	}
	if _, ok := first["extractor_1"]; !ok {
		t.Errorf("step 1: expected extractor_1 fallback name in diff, got %+v", first)
	}

	push(1, 2)
	second := verifier.ChangedExtractors()
	if _, ok := second["alpha"]; ok {
		t.Errorf("step 2: alpha did not change, should not appear: %+v", second)
	}
	change, ok := second["extractor_1"]
	if !ok {
		t.Fatalf("step 2: expected extractor_1 in diff, got %+v", second)
	}
	if string(change.Prev) != "1" || string(change.Curr) != "2" {
		t.Errorf("step 2: extractor_1 diff prev=%s curr=%s, want 1 -> 2", change.Prev, change.Curr)
	}

	push(1, 2)
	third := verifier.ChangedExtractors()
	if len(third) != 0 {
		t.Errorf("step 3: nothing changed, diff should be empty, got %+v", third)
	}
}

// TestExtract_DefaultsAndNamedNames verifies bindExtract assigns a fallback
// `extractor_N` name when no name is supplied and respects an explicit one.
func TestExtract_DefaultsAndNamedNames(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		__sanderling__.extract(state => 1);
		__sanderling__.extract(state => 2, "ledgerRows");
		__sanderling__.extract(state => 3);
	`)
	if len(verifier.extractors) != 3 {
		t.Fatalf("extractors registered: got %d, want 3", len(verifier.extractors))
	}
	want := []string{"extractor_0", "ledgerRows", "extractor_2"}
	for i, name := range want {
		if got := verifier.extractors[i].name; got != name {
			t.Errorf("extractor %d name: got %q, want %q", i, got, name)
		}
	}
}

// TestSelectorStringFromJS_NullEmpty verifies that nil/undefined args produce
// an empty string instead of "null"/"undefined" garbage.
func TestSelectorStringFromJS_NullEmpty(t *testing.T) {
	verifier := newVerifier(t)
	if got := selectorStringFromJS(verifier.runtime, goja.Undefined()); got != "" {
		t.Errorf("undefined: got %q, want empty", got)
	}
	if got := selectorStringFromJS(verifier.runtime, goja.Null()); got != "" {
		t.Errorf("null: got %q, want empty", got)
	}
}

func TestOverrideExtractorValues_PropagatesNestedObjectFields(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, objectExtractorSpec)

	if err := verifier.PushSnapshot(SnapshotInput{}); err != nil {
		t.Fatal(err)
	}
	override := json.RawMessage(`{"attrs": {"testTag": "account-card"}, "balance": 12345}`)
	skipped, err := verifier.OverrideExtractorValues(map[int]json.RawMessage{0: override})
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 {
		t.Errorf("unexpected skipped count: %d", skipped)
	}

	card := verifier.runtime.GlobalObject().Get("card").ToObject(verifier.runtime)
	current := card.Get("current").ToObject(verifier.runtime)
	attrs := current.Get("attrs").ToObject(verifier.runtime)
	if got := attrs.Get("testTag").String(); got != "account-card" {
		t.Errorf("nested override missing: card.current.attrs.testTag = %q, want %q", got, "account-card")
	}
	if got := current.Get("balance").ToInteger(); got != 12345 {
		t.Errorf("scalar field missing: card.current.balance = %d, want 12345", got)
	}
}

// TestUnsupportedVerbs_CollectedDedupedInOrder drives the real host binding the
// shared picker invokes (__sanderlingHost__.reportUnsupported) and asserts the
// verifier collects each verb once, in first-seen order, for the run report.
func TestUnsupportedVerbs_CollectedDedupedInOrder(t *testing.T) {
	verifier := newVerifier(t)
	report, ok := goja.AssertFunction(
		verifier.runtime.GlobalObject().Get("__sanderlingHost__").
			ToObject(verifier.runtime).Get("reportUnsupported"),
	)
	if !ok {
		t.Fatal("reportUnsupported host binding missing")
	}
	for _, verb := range []string{"scrolls", "swipes", "scrolls", "longPresses"} {
		if _, err := report(goja.Undefined(), verifier.runtime.ToValue(verb)); err != nil {
			t.Fatalf("reportUnsupported(%q): %v", verb, err)
		}
	}
	got := verifier.UnsupportedVerbs()
	want := []string{"scrolls", "swipes", "longPresses"}
	if !slices.Equal(got, want) {
		t.Errorf("UnsupportedVerbs = %v, want %v", got, want)
	}
}
