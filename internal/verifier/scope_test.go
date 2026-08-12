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

// TestTaps_ExcludeKeyboardRegionNoPackageKey proves a keyboard key that carries
// no package (the keyboard's "Settings" key is a bare node with a content-desc
// and no package) is still dropped: it is a child of the IME window, so it
// inherits the IME package as its owner and falls out of scope. A per-element
// package check would admit it. Only the in-app button remains a target.
func TestTaps_ExcludeKeyboardRegionNoPackageKey(t *testing.T) {
	const treeJSON = `{
	  "attributes": {"resource-id": "root", "bounds": "[0,0,1080,2400]", "package": "com.folio"},
	  "children": [
	    {"attributes": {"testTag": "SubmitButton", "bounds": "[100,400,500,500]", "package": "com.folio"}, "clickable": true, "enabled": true, "children": []},
	    {"attributes": {"resource-id": "com.google.android.inputmethod.latin:id/keyboard_holder", "bounds": "[0,1503,1080,2268]"}, "children": [
	      {"attributes": {"content-desc": "Settings", "bounds": "[461,1503,618,1635]"}, "clickable": true, "enabled": true, "children": []}
	    ]}
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
		t.Fatal(err)
	}
	if action.X != 300 || action.Y != 450 {
		t.Errorf("coords = (%d,%d), want (300,450) at SubmitButton; a keyboard key leaked into targets", action.X, action.Y)
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

// gestureTreeJSON gives each gesture verb a legal and an illegal anchor: an app
// list holding a plain row, against the soft keyboard's own scrollable strip
// holding one of its keys.
const gestureTreeJSON = `{
  "attributes": {"resource-id": "root", "bounds": "[0,0,100,500]", "package": "com.folio"},
  "children": [
    {"attributes": {"testTag": "AppList", "scrollable": "true", "bounds": "[0,0,100,300]", "package": "com.folio"}, "children": [
      {"attributes": {"testTag": "Row", "bounds": "[0,0,100,60]", "package": "com.folio"}, "children": []}
    ]},
    {"attributes": {"testTag": "EmojiStrip", "scrollable": "true", "bounds": "[0,400,100,440]", "package": "com.google.android.inputmethod.latin"}, "children": [
      {"attributes": {"testTag": "EmojiKey", "bounds": "[0,400,100,420]", "package": "com.google.android.inputmethod.latin"}, "children": []}
    ]}
  ]
}`

// TestGestures_ExcludeOffAppPackage proves both gesture verbs anchor on app
// nodes only, so exploration never drags the keyboard instead of the app, and
// pins what each verb accepts within the app and which way it drags. Scrolls
// anchor on the scrollable list alone and stay vertical, because every
// scrollable container earns a candidate and scrolling a list means up and down.
// Swipes anchor on any app element, the row and the root included, and drag in
// all four directions, which is what puts swipe-to-dismiss on a list row inside
// the action space.
func TestGestures_ExcludeOffAppPackage(t *testing.T) {
	tests := []struct {
		verb       string
		kind       ActionKind
		anchors    map[[2]int]bool
		directions map[string]bool
	}{
		{
			"scrolls",
			ActionKindScroll,
			map[[2]int]bool{{50, 150}: true},
			map[string]bool{"down": true, "up": true},
		},
		{
			"swipes",
			ActionKindSwipe,
			map[[2]int]bool{{50, 250}: true, {50, 150}: true, {50, 30}: true},
			map[string]bool{"down": true, "up": true, "left": true, "right": true},
		},
	}
	for _, test := range tests {
		t.Run(test.verb, func(t *testing.T) {
			verifier := newVerifier(t, WithAppPackage("com.folio"))
			loadActionSpec(t, verifier, `
				import { `+test.verb+` } from "@sanderling/spec";
				globalThis.actions = `+test.verb+`;
			`)
			pushTree(t, verifier, gestureTreeJSON)

			// Draw many times so neither the exclusion nor the coverage below is
			// satisfied by a lucky seed. The keyboard strip (50,420) and its key
			// (50,410) are the anchors that must never appear.
			seen := map[[2]int]bool{}
			seenDirections := map[string]bool{}
			for i := range 400 {
				action, err := verifier.NextAction()
				if err != nil {
					t.Fatal(err)
				}
				if action.Kind != test.kind {
					t.Fatalf("kind = %v, want %v", action.Kind, test.kind)
				}
				anchor := [2]int{action.FromX, action.FromY}
				if !test.anchors[anchor] {
					t.Fatalf("draw %d anchored at %v, outside %v", i, anchor, test.anchors)
				}
				direction := gestureDirection(action)
				if !test.directions[direction] {
					t.Fatalf("draw %d dragged %s, outside %v", i, direction, test.directions)
				}
				seen[anchor] = true
				seenDirections[direction] = true
			}
			if len(seen) != len(test.anchors) {
				t.Errorf("reached anchors %v, want all of %v", seen, test.anchors)
			}
			if len(seenDirections) != len(test.directions) {
				t.Errorf("reached directions %v, want all of %v", seenDirections, test.directions)
			}
		})
	}
}

// gestureDirection names which way a drawn gesture drags. A scroll carries the
// name it was enumerated under; a swipe carries only its endpoints, so the sign
// of the drag is what says where the finger went.
func gestureDirection(action Action) string {
	if action.Kind == ActionKindScroll {
		return action.Direction
	}
	switch {
	case action.ToX > action.FromX:
		return "right"
	case action.ToX < action.FromX:
		return "left"
	case action.ToY > action.FromY:
		return "down"
	default:
		return "up"
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
