package ltl

import (
	"testing"
	"time"
)

// TestRoot_BoundedEventuallyStaysOneObligation locks the cost of the one-shot
// root. A duration-bounded top-level eventually is one reachability goal, so
// the pending set holds one obligation for the whole run. Re-instantiating it
// every step monitored G F<=n(p) instead and left one live obligation per step
// behind: a 553-step run carried 553 of them and re-ran the predicate once per
// obligation per step.
func TestRoot_BoundedEventuallyStaysOneObligation(t *testing.T) {
	const steps = 600
	calls := 0
	formula := EventuallyWithin(ThunkNamed("p", func() (bool, error) {
		calls++
		return false, nil
	}), 300*time.Second)

	evaluator := NewEvaluator(formula)
	base := time.Unix(0, 0)
	for index := range steps {
		if got := evaluator.ObserveAtStep(base.Add(time.Duration(index)*110*time.Millisecond), index+1); got != VerdictPending {
			t.Fatalf("step %d: got %v, want pending", index+1, got)
		}
		if len(evaluator.pending) != 1 {
			t.Fatalf("step %d: %d pending obligations, want 1", index+1, len(evaluator.pending))
		}
	}
	if calls != steps {
		t.Errorf("predicate ran %d times over %d steps, want one call per step", calls, steps)
	}
}

// TestRoot_TopLevelEventuallyIsSatisfiedOnce pins the semantics behind that
// bound: a top-level eventually is discharged for good the first time it is
// satisfied. Under the old implicit-always reading it was re-armed at every
// step, so a property that had already been reached could still violate later.
func TestRoot_TopLevelEventuallyIsSatisfiedOnce(t *testing.T) {
	reached := false
	evaluator := NewEvaluator(EventuallyWithinSteps(ThunkNamed("p", func() (bool, error) {
		return reached, nil
	}), 2))

	if got := evaluator.ObserveAtStep(time.Unix(0, 0), 1); got != VerdictPending {
		t.Fatalf("step 1: got %v, want pending", got)
	}
	reached = true
	if got := evaluator.ObserveAtStep(time.Unix(1, 0), 2); got != VerdictHolds {
		t.Fatalf("step 2: got %v, want holds", got)
	}
	reached = false
	for index := 3; index <= 6; index++ {
		if got := evaluator.ObserveAtStep(time.Unix(int64(index), 0), index); got != VerdictHolds {
			t.Fatalf("step %d: got %v, want holds (the goal was already reached)", index, got)
		}
	}
	if got := evaluator.Finalize(); got != VerdictHolds {
		t.Errorf("Finalize = %v, want holds", got)
	}
}

// TestRoot_BoundedAlwaysKeepsItsBound: a bounded root Always is a single
// window, not a recurrence. Stripping it and re-instantiating its inner every
// step dropped the bound, so G<=1(p) behaved as G(p) and a false p after the
// window closed still violated.
func TestRoot_BoundedAlwaysKeepsItsBound(t *testing.T) {
	values := []bool{true, true, false}
	step := 0
	formula := AlwaysFormula{
		Inner:        ThunkNamed("p", func() (bool, error) { return values[step], nil }),
		StepBound:    1,
		HasStepBound: true,
	}
	evaluator := NewEvaluator(formula)
	for index := range values {
		step = index
		if got := evaluator.ObserveAtStep(time.Unix(int64(index), 0), index+1); got == VerdictViolated {
			t.Fatalf("step %d: violated outside the 1-step window", index+1)
		}
	}
}

// TestRoot_UnboundedAlwaysStillReInstantiates: the one-shot rule must not touch
// the recurrence root every spec property is built on. Each step gets its own
// instance of the inner, which is what gives a deferred failure the origin step
// that armed it.
func TestRoot_UnboundedAlwaysStillReInstantiates(t *testing.T) {
	values := []bool{true, true, false}
	step := 0
	evaluator := NewEvaluator(Always(ThunkNamed("p", func() (bool, error) {
		return values[step], nil
	})))
	for index := range 2 {
		step = index
		if got := evaluator.ObserveAtStep(time.Unix(int64(index), 0), index+1); got != VerdictHolds {
			t.Fatalf("step %d: got %v, want holds", index+1, got)
		}
	}
	step = 2
	if got := evaluator.ObserveAtStep(time.Unix(2, 0), 3); got != VerdictViolated {
		t.Errorf("step 3: got %v, want violated", got)
	}
}
