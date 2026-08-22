package runner

import (
	"context"
	"encoding/json"
	"testing"

	mockdriver "github.com/priyanshujain/sanderling/internal/driver/mock"
	"github.com/priyanshujain/sanderling/internal/hierarchy"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

// legacyAuthoredScrollWire is what @sanderling/spec 0.0.3 put on the wire for
// `Scroll({ in: container, direction: "down" })`: the container's own point as
// both endpoints. This binary reads pre-computed endpoints as authoritative,
// so the pair dispatches a 250ms press and hold that the app reads as a tap,
// with `direction` riding along unread.
const legacyAuthoredScrollWire = `{"kind":"Scroll","direction":"down",` +
	`"fromX":540,"fromY":1200,"toX":540,"toY":1200,"durationMillis":250,` +
	`"selector":"id:ledger"}`

func TestApplyAction_ZeroDistanceScrollNeverReachesTheDriver(t *testing.T) {
	action, err := verifier.DecodeAction(json.RawMessage(legacyAuthoredScrollWire))
	if err != nil {
		t.Fatalf("DecodeAction: %v", err)
	}
	treeJSON := `{"attributes":{"bounds":"[0,0,1080,2400]"},"children":[
		{"attributes":{"resource-id":"com.fixture:id/ledger","scrollable":"true","bounds":"[0,400,1080,2000]"},"children":[],"enabled":true}
	]}`
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatalf("parse tree: %v", err)
	}
	driverMock := mockdriver.New()

	skipped, err := applyAction(context.Background(), driverMock, action, tree)
	if err != nil {
		t.Fatalf("applyAction: %v", err)
	}
	for _, dispatched := range driverMock.Actions() {
		if dispatched.Kind == mockdriver.ActionSwipe {
			t.Fatalf("a scroll travelling zero distance reached the driver as %v; "+
				"it is a press and hold, not a scroll", dispatched)
		}
	}
	if skipped != actionSkippedZeroDistanceScroll {
		t.Errorf("applyAction reported %q, want %q: a step that scrolled nothing "+
			"has to say so", skipped, actionSkippedZeroDistanceScroll)
	}
}

func TestApplyAction_ScrollWithRealDistanceStillReachesTheDriver(t *testing.T) {
	driverMock := mockdriver.New()
	action := verifier.Action{
		Kind:           verifier.ActionKindScroll,
		Direction:      "down",
		FromX:          540,
		FromY:          1200,
		ToX:            540,
		ToY:            600,
		DurationMillis: 250,
	}

	mustDispatch(t, driverMock, action, nil)
	found := false
	for _, dispatched := range driverMock.Actions() {
		if dispatched.Kind == mockdriver.ActionSwipe && dispatched.ToY == 600 {
			found = true
		}
	}
	if !found {
		t.Errorf("the scroll never reached the driver: %v", driverMock.Actions())
	}
}

func TestApplyAction_ZeroDistanceSwipeNeverReachesTheDriver(t *testing.T) {
	driverMock := mockdriver.New()
	action := verifier.Action{
		Kind:           verifier.ActionKindSwipe,
		FromX:          540,
		FromY:          1200,
		ToX:            540,
		ToY:            1200,
		DurationMillis: 250,
	}

	skipped, err := applyAction(context.Background(), driverMock, action, nil)
	if err != nil {
		t.Fatalf("applyAction: %v", err)
	}
	if len(driverMock.Actions()) != 0 {
		t.Fatalf("a swipe travelling zero distance reached the driver as %v; "+
			"it is a press and hold, not a swipe", driverMock.Actions())
	}
	if skipped != actionSkippedZeroDistanceSwipe {
		t.Errorf("applyAction reported %q, want %q", skipped, actionSkippedZeroDistanceSwipe)
	}
}

// clampGestureToSafeArea can collapse a swipe that was authored with real
// distance, so the guard has to read the endpoints it is about to dispatch
// rather than the ones the action carried.
func TestApplyAction_SwipeCollapsedByTheSafeAreaClampNeverReachesTheDriver(t *testing.T) {
	tree, err := hierarchy.Parse(`{"attributes":{"bounds":"[0,0,1080,2400]"},"children":[]}`)
	if err != nil {
		t.Fatalf("parse tree: %v", err)
	}
	driverMock := mockdriver.New()
	action := verifier.Action{
		Kind:           verifier.ActionKindSwipe,
		FromX:          540,
		FromY:          2500,
		ToX:            540,
		ToY:            2600,
		DurationMillis: 250,
	}

	skipped, err := applyAction(context.Background(), driverMock, action, tree)
	if err != nil {
		t.Fatalf("applyAction: %v", err)
	}
	if len(driverMock.Actions()) != 0 {
		t.Fatalf("a swipe the clamp collapsed onto one point reached the driver as %v",
			driverMock.Actions())
	}
	if skipped != actionSkippedZeroDistanceSwipe {
		t.Errorf("applyAction reported %q, want %q", skipped, actionSkippedZeroDistanceSwipe)
	}
}

func TestApplyAction_SwipeWithRealDistanceStillReachesTheDriver(t *testing.T) {
	driverMock := mockdriver.New()
	action := verifier.Action{
		Kind:           verifier.ActionKindSwipe,
		FromX:          540,
		FromY:          1800,
		ToX:            540,
		ToY:            600,
		DurationMillis: 250,
	}

	mustDispatch(t, driverMock, action, nil)
	found := false
	for _, dispatched := range driverMock.Actions() {
		if dispatched.Kind == mockdriver.ActionSwipe && dispatched.ToY == 600 {
			found = true
		}
	}
	if !found {
		t.Errorf("the swipe never reached the driver: %v", driverMock.Actions())
	}
}
