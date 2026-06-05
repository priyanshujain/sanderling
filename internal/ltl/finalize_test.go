package ltl

import (
	"testing"
	"testing/quick"
	"time"
)

func TestFinalize_UnboundedEventuallyUnmetIsViolated(t *testing.T) {
	evaluator := NewEvaluator(Eventually(ThunkNamed("p", func() (bool, error) { return false, nil })))
	for index := range 3 {
		if got := evaluator.ObserveAt(time.Unix(int64(index), 0)); got != VerdictPending {
			t.Fatalf("step %d: got %v, want pending", index, got)
		}
	}
	if got := evaluator.Finalize(); got != VerdictViolated {
		t.Errorf("Finalize = %v, want violated", got)
	}
}

func TestFinalize_FinalStepNextIsVacuouslyHolds(t *testing.T) {
	// A next obligation pending at run end has no successor state to check;
	// the run ending before the check is not a failure (weak next at the
	// trace boundary).
	evaluator := NewEvaluator(Next(ThunkNamed("p", func() (bool, error) { return true, nil })))
	if got := evaluator.Observe(); got != VerdictPending {
		t.Fatalf("step 1: got %v, want pending", got)
	}
	if got := evaluator.Finalize(); got != VerdictHolds {
		t.Errorf("Finalize = %v, want holds", got)
	}
	if witness := evaluator.Violation(); witness != nil {
		t.Errorf("Violation = %+v, want nil for a vacuous next", witness)
	}
}

func TestFinalize_AlwaysNextNeverReportsAtRunEnd(t *testing.T) {
	// always(next(p)): every step spawns a deferred check and the last one is
	// always pending when the run ends. That residue must not surface as an
	// end-of-run violation.
	evaluator := NewEvaluator(Always(Next(ThunkNamed("p", func() (bool, error) { return true, nil }))))
	for index := range 3 {
		evaluator.ObserveAt(time.Unix(int64(index), 0))
	}
	if got := evaluator.Finalize(); got != VerdictHolds {
		t.Errorf("Finalize = %v, want holds", got)
	}
}

func TestFinalize_HoldingRunStaysHolds(t *testing.T) {
	evaluator := NewEvaluator(Always(Pure(true)))
	evaluator.Observe()
	if got := evaluator.Finalize(); got != VerdictHolds {
		t.Errorf("Finalize = %v, want holds", got)
	}
}

func TestFinalize_AlreadyViolatedStaysViolated(t *testing.T) {
	evaluator := NewEvaluator(Always(Pure(false)))
	if got := evaluator.Observe(); got != VerdictViolated {
		t.Fatalf("expected violated, got %v", got)
	}
	if got := evaluator.Finalize(); got != VerdictViolated {
		t.Errorf("Finalize = %v, want violated", got)
	}
}

func TestFinalize_BoundedAlwaysVacuouslyHolds(t *testing.T) {
	// A bounded Always whose window never closed (still pending) is safe.
	evaluator := NewEvaluator(EventuallyWithinSteps(Pure(false), 5))
	evaluator.Observe()
	// The negated form of this is a bounded Always; build it directly.
	bounded := NewEvaluator(Always(Not(EventuallyWithinSteps(ThunkNamed("p", func() (bool, error) { return false, nil }), 5))))
	bounded.Observe()
	if got := bounded.Finalize(); got == VerdictViolated {
		t.Errorf("bounded always should not finalize to violated, got %v", got)
	}
}

// TestEventuallyWithin_ViolatesIffNConsecutiveFalse locks the bounded
// eventually contract: with a step bound of n and an inner that is false for
// the first n observations, the verdict violates exactly at step n, and with at
// least one true observation inside the window it holds.
func TestEventuallyWithin_ViolatesIffNConsecutiveFalse(t *testing.T) {
	law := func(boundSeed uint8, trueAtSeed uint8) bool {
		bound := int(boundSeed%5) + 1
		// trueAt < 0 means inner is never true.
		trueAt := int(trueAtSeed)%(bound+2) - 1
		step := 0
		inner := ThunkNamed("p", func() (bool, error) {
			current := trueAt >= 0 && step == trueAt
			return current, nil
		})
		evaluator := NewEvaluator(EventuallyWithinSteps(inner, bound))

		satisfiedInWindow := trueAt >= 0 && trueAt < bound
		var final Verdict = VerdictPending
		for index := range bound {
			step = index
			final = evaluator.ObserveAt(time.Unix(int64(index), 0))
			if final == VerdictHolds || final == VerdictViolated {
				break
			}
		}

		if satisfiedInWindow {
			return final == VerdictHolds
		}
		return final == VerdictViolated
	}
	if err := quick.Check(law, nil); err != nil {
		t.Error(err)
	}
}

// TestViolationLatchIsMonotonic locks: once an evaluator reports Violated, every
// subsequent observation (and Finalize) stays Violated regardless of inputs.
func TestViolationLatchIsMonotonic(t *testing.T) {
	law := func(seed uint64) bool {
		values := make([]bool, 8)
		for index := range values {
			values[index] = (seed>>uint(index))&1 == 1
		}
		step := 0
		evaluator := NewEvaluator(Always(ThunkNamed("p", func() (bool, error) {
			current := values[step%len(values)]
			step++
			return current, nil
		})))
		seenViolated := false
		for index := range 16 {
			got := evaluator.ObserveAt(time.Unix(int64(index), 0))
			if got == VerdictViolated {
				seenViolated = true
			} else if seenViolated {
				return false
			}
		}
		if seenViolated && evaluator.Finalize() != VerdictViolated {
			return false
		}
		return true
	}
	if err := quick.Check(law, nil); err != nil {
		t.Error(err)
	}
}

func TestCollapse_IdenticalObligationsMerge(t *testing.T) {
	merged := collapse([]obligation{
		{formula: Next(Pure(true)), origin: 1},
		{formula: Next(Pure(true)), origin: 2},
		{formula: Next(Pure(true)), origin: 3},
	})
	if len(merged) != 1 {
		t.Errorf("expected 1 obligation after collapse, got %d", len(merged))
	}
	if merged[0].origin != 1 {
		t.Errorf("collapse must keep the earliest origin, got %d", merged[0].origin)
	}
}

func TestCollapse_DistinctPredicatesDoNotMerge(t *testing.T) {
	merged := collapse([]obligation{
		{formula: Eventually(ThunkNamed("p3", func() (bool, error) { return false, nil }))},
		{formula: Eventually(ThunkNamed("p4", func() (bool, error) { return false, nil }))},
	})
	if len(merged) != 2 {
		t.Errorf("distinct predicates must not merge, got %d", len(merged))
	}
}

func TestCollapse_NamedThunkLeakBoundsPendingSet(t *testing.T) {
	// Always(Eventually(sameThunk)): each step spawns an identical obligation.
	// Without collapse the pending set grows unboundedly.
	evaluator := NewEvaluator(Always(Eventually(ThunkNamed("p", func() (bool, error) { return false, nil }))))
	for index := range 20 {
		evaluator.ObserveAt(time.Unix(int64(index), 0))
	}
	if len(evaluator.pending) > 2 {
		t.Errorf("pending set leaked to %d obligations", len(evaluator.pending))
	}
}
