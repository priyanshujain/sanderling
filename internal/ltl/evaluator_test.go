package ltl

import (
	"strings"
	"testing"
	"time"
)

func observe(formula Formula, count int) []Verdict {
	evaluator := NewEvaluator(formula)
	verdicts := make([]Verdict, 0, count)
	for range count {
		verdicts = append(verdicts, evaluator.Observe())
	}
	return verdicts
}

func TestPure_HoldsThenStays(t *testing.T) {
	got := observe(Always(Pure(true)), 3)
	for index, verdict := range got {
		if verdict != VerdictHolds {
			t.Errorf("step %d: got %v, want holds", index, verdict)
		}
	}
}

func TestPure_FalseImmediatelyViolates(t *testing.T) {
	got := observe(Always(Pure(false)), 3)
	for index, verdict := range got {
		if verdict != VerdictViolated {
			t.Errorf("step %d: got %v, want violated", index, verdict)
		}
	}
}

func TestThunk_TransitionFromHoldToViolate(t *testing.T) {
	values := []bool{true, true, false, true, true}
	step := 0
	evaluator := NewEvaluator(Always(Thunk(func() (bool, error) {
		current := values[step]
		step++
		return current, nil
	})))

	wantSequence := []Verdict{
		VerdictHolds,    // true
		VerdictHolds,    // true
		VerdictViolated, // false: latches
		VerdictViolated, // true after violation: still violated
		VerdictViolated, // true after violation: still violated
	}
	for index, want := range wantSequence {
		got := evaluator.Observe()
		if got != want {
			t.Errorf("step %d: got %v, want %v", index, got, want)
		}
	}
}

func TestEvaluator_StickinessAfterViolation(t *testing.T) {
	state := true
	evaluator := NewEvaluator(Always(Thunk(func() (bool, error) { return state, nil })))

	if got := evaluator.Observe(); got != VerdictHolds {
		t.Fatalf("step 1: got %v, want holds", got)
	}
	state = false
	if got := evaluator.Observe(); got != VerdictViolated {
		t.Fatalf("step 2: got %v, want violated", got)
	}
	state = true
	if got := evaluator.Observe(); got != VerdictViolated {
		t.Fatalf("step 3 (recovered state): violation should latch, got %v", got)
	}
}

func TestEvaluator_TopLevelPureCountedAtEachStep(t *testing.T) {
	got := observe(Pure(true), 2)
	if got[0] != VerdictHolds || got[1] != VerdictHolds {
		t.Errorf("bare Pure(true): %v", got)
	}
}

func TestEvaluator_TopLevelThunkRespectsObservation(t *testing.T) {
	state := true
	evaluator := NewEvaluator(Thunk(func() (bool, error) { return state, nil }))
	if got := evaluator.Observe(); got != VerdictHolds {
		t.Errorf("expected holds, got %v", got)
	}
	state = false
	if got := evaluator.Observe(); got != VerdictViolated {
		t.Errorf("expected violated, got %v", got)
	}
}

func TestDescribe(t *testing.T) {
	formula := Always(Pure(true))
	if got := Describe(formula); !strings.Contains(got, "Always") || !strings.Contains(got, "Pure(true)") {
		t.Errorf("Describe wrong: %q", got)
	}
	thunk := Always(Thunk(func() (bool, error) { return true, nil }))
	if got := Describe(thunk); !strings.Contains(got, "Thunk") {
		t.Errorf("Describe(thunk) wrong: %q", got)
	}
}

// TestEventuallyWithinSteps_NextInnerHitsBoundFirstStep pins the boundary: a
// 1-step Eventually whose inner is a Next defers the inner to step 2, but the
// window closes at step 1, so the obligation is unmet and violates. Bug class:
// off-by-one at the step bound treating the deferred inner as still in-window.
func TestEventuallyWithinSteps_NextInnerHitsBoundFirstStep(t *testing.T) {
	evaluator := NewEvaluator(EventuallyWithinSteps(Next(ThunkNamed("p", func() (bool, error) { return true, nil })), 1))
	if got := evaluator.ObserveAt(time.Unix(0, 0)); got != VerdictViolated {
		t.Errorf("EventuallyWithinSteps(Next(p), 1) step 1: got %v, want violated", got)
	}
}

// TestOr_ViolatedDisjunctDoesNotViolateWhileOtherPending guards the Or-reduce
// path where one disjunct fails (Pure(false)) while the other is still pending
// (Next(p)). The disjunction must stay pending on the failing step, never
// violate. Bug class: Or-reduction dropping the still-pending branch and
// latching violated on a single failed disjunct.
func TestOr_ViolatedDisjunctDoesNotViolateWhileOtherPending(t *testing.T) {
	evaluator := NewEvaluator(Always(Or(Pure(false), Next(ThunkNamed("p", func() (bool, error) { return true, nil })))))
	if got := evaluator.ObserveAt(time.Unix(0, 0)); got != VerdictPending {
		t.Errorf("step 1: got %v, want pending (Pure(false) disjunct must not violate)", got)
	}
	if got := evaluator.ObserveAt(time.Unix(1, 0)); got != VerdictPending {
		t.Errorf("step 2: got %v, want pending", got)
	}
}

// TestNot_OverPendingStaysPendingThenResolves pins Not over a pending inner: it
// must carry a Not-wrapped residual rather than collapse to a definite verdict
// at the step the inner is still deferred. Bug class: negation of a pending
// verdict resolving early to holds/violated.
func TestNot_OverPendingStaysPendingThenResolves(t *testing.T) {
	// Next(p) is deferred at step 1, so Not(Next(p)) is pending, not definite.
	// At step 2 the inner Next holds, so Not violates.
	evaluator := NewEvaluator(Always(Not(Next(ThunkNamed("p", func() (bool, error) { return true, nil })))))
	if got := evaluator.ObserveAt(time.Unix(0, 0)); got != VerdictPending {
		t.Errorf("step 1: got %v, want pending", got)
	}
	if got := evaluator.ObserveAt(time.Unix(1, 0)); got != VerdictViolated {
		t.Errorf("step 2: got %v, want violated (Not over a held inner)", got)
	}
}

func TestObserve_PanicsOnUnknownFormulaType(t *testing.T) {
	type unsupportedFormula struct{ Formula }
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Errorf("expected panic on unsupported formula type")
		}
	}()
	reduce(unsupportedFormula{}, time.Now(), 1)
}
