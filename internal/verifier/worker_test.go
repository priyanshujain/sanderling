package verifier

import (
	"math"
	"testing"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// TestVerbAcceptsSwipeRequiresPositiveBounds locks the fix for the notification
// shade: a zero-bounds element centers at (0,0), and a downward swipe from the
// top-left corner is the system gesture that pulls the shade over the app. The
// swipe verb must reject zero-bounds nodes like every other verb does.
func TestVerbAcceptsSwipeRequiresPositiveBounds(t *testing.T) {
	zeroBounds := &hierarchy.Element{Bounds: hierarchy.Bounds{}}
	if verbAccepts("swipes", zeroBounds) {
		t.Error("swipes must reject a zero-bounds element (it centers at (0,0) and pulls the notification shade)")
	}

	realBounds := &hierarchy.Element{Bounds: hierarchy.Bounds{Left: 100, Top: 400, Right: 980, Bottom: 600}}
	if !verbAccepts("swipes", realBounds) {
		t.Error("swipes must accept an element with positive bounds")
	}
}

// TestKeyboardRegionTop covers the region detection that keeps the fuzzer off
// keyboard keys, especially the guard that ignores the IME's full-screen decor
// view (which otherwise reports a huge bounds and would push the keyboard line
// to the top of the screen, excluding the whole app).
func TestKeyboardRegionTop(t *testing.T) {
	ime := func(top, bottom int) *hierarchy.Element {
		return &hierarchy.Element{
			ResourceID: "com.google.android.inputmethod.latin:id/keyboard_holder",
			Bounds:     hierarchy.Bounds{Right: 1080, Top: top, Bottom: bottom},
		}
	}
	app := func(top, bottom int) *hierarchy.Element {
		return &hierarchy.Element{ResourceID: "app/field", Bounds: hierarchy.Bounds{Right: 1080, Top: top, Bottom: bottom}}
	}

	t.Run("no keyboard yields sentinel", func(t *testing.T) {
		tree := &hierarchy.Tree{Elements: []*hierarchy.Element{app(0, 2400)}}
		if got := keyboardRegionTop(tree); got != math.MaxInt {
			t.Errorf("keyboardRegionTop = %d, want MaxInt with no keyboard", got)
		}
	})

	t.Run("decor view ignored, real keyboard sets the line", func(t *testing.T) {
		tree := &hierarchy.Tree{Elements: []*hierarchy.Element{
			app(0, 2400),
			ime(0, 2400),    // full-screen IME decor view: must be ignored
			ime(1503, 2268), // the actual keyboard
		}}
		if got := keyboardRegionTop(tree); got != 1503 {
			t.Errorf("keyboardRegionTop = %d, want 1503 (keyboard top, not the decor view's 0)", got)
		}
	})

	t.Run("decor-only view yields sentinel", func(t *testing.T) {
		tree := &hierarchy.Tree{Elements: []*hierarchy.Element{app(0, 2400), ime(0, 2400)}}
		if got := keyboardRegionTop(tree); got != math.MaxInt {
			t.Errorf("keyboardRegionTop = %d, want MaxInt when only a full-screen IME decor view is present", got)
		}
	})
}
