package ltl

import (
	"testing"
	"time"
)

// thunkSeq returns a predicate that yields the given boolean per observation,
// repeating the last value once the sequence is exhausted.
func thunkSeq(values ...bool) func() (bool, error) {
	step := 0
	return func() (bool, error) {
		value := values[len(values)-1]
		if step < len(values) {
			value = values[step]
		}
		step++
		return value, nil
	}
}

func runAndFinalize(formula Formula, steps int) (Verdict, *Evaluator) {
	evaluator := NewEvaluator(formula)
	var last Verdict
	for index := range steps {
		last = evaluator.ObserveAt(time.Unix(int64(index), 0))
		if last == VerdictViolated {
			return last, evaluator
		}
	}
	return evaluator.Finalize(), evaluator
}

// Implies with a temporal antecedent must still evaluate the consequent at the
// current step. Always(p) holds over the observed run, so a false consequent
// Not(q) at the last step makes the implication violated. The pre-fix engine
// deferred the whole implication and reported Holds.
func TestImplies_TemporalAntecedent_ConsequentFalseViolates(t *testing.T) {
	p := Thunk(thunkSeq(true, true, true))
	q := Thunk(thunkSeq(false, false, true))
	formula := Implies(Always(p), Not(q))
	if verdict, _ := runAndFinalize(formula, 3); verdict != VerdictViolated {
		t.Fatalf("Implies(Always(p),Not(q)) p=TTT q=FFT: got %v, want violated", verdict)
	}
}

// Single-step witness of the same class: antecedent holds, consequent false.
func TestImplies_TemporalAntecedent_SingleStepViolates(t *testing.T) {
	formula := Implies(Always(Thunk(thunkSeq(true))), Not(Thunk(thunkSeq(true))))
	if verdict, _ := runAndFinalize(formula, 1); verdict != VerdictViolated {
		t.Fatalf("Implies(Always(true),Not(true)): got %v, want violated", verdict)
	}
}

// A consequent inside an Or must not mask the violation either.
func TestImplies_TemporalAntecedent_OrConsequentViolates(t *testing.T) {
	formula := Implies(Always(Thunk(thunkSeq(true))), Or(Not(Thunk(thunkSeq(true))), Pure(false)))
	if verdict, _ := runAndFinalize(formula, 1); verdict != VerdictViolated {
		t.Fatalf("Implies(Always(true),Or(Not(true),false)): got %v, want violated", verdict)
	}
}

// Control: a false antecedent makes the implication vacuously hold.
func TestImplies_TemporalAntecedent_FalseAntecedentHolds(t *testing.T) {
	p := Thunk(thunkSeq(true, true, false))
	q := Thunk(thunkSeq(false, false, true))
	formula := Implies(Always(p), Not(q))
	if verdict, _ := runAndFinalize(formula, 3); verdict != VerdictHolds {
		t.Fatalf("Implies(Always(p),Not(q)) p=TTF q=FFT: got %v, want holds", verdict)
	}
}

// A bounded Always whose inner is definitely false inside the window violates.
// The pre-fix window-close branch returned holds() unconditionally and lost
// this; the breach check runs before the window check and must stay there.
func TestBoundedAlways_ViolatedInnerInsideWindow(t *testing.T) {
	inner := AlwaysFormula{Inner: Thunk(thunkSeq(false)), StepBound: 1, HasStepBound: true}
	formula := Always(inner)
	if verdict, _ := runAndFinalize(formula, 3); verdict != VerdictViolated {
		t.Fatalf("Always(boundedAlways(false,1)): got %v, want violated", verdict)
	}
}

// A bounded Always whose inner is still a deferred Next obligation when the
// window closes discharges vacuously: nothing was breached inside the window.
// This is the exact dual of the bounded Eventually violating when its inner has
// not held by the time the window closes
// (TestEventuallyWithinSteps_NextInnerHitsBoundFirstStep), and the pair is what
// makes nnf's G/F dualisation semantics preserving. It costs the deferred check
// the window closed on: G<=n and F<=n both range over the observations at which
// their inner can definitely resolve, never past them.
func TestBoundedAlways_PendingInnerDischargesAtWindowClose(t *testing.T) {
	inner := AlwaysFormula{Inner: Next(Thunk(thunkSeq(false))), StepBound: 1, HasStepBound: true}
	if verdict, _ := runAndFinalize(Always(inner), 3); verdict != VerdictHolds {
		t.Fatalf("Always(boundedAlways(Next(false),1)): got %v, want holds", verdict)
	}
	if verdict, _ := runAndFinalize(Always(nnf(Not(inner))), 3); verdict != VerdictViolated {
		t.Fatalf("its negation: got %v, want violated", verdict)
	}
}

// A bounded Always whose inner genuinely holds each step stays satisfied: the
// fix must not turn a satisfied bounded window into a false positive.
func TestBoundedAlways_HoldingInnerStillHolds(t *testing.T) {
	inner := AlwaysFormula{Inner: Not(Thunk(thunkSeq(false))), StepBound: 1, HasStepBound: true}
	formula := Always(inner)
	if verdict, _ := runAndFinalize(formula, 3); verdict != VerdictHolds {
		t.Fatalf("Always(boundedAlways(Not(false),1)): got %v, want holds", verdict)
	}
}
