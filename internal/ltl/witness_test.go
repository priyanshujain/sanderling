package ltl

import (
	"errors"
	"testing"
	"time"
)

func TestViolation_PredicateFalseCarriesReasonAndStep(t *testing.T) {
	values := []bool{true, false}
	step := 0
	evaluator := NewEvaluator(Always(ThunkNamed("p", func() (bool, error) {
		current := values[step]
		step++
		return current, nil
	})))
	if got := evaluator.ObserveAt(time.Unix(0, 0)); got != VerdictHolds {
		t.Fatalf("step 1: got %v, want holds", got)
	}
	if got := evaluator.ObserveAt(time.Unix(1, 0)); got != VerdictViolated {
		t.Fatalf("step 2: got %v, want violated", got)
	}
	witness := evaluator.Violation()
	if witness == nil {
		t.Fatal("Violation = nil, want non-nil")
	}
	if witness.Reason != "predicate false" {
		t.Errorf("Reason = %q, want %q", witness.Reason, "predicate false")
	}
	if witness.Step != 2 {
		t.Errorf("Step = %d, want 2", witness.Step)
	}
	if witness.IsError {
		t.Errorf("IsError = true, want false for a plain false")
	}
}

func TestViolation_ThrownPredicateSetsIsError(t *testing.T) {
	evaluator := NewEvaluator(Always(ThunkNamed("p", func() (bool, error) {
		return false, errors.New("boom")
	})))
	if got := evaluator.Observe(); got != VerdictViolated {
		t.Fatalf("got %v, want violated", got)
	}
	witness := evaluator.Violation()
	if witness == nil {
		t.Fatal("Violation = nil, want non-nil")
	}
	if !witness.IsError {
		t.Errorf("IsError = false, want true for a thrown predicate")
	}
	if witness.Reason != "boom" {
		t.Errorf("Reason = %q, want %q", witness.Reason, "boom")
	}
}

func TestViolation_FinalizeFillsWitness(t *testing.T) {
	evaluator := NewEvaluator(Eventually(ThunkNamed("p", func() (bool, error) {
		return false, nil
	})))
	evaluator.ObserveAt(time.Unix(0, 0))
	if got := evaluator.Finalize(); got != VerdictViolated {
		t.Fatalf("Finalize = %v, want violated", got)
	}
	witness := evaluator.Violation()
	if witness == nil {
		t.Fatal("Violation = nil after Finalize, want non-nil")
	}
	if witness.Reason != "eventually never satisfied" {
		t.Errorf("Reason = %q, want %q", witness.Reason, "eventually never satisfied")
	}
}

func TestViolation_NilBeforeViolation(t *testing.T) {
	evaluator := NewEvaluator(Always(Pure(true)))
	evaluator.Observe()
	if got := evaluator.Violation(); got != nil {
		t.Errorf("Violation = %+v, want nil for a holding run", got)
	}
}
