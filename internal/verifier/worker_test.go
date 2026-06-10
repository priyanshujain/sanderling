package verifier

import (
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
