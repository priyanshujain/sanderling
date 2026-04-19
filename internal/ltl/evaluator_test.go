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
	evaluator := NewEvaluator(Always(Thunk(func() bool {
		current := values[step]
		step++
		return current
	})))

	wantSequence := []Verdict{
		VerdictHolds,    // true
		VerdictHolds,    // true
		VerdictViolated, // false — latches
		VerdictViolated, // true after violation — still violated
		VerdictViolated, // true after violation — still violated
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
	evaluator := NewEvaluator(Always(Thunk(func() bool { return state })))

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
	evaluator := NewEvaluator(Thunk(func() bool { return state }))
	if got := evaluator.Observe(); got != VerdictHolds {
		t.Errorf("expected holds, got %v", got)
	}
	state = false
	if got := evaluator.Observe(); got != VerdictViolated {
		t.Errorf("expected violated, got %v", got)
	}
}

func TestVerdict_String(t *testing.T) {
	if VerdictHolds.String() != "holds" {
		t.Errorf("VerdictHolds.String() = %q", VerdictHolds.String())
	}
	if VerdictViolated.String() != "violated" {
		t.Errorf("VerdictViolated.String() = %q", VerdictViolated.String())
	}
}

func TestDescribe(t *testing.T) {
	formula := Always(Pure(true))
	if got := Describe(formula); !strings.Contains(got, "Always") || !strings.Contains(got, "Pure(true)") {
		t.Errorf("Describe wrong: %q", got)
	}
	thunk := Always(Thunk(func() bool { return true }))
	if got := Describe(thunk); !strings.Contains(got, "Thunk") {
		t.Errorf("Describe(thunk) wrong: %q", got)
	}
}

func TestObserve_PanicsOnUnknownFormulaType(t *testing.T) {
	type unsupportedFormula struct{ Formula }
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Errorf("expected panic on unsupported formula type")
		}
	}()
	reduce(unsupportedFormula{}, time.Now())
}
