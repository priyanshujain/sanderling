package runner

import (
	"context"
	"fmt"
	"testing"

	mockdriver "github.com/priyanshujain/sanderling/internal/driver/mock"
	"github.com/priyanshujain/sanderling/internal/hierarchy"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

// authoredParityTreeJSON holds one target per authored action shape: a button to
// tap, a field to type into, and a scrollable container to scroll.
const authoredParityTreeJSON = `{
  "attributes": {"bounds": "[0,0,400,800]"},
  "children": [
    {"attributes": {"resource-id": "Save", "text": "Save", "bounds": "[0,0,200,60]"}, "clickable": true, "enabled": true, "children": []},
    {"attributes": {"resource-id": "Amount", "class": "EditText", "hintText": "Amount", "bounds": "[0,100,400,160]"}, "enabled": true, "children": []},
    {"attributes": {"resource-id": "List", "scrollable": "true", "bounds": "[0,300,400,700]"}, "children": []}
  ]
}`

// TestPoliciesDispatchTheSameAuthoredAction is the authored-leaf half of the
// policy-parity claim (policy_parity_test.go in the verifier package covers the
// builtin verbs): for one authored action, the seeded picker and the model's
// candidate must reach the DRIVER as the same call. The two carry it
// differently on purpose (a selector target keeps its coordinates on one side
// and re-resolves on the other), so the comparison is what executes, never the
// struct.
func TestPoliciesDispatchTheSameAuthoredAction(t *testing.T) {
	fastFocusSettle(t)
	cases := []struct {
		name string
		leaf string
	}{
		{"tap an element", `const e = state.ax.find("id:Save"); return e ? [Tap({on: e})] : [];`},
		{"tap a selector", `return [Tap({on: "id:Save"})];`},
		{"double-tap an element", `const e = state.ax.find("id:Save"); return e ? [DoubleTap({on: e})] : [];`},
		{"long-press an element", `const e = state.ax.find("id:Save"); return e ? [LongPress({on: e})] : [];`},
		{"type into an element", `const e = state.ax.find("id:Amount"); return e ? [InputText({into: e, text: "42"})] : [];`},
		{"type into a selector", `return [InputText({into: "id:Amount", text: "42"})];`},
		{"scroll an element", `const e = state.ax.find("id:List"); return e ? [Scroll({direction: "down", in: e})] : [];`},
		{"scroll a selector", `return [Scroll({direction: "down", in: "id:List"})];`},
		{"scroll with no container", `return [Scroll({direction: "up"})];`},
		{"swipe with a duration", `return [Swipe({from: {x: 10, y: 600}, to: {x: 10, y: 100}, durationMillis: 300})];`},
		{"swipe with no duration", `return [Swipe({from: {x: 10, y: 600}, to: {x: 10, y: 100}})];`},
		{"press a key", `return [PressKey({key: "back"})];`},
		{"wait", `return [Wait({durationMillis: 5})];`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			spec := authoredSpec(testCase.leaf)
			tree := mustParseTree(t, authoredParityTreeJSON)

			seededVerifier := loadAuthoredSpec(t, spec, tree)
			seededAction, err := seededVerifier.NextAction()
			if err != nil {
				t.Fatalf("seeded picker declined the authored action: %v", err)
			}

			modelVerifier := loadAuthoredSpec(t, spec, tree)
			candidates := modelVerifier.Candidates(verifier.LabelSourceVisibleText)
			if len(candidates) != 1 {
				t.Fatalf("model was offered %d candidates, want the one authored action", len(candidates))
			}

			seeded := dispatchToMock(t, seededAction, tree)
			model := dispatchToMock(t, candidates[0].Action, tree)
			if len(seeded) != len(model) {
				t.Fatalf("policies dispatched different calls\n seeded=%v\n  model=%v", seeded, model)
			}
			for i := range seeded {
				if seeded[i] != model[i] {
					t.Errorf("call %d differs\n seeded=%+v\n  model=%+v", i, seeded[i], model[i])
				}
			}
		})
	}
}

// TestSeededAuthoredScrollDragsInsideItsContainer is the half of the scroll
// contract parity alone cannot check: both policies agreeing on a drag from a
// point to itself would still be two policies scrolling nothing. The list sits
// at y 300..700, so the gesture has to start inside it and travel upward to
// reveal what is below.
func TestSeededAuthoredScrollDragsInsideItsContainer(t *testing.T) {
	tree := mustParseTree(t, authoredParityTreeJSON)
	spec := authoredSpec(`const e = state.ax.find("id:List"); return e ? [Scroll({direction: "down", in: e})] : [];`)
	action, err := loadAuthoredSpec(t, spec, tree).NextAction()
	if err != nil {
		t.Fatalf("seeded picker declined the authored scroll: %v", err)
	}
	calls := dispatchToMock(t, action, tree)
	if len(calls) != 1 {
		t.Fatalf("want one driver call, got %v", calls)
	}
	swipe := calls[0]
	if swipe.FromY < 300 || swipe.FromY > 700 {
		t.Errorf("gesture starts at y=%d, outside the list (300..700)", swipe.FromY)
	}
	if swipe.ToY >= swipe.FromY {
		t.Errorf("scroll down must drag upward, got from y=%d to y=%d", swipe.FromY, swipe.ToY)
	}
}

func authoredSpec(leaf string) string {
	return fmt.Sprintf(`
import { actions, DoubleTap, InputText, LongPress, PressKey, Scroll, Swipe, Tap, Wait } from "@sanderling/spec";
globalThis.actions = actions(() => { %s });
`, leaf)
}

func loadAuthoredSpec(t *testing.T, spec string, tree *hierarchy.Tree) *verifier.Verifier {
	t.Helper()
	loaded, err := verifier.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.Load(bundleSpec(t, spec)); err != nil {
		t.Fatal(err)
	}
	if err := loaded.PushSnapshot(verifier.SnapshotInput{Snapshots: verifier.Snapshots{}, Tree: tree}); err != nil {
		t.Fatal(err)
	}
	return loaded
}

func mustParseTree(t *testing.T, treeJSON string) *hierarchy.Tree {
	t.Helper()
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatalf("parse tree: %v", err)
	}
	return tree
}

// dispatchToMock runs one action through applyAction and returns what the
// driver was asked to do.
func dispatchToMock(t *testing.T, action verifier.Action, tree *hierarchy.Tree) []mockdriver.Action {
	t.Helper()
	drv := mockdriver.New()
	skipped, err := applyAction(context.Background(), drv, action, tree)
	if err != nil {
		t.Fatalf("applyAction: %v", err)
	}
	if skipped != "" {
		t.Fatalf("action %+v never reached the driver: %s", action, skipped)
	}
	return drv.Actions()
}
