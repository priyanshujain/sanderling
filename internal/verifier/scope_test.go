package verifier

import (
	"errors"
	"testing"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// A clickable app button and a clickable soft-keyboard key sharing the screen.
// The keyboard key sits where a random tap would otherwise insert a glyph.
const scopedTreeJSON = `{
  "attributes": {"resource-id": "root", "bounds": "[0,0,100,500]", "package": "com.folio"},
  "children": [
    {"attributes": {"testTag": "SubmitButton", "bounds": "[0,40,100,80]", "package": "com.folio"}, "clickable": true, "editable": false, "enabled": true, "children": []},
    {"attributes": {"testTag": "Emoticon", "bounds": "[0,400,100,440]", "package": "com.google.android.inputmethod.latin"}, "clickable": true, "enabled": true, "children": []}
  ]
}`

func pushTree(t *testing.T, v *Verifier, treeJSON string) {
	t.Helper()
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree}); err != nil {
		t.Fatal(err)
	}
}

// TestTaps_ExcludeOffAppPackage proves the tap generator never targets the soft
// keyboard: with the app package set, only the in-app button is a candidate, so
// the result lands on its center regardless of seed.
func TestTaps_ExcludeOffAppPackage(t *testing.T) {
	verifier := newVerifier(t, WithAppPackage("com.folio"))
	loadActionSpec(t, verifier, `
		import { taps } from "@sanderling/spec";
		globalThis.actions = taps;
	`)
	pushTree(t, verifier, scopedTreeJSON)

	action, err := verifier.NextAction()
	if err != nil {
		t.Fatal(err)
	}
	if action.X != 50 || action.Y != 60 {
		t.Errorf("coords = (%d,%d), want (50,60) at SubmitButton; keyboard key leaked into targets", action.X, action.Y)
	}
}

// TestTyping_ExcludeOffAppPackage proves keyboard glyph buttons that report as
// editable never become typing targets once the app package is set.
func TestTyping_ExcludeOffAppPackage(t *testing.T) {
	const treeJSON = `{
	  "attributes": {"resource-id": "root", "bounds": "[0,0,100,500]", "package": "com.folio"},
	  "children": [
	    {"attributes": {"testTag": "NameField", "bounds": "[0,0,100,40]", "package": "com.folio"}, "editable": true, "enabled": true, "children": []},
	    {"attributes": {"testTag": "SearchBox", "bounds": "[0,400,100,440]", "package": "com.google.android.inputmethod.latin"}, "editable": true, "enabled": true, "children": []}
	  ]
	}`
	verifier := newVerifier(t, WithAppPackage("com.folio"))
	loadActionSpec(t, verifier, `
		import { typing } from "@sanderling/spec";
		globalThis.actions = typing;
	`)
	pushTree(t, verifier, treeJSON)

	action, err := verifier.NextAction()
	if err != nil {
		t.Fatal(err)
	}
	if action.X != 50 || action.Y != 20 {
		t.Errorf("coords = (%d,%d), want (50,20) at NameField; off-app field leaked into targets", action.X, action.Y)
	}
}

// TestSwipes_ExcludeOffAppPackage proves swipes anchor on app nodes only, so
// exploration never scrolls the keyboard's emoji list instead of the app.
func TestSwipes_ExcludeOffAppPackage(t *testing.T) {
	verifier := newVerifier(t, WithAppPackage("com.folio"))
	loadActionSpec(t, verifier, `
		import { swipes } from "@sanderling/spec";
		globalThis.actions = swipes;
	`)
	pushTree(t, verifier, scopedTreeJSON)

	// Both the root and SubmitButton (com.folio) are valid anchors; only the
	// keyboard key at center (50,420) must be excluded. Draw many times so the
	// invariant is not satisfied by a lucky seed.
	for i := range 200 {
		action, err := verifier.NextAction()
		if err != nil {
			t.Fatal(err)
		}
		if action.Kind != ActionKindSwipe {
			t.Fatalf("kind = %v, want Swipe", action.Kind)
		}
		if action.FromX == 50 && action.FromY == 420 {
			t.Fatalf("draw %d anchored on the keyboard key (50,420); off-app node leaked into swipe targets", i)
		}
	}
}

// TestTaps_AllOffAppYieldsErrNoAction proves the generator declines when every
// clickable node belongs to another package, so a weighted layer falls through
// instead of fuzzing the keyboard.
func TestTaps_AllOffAppYieldsErrNoAction(t *testing.T) {
	const treeJSON = `{
	  "attributes": {"resource-id": "root", "bounds": "[0,0,100,500]", "package": "com.folio"},
	  "children": [
	    {"attributes": {"testTag": "Emoticon", "bounds": "[0,400,100,440]", "package": "com.google.android.inputmethod.latin"}, "clickable": true, "enabled": true, "children": []}
	  ]
	}`
	verifier := newVerifier(t, WithAppPackage("com.folio"))
	loadActionSpec(t, verifier, `
		import { taps } from "@sanderling/spec";
		globalThis.actions = taps;
	`)
	pushTree(t, verifier, treeJSON)

	if _, err := verifier.NextAction(); !errors.Is(err, ErrNoAction) {
		t.Fatalf("err = %v, want ErrNoAction", err)
	}
}

// TestTaps_UnsetAppPackageKeepsAllNodes proves the filter is opt-in: with no app
// package configured, an off-app node is still a valid target (prior behavior).
func TestTaps_UnsetAppPackageKeepsAllNodes(t *testing.T) {
	const treeJSON = `{
	  "attributes": {"resource-id": "root", "bounds": "[0,0,100,500]", "package": "com.folio"},
	  "children": [
	    {"attributes": {"testTag": "Emoticon", "bounds": "[0,400,100,440]", "package": "com.google.android.inputmethod.latin"}, "clickable": true, "enabled": true, "children": []}
	  ]
	}`
	verifier := newVerifier(t)
	loadActionSpec(t, verifier, `
		import { taps } from "@sanderling/spec";
		globalThis.actions = taps;
	`)
	pushTree(t, verifier, treeJSON)

	action, err := verifier.NextAction()
	if err != nil {
		t.Fatalf("err = %v, want a tap on the only node when unscoped", err)
	}
	if action.X != 50 || action.Y != 420 {
		t.Errorf("coords = (%d,%d), want (50,420); unscoped run should target any node", action.X, action.Y)
	}
}

// TestLongPresses_TargetsClickableElement proves the longPresses generator
// mirrors taps: it yields a LongPress on the only clickable in-app node.
func TestLongPresses_TargetsClickableElement(t *testing.T) {
	verifier := newVerifier(t, WithAppPackage("com.folio"))
	loadActionSpec(t, verifier, `
		import { longPresses } from "@sanderling/spec";
		globalThis.actions = longPresses;
	`)
	pushTree(t, verifier, scopedTreeJSON)

	action, err := verifier.NextAction()
	if err != nil {
		t.Fatal(err)
	}
	if action.Kind != ActionKindLongPress {
		t.Fatalf("kind = %v, want LongPress", action.Kind)
	}
	if action.X != 50 || action.Y != 60 {
		t.Errorf("coords = (%d,%d), want (50,60) at SubmitButton", action.X, action.Y)
	}
}

// TestScrolls_TargetsScrollableContainer proves the scrolls generator anchors on
// a scrollable container and pre-computes swipe endpoints inside its bounds.
func TestScrolls_TargetsScrollableContainer(t *testing.T) {
	const treeJSON = `{
	  "attributes": {"resource-id": "root", "bounds": "[0,0,100,500]", "package": "com.folio"},
	  "children": [
	    {"attributes": {"testTag": "Feed", "bounds": "[0,0,100,400]", "scrollable": "true", "package": "com.folio"}, "enabled": true, "children": []},
	    {"attributes": {"testTag": "Header", "bounds": "[0,400,100,440]", "package": "com.folio"}, "clickable": true, "enabled": true, "children": []}
	  ]
	}`
	verifier := newVerifier(t, WithAppPackage("com.folio"))
	loadActionSpec(t, verifier, `
		import { scrolls } from "@sanderling/spec";
		globalThis.actions = scrolls;
	`)
	pushTree(t, verifier, treeJSON)

	for range 50 {
		action, err := verifier.NextAction()
		if err != nil {
			t.Fatal(err)
		}
		if action.Kind != ActionKindScroll {
			t.Fatalf("kind = %v, want Scroll", action.Kind)
		}
		// Feed center is (50,200); both endpoints must stay anchored there.
		if action.FromX != 50 || action.FromY != 200 {
			t.Fatalf("from = (%d,%d), want (50,200) at Feed center", action.FromX, action.FromY)
		}
		switch action.Direction {
		case "up", "down":
			if action.ToX != 50 {
				t.Fatalf("vertical scroll moved x: to = (%d,%d)", action.ToX, action.ToY)
			}
		case "left", "right":
			if action.ToY != 200 {
				t.Fatalf("horizontal scroll moved y: to = (%d,%d)", action.ToX, action.ToY)
			}
		default:
			t.Fatalf("unexpected direction %q", action.Direction)
		}
		if action.DurationMillis != 250 {
			t.Fatalf("durationMillis = %d, want 250", action.DurationMillis)
		}
	}
}

// TestScrolls_NoScrollableYieldsErrNoAction proves the generator declines when
// no scrollable container is present.
func TestScrolls_NoScrollableYieldsErrNoAction(t *testing.T) {
	verifier := newVerifier(t, WithAppPackage("com.folio"))
	loadActionSpec(t, verifier, `
		import { scrolls } from "@sanderling/spec";
		globalThis.actions = scrolls;
	`)
	pushTree(t, verifier, scopedTreeJSON)

	if _, err := verifier.NextAction(); !errors.Is(err, ErrNoAction) {
		t.Fatalf("err = %v, want ErrNoAction", err)
	}
}

// TestTaps_EmptyPackageNodeStaysInScope proves nodes that omit the package
// attribute (e.g. iOS, decor views) are kept, so the filter never empties a
// legitimate app screen.
func TestTaps_EmptyPackageNodeStaysInScope(t *testing.T) {
	const treeJSON = `{
	  "attributes": {"resource-id": "root", "bounds": "[0,0,100,500]"},
	  "children": [
	    {"attributes": {"testTag": "SubmitButton", "bounds": "[0,40,100,80]"}, "clickable": true, "enabled": true, "children": []}
	  ]
	}`
	verifier := newVerifier(t, WithAppPackage("com.folio"))
	loadActionSpec(t, verifier, `
		import { taps } from "@sanderling/spec";
		globalThis.actions = taps;
	`)
	pushTree(t, verifier, treeJSON)

	action, err := verifier.NextAction()
	if err != nil {
		t.Fatalf("err = %v, want the empty-package node kept in scope", err)
	}
	if action.X != 50 || action.Y != 60 {
		t.Errorf("coords = (%d,%d), want (50,60)", action.X, action.Y)
	}
}
