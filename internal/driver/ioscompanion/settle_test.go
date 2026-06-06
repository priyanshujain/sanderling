package ioscompanion

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// fakeClock advances its internal time only when Sleep is called, so the poll
// loop runs instantly and deterministically without real sleeping.
type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(d time.Duration) { c.now = c.now.Add(d) }

func mustTree(t *testing.T, json string) *hierarchy.Tree {
	t.Helper()
	tree, err := hierarchy.Parse(json)
	if err != nil {
		t.Fatalf("parse hierarchy: %v", err)
	}
	return tree
}

// fetcher returns a fetch function yielding trees in sequence; once the
// sequence is exhausted the last tree repeats forever.
func fetcher(trees ...*hierarchy.Tree) func() *hierarchy.Tree {
	index := 0
	return func() *hierarchy.Tree {
		tree := trees[index]
		if index < len(trees)-1 {
			index++
		}
		return tree
	}
}

const singleScreenJSON = `{
  "attributes": {"resource-id": "homeScreen", "text": "Home"},
  "children": [
    {"attributes": {"text": "Welcome", "bounds": "[0,0,100,50]"}, "children": []}
  ]
}`

// twoScreenJSON carries two route Screens (a navigation cross-fade), which must
// be treated as transitional.
const twoScreenJSON = `{
  "attributes": {"resource-id": "rootView"},
  "children": [
    {"attributes": {"testTag": "homeScreen"}, "children": []},
    {"attributes": {"testTag": "detailScreen"}, "children": []}
  ]
}`

func TestCountRouteScreens(t *testing.T) {
	tests := []struct {
		name string
		json string
		want int
	}{
		{"single screen", singleScreenJSON, 1},
		{"two screens", twoScreenJSON, 2},
		{"no screens", `{"attributes": {"text": "plain"}, "children": []}`, 0},
		{
			"one node only counts once across route keys",
			`{"attributes": {"resource-id": "fooScreen", "testTag": "barScreen"}, "children": []}`,
			1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CountRouteScreens(mustTree(t, test.json))
			if got != test.want {
				t.Fatalf("CountRouteScreens = %d, want %d", got, test.want)
			}
		})
	}
}

func TestStabilitySnapshotTreatsTwoScreensAsTransitional(t *testing.T) {
	snap := StabilitySnapshot(mustTree(t, twoScreenJSON))
	if snap.valid {
		t.Fatalf("snapshot with two route Screens should be invalid (transitional)")
	}
}

func TestStructuralHashIgnoresBoundsButNotTextOrStructure(t *testing.T) {
	base := `{"attributes": {"text": "Hi", "bounds": "[0,0,10,10]"}, "children": [
    {"attributes": {"text": "child", "bounds": "[0,0,5,5]"}, "children": []}
  ]}`
	boundsShifted := `{"attributes": {"text": "Hi", "bounds": "[5,5,20,20]"}, "children": [
    {"attributes": {"text": "child", "bounds": "[5,5,9,9]"}, "children": []}
  ]}`
	textChanged := `{"attributes": {"text": "Bye", "bounds": "[0,0,10,10]"}, "children": [
    {"attributes": {"text": "child", "bounds": "[0,0,5,5]"}, "children": []}
  ]}`
	structureChanged := `{"attributes": {"text": "Hi", "bounds": "[0,0,10,10]"}, "children": [
    {"attributes": {"text": "child", "bounds": "[0,0,5,5]"}, "children": []},
    {"attributes": {"text": "extra"}, "children": []}
  ]}`

	baseHash := StructuralHash(mustTree(t, base))
	if got := StructuralHash(mustTree(t, boundsShifted)); got != baseHash {
		t.Fatalf("bounds-only change must not alter hash:\n base=%q\n shift=%q", baseHash, got)
	}
	if got := StructuralHash(mustTree(t, textChanged)); got == baseHash {
		t.Fatalf("text change must alter hash, both were %q", baseHash)
	}
	if got := StructuralHash(mustTree(t, structureChanged)); got == baseHash {
		t.Fatalf("structure change must alter hash, both were %q", baseHash)
	}
}

func TestPollUntilStableTwoScreensNeverStable(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	fetch := fetcher(mustTree(t, twoScreenJSON))
	start := clock.Now()
	PollUntilStable(context.Background(), clock, fetch)
	elapsed := clock.Now().Sub(start)
	if elapsed < StabilityPollCap {
		t.Fatalf("a perpetually transitional UI must poll until the cap, elapsed=%v cap=%v", elapsed, StabilityPollCap)
	}
}

func TestPollUntilStableReturnsAfterStreak(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	fetch := fetcher(mustTree(t, singleScreenJSON))
	start := clock.Now()
	PollUntilStable(context.Background(), clock, fetch)
	elapsed := clock.Now().Sub(start)
	// prior is sampled at t0, then equal snapshots accumulate the streak. The
	// streak starts at the first matching tick and must reach MinStableStreak.
	if elapsed > StabilityPollCap {
		t.Fatalf("stable UI must settle before the cap, elapsed=%v", elapsed)
	}
	if elapsed < MinStableStreak {
		t.Fatalf("must observe a full stable streak before returning, elapsed=%v streak=%v", elapsed, MinStableStreak)
	}
}

func TestPollUntilStableResetsStreakOnMidStreakChange(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	stable := mustTree(t, singleScreenJSON)
	churned := mustTree(t, `{"attributes": {"resource-id": "homeScreen", "text": "Loading"}, "children": []}`)
	// Two stable ticks build part of a streak (250ms, 500ms accumulated since
	// the streak start at the first match) then a changed tree resets it, after
	// which a fresh full 750ms streak is required.
	fetch := fetcher(stable, stable, stable, churned, stable)
	start := clock.Now()
	PollUntilStable(context.Background(), clock, fetch)
	elapsed := clock.Now().Sub(start)
	// The streak that succeeds begins after the churn, so total elapsed must
	// exceed a single uninterrupted streak. Without a reset the loop would have
	// returned far earlier.
	if elapsed < MinStableStreak+3*StabilityPollInterval {
		t.Fatalf("mid-streak change must reset and require a fresh streak, elapsed=%v", elapsed)
	}
	if elapsed > StabilityPollCap {
		t.Fatalf("expected settle before cap once a clean streak completes, elapsed=%v", elapsed)
	}
}

func TestPollUntilStableCapReturnsWhenNeverStable(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	// Each fetch yields a structurally different tree, so the streak never
	// grows and only the cap can end the loop.
	tick := 0
	fetch := func() *hierarchy.Tree {
		tick++
		return mustTree(t, fmt.Sprintf(`{"attributes": {"text": "frame-%d"}, "children": []}`, tick))
	}
	start := clock.Now()
	PollUntilStable(context.Background(), clock, fetch)
	elapsed := clock.Now().Sub(start)
	if elapsed < StabilityPollCap {
		t.Fatalf("never-stable UI must poll up to the cap, elapsed=%v cap=%v", elapsed, StabilityPollCap)
	}
	if elapsed > StabilityPollCap+StabilityPollInterval {
		t.Fatalf("must not overshoot the cap by more than one interval, elapsed=%v", elapsed)
	}
}

func TestPollUntilStableHonorsContextCancellation(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fetch := fetcher(mustTree(t, twoScreenJSON))
	start := clock.Now()
	PollUntilStable(ctx, clock, fetch)
	if elapsed := clock.Now().Sub(start); elapsed > StabilityPollInterval {
		t.Fatalf("cancelled context must stop the loop promptly, elapsed=%v", elapsed)
	}
}
