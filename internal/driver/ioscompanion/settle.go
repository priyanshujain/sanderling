// Package ioscompanion drives an iOS simulator through the native simulator
// companion. This file ports the screen-settle (stability polling) logic that
// waits for the on-device UI to stop churning before the runner reads a
// hierarchy and screenshot pair.
package ioscompanion

import (
	"context"
	"strings"
	"time"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// StabilityPollInterval is how long the loop waits between hierarchy probes.
// The companion answers describe-all in tens of milliseconds, so a tight
// interval samples transitions promptly without backing the stream up.
const StabilityPollInterval = 100 * time.Millisecond

// MinStableStreak is how long the tree must stay structurally identical (and
// non-transitional) before the poll declares settle. Actions whose effect is
// async (a tap that fires a write which later pops the back stack) leave the
// UI momentarily stable before the navigation transition fires; requiring an
// uninterrupted streak means churn that starts during the window resets the
// clock instead of being missed. The streak is shorter than the JVM sidecar's
// (which polls a slower, flakier adb hierarchy): the companion's describe is
// fast and deterministic, so three consecutive identical samples are a solid
// stability signal, and cross-fade transitions are caught separately by the
// route-screen transitional check rather than by streak length.
const MinStableStreak = 300 * time.Millisecond

// StabilityPollCap bounds total time spent polling so a UI that never settles
// does not block the runner indefinitely. A genuinely still-churning screen
// hands a transitional snapshot to the next step, which settles it, so the cap
// trades a slightly stale read for bounded latency rather than dropping work.
const StabilityPollCap = 1500 * time.Millisecond

// Clock abstracts time so the poll loop can be driven by a fake in tests
// without sleeping for real.
type Clock interface {
	Now() time.Time
	Sleep(duration time.Duration)
}

// systemClock is the production Clock backed by the standard library.
type systemClock struct{}

func (systemClock) Now() time.Time        { return time.Now() }
func (systemClock) Sleep(d time.Duration) { time.Sleep(d) }

// SystemClock returns a Clock backed by the standard library wall clock.
func SystemClock() Clock { return systemClock{} }

// PollUntilStable returns once StabilitySnapshot has been non-transitional and
// equal to itself for an uninterrupted stretch of at least MinStableStreak,
// capped at StabilityPollCap. fetch returns the current hierarchy tree (nil on
// fetch failure, treated like a transitional snapshot so the streak resets).
//
// The loop mirrors the companion's JVM implementation: it samples a prior
// snapshot, then on each tick sleeps the poll interval, fetches a fresh
// snapshot, and grows the streak only while consecutive snapshots are both
// non-transitional and structurally identical. A transitional snapshot, a
// changed snapshot, or a fetch failure resets the streak. The function returns
// when the streak reaches MinStableStreak or the cap elapses, and respects
// context cancellation.
func PollUntilStable(ctx context.Context, clock Clock, fetch func() *hierarchy.Tree) {
	deadline := clock.Now().Add(StabilityPollCap)
	prior := snapshot(fetch)
	var streakStart time.Time
	for clock.Now().Before(deadline) {
		if ctx.Err() != nil {
			return
		}
		clock.Sleep(StabilityPollInterval)
		current := snapshot(fetch)
		now := clock.Now()
		if prior.valid && current.valid && prior.hash == current.hash {
			if streakStart.IsZero() {
				streakStart = now
			}
			if now.Sub(streakStart) >= MinStableStreak {
				return
			}
		} else {
			streakStart = time.Time{}
		}
		prior = current
	}
}

// stableSnapshot is the result of probing a single hierarchy tree. valid is
// false when the snapshot is transitional (mid route transition) or the fetch
// failed, both of which must reset the stable streak.
type stableSnapshot struct {
	hash  string
	valid bool
}

func snapshot(fetch func() *hierarchy.Tree) stableSnapshot {
	tree := fetch()
	if tree == nil {
		return stableSnapshot{}
	}
	return StabilitySnapshot(tree)
}

// StabilitySnapshot probes a hierarchy tree for both structural shape and route
// transition state. An empty tree is a valid stable snapshot (the blank-tree
// case in the companion). A tree with more than one route Screen is
// transitional and reported invalid: a navigation host keeps both source and
// destination destinations alive during a cross-fade, and a snapshot taken in
// that window is unreliable because lazy lists in the incoming screen mount
// over several frames. Otherwise the snapshot carries the structural hash.
func StabilitySnapshot(tree *hierarchy.Tree) stableSnapshot {
	if tree == nil || tree.Root == nil {
		return stableSnapshot{valid: true}
	}
	if CountRouteScreens(tree) > 1 {
		return stableSnapshot{}
	}
	return stableSnapshot{hash: StructuralHash(tree), valid: true}
}

// routeTagKeys are the attribute keys whose value, when it ends with "Screen",
// marks a node as a route-level destination. Mirrors the companion's set.
var routeTagKeys = []string{
	"resource-id", "resourceId", "testTag",
	"identifier", "accessibilityIdentifier",
}

// CountRouteScreens counts nodes that carry a route-level destination tag (a
// route-tag attribute whose value ends with "Screen"). At most one tag is
// counted per node, matching the companion which breaks on the first match.
func CountRouteScreens(tree *hierarchy.Tree) int {
	if tree == nil || tree.Root == nil {
		return 0
	}
	return countRouteScreens(tree.Root)
}

func countRouteScreens(node *hierarchy.Node) int {
	count := 0
	for _, key := range routeTagKeys {
		value, ok := node.Attributes[key]
		if !ok {
			continue
		}
		if strings.HasSuffix(value, "Screen") {
			count++
			break
		}
	}
	for _, child := range node.Children {
		count += countRouteScreens(child)
	}
	return count
}

// stableAttributeKeys are the identity attributes folded into the structural
// hash, in this exact order. The transient bounds attribute is deliberately
// excluded so a measure pass that shifts pixels without changing what is on
// screen does not extend the wait; structure (via tree shape) and text are
// included so a real content or layout-tree change does reset stability.
var stableAttributeKeys = []string{
	"resource-id", "resourceId",
	"class", "className",
	"content-desc", "contentDescription", "accessibilityText",
	"text",
	"testTag", "identifier", "accessibilityIdentifier",
}

// StructuralHash walks the tree and concatenates only the stable identity
// attributes (excluding bounds), in tree order, wrapping each node in
// parentheses so tree shape is encoded. The result is compared by equality, so
// it is the hash itself rather than a digest of it.
func StructuralHash(tree *hierarchy.Tree) string {
	if tree == nil || tree.Root == nil {
		return ""
	}
	var builder strings.Builder
	walkForStructuralHash(tree.Root, &builder)
	return builder.String()
}

func walkForStructuralHash(node *hierarchy.Node, out *strings.Builder) {
	out.WriteByte('(')
	for _, key := range stableAttributeKeys {
		value, ok := node.Attributes[key]
		if !ok {
			continue
		}
		out.WriteString(key)
		out.WriteByte(':')
		out.WriteString(value)
		out.WriteByte('|')
	}
	for _, child := range node.Children {
		walkForStructuralHash(child, out)
	}
	out.WriteByte(')')
}
