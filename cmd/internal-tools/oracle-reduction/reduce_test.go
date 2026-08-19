package main

import (
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/ltl"
)

// observe drives an evaluator over a fixed sequence of predicate readings and
// returns the observation at which it latched, or 0 if it never did.
func observe(formula ltl.Formula, steps int) int {
	evaluator := ltl.NewEvaluator(formula)
	base := time.Unix(0, 0)
	for step := 1; step <= steps; step++ {
		if evaluator.ObserveAtStep(
			base.Add(time.Duration(step)*time.Second),
			step,
		) == ltl.VerdictViolated {
			return step
		}
	}
	if evaluator.Finalize() == ltl.VerdictViolated {
		return steps + 1
	}
	return 0
}

func constant(value bool) ltl.Formula {
	return ltl.ThunkNamed("p", func() (bool, error) { return value, nil })
}

func TestSingleStateHoldsWhereRefutationNeedsTheNextObservation(t *testing.T) {
	property := ltl.Always(
		ltl.Implies(ltl.Now(constant(true)), ltl.Next(constant(false))),
	)

	if engine := observe(property, 4); engine == 0 {
		t.Fatal(
			"the engine was expected to refute a next obligation that never held",
		)
	}
	if reduced := observe(singleStateFormula(property), 4); reduced != 0 {
		t.Errorf(
			"single-state refuted at observation %d, and one observation cannot see a next",
			reduced,
		)
	}
	if reduced := observe(singleStepFormula(property), 4); reduced == 0 {
		t.Error("single-step was expected to refute a one-step obligation")
	}
}

func TestSingleStateRefutesAPredicateReadAtOneObservation(t *testing.T) {
	property := ltl.Always(constant(false))

	if reduced := observe(singleStateFormula(property), 3); reduced != 1 {
		t.Errorf(
			"single-state latched at %d, want the first observation",
			reduced,
		)
	}
}

func TestSingleStateKeepsANegatedTemporalHarmless(t *testing.T) {
	property := ltl.Always(ltl.Not(ltl.Eventually(constant(false))))

	if reduced := observe(singleStateFormula(property), 3); reduced != 0 {
		t.Errorf(
			"single-state refuted at observation %d; erasing a negated eventually must not manufacture a violation",
			reduced,
		)
	}
}

func TestSingleStepShrinksAReachabilityGoalToTwoObservations(t *testing.T) {
	property := ltl.EventuallyWithinSteps(constant(false), 500)

	if engine := observe(property, 4); engine != 5 {
		t.Fatalf(
			"a 500-step window closes only at run end, latched at %d",
			engine,
		)
	}
	if reduced := observe(singleStepFormula(property), 4); reduced != 2 {
		t.Errorf(
			"single-step latched at %d, want the second observation",
			reduced,
		)
	}
	if !truncatesWindow(property) {
		t.Error(
			"a 500-step window shortened to two observations must be reported as truncated",
		)
	}
}

// sequence returns a predicate that reads the given values, one per call, so
// each evaluator gets its own reading counter.
func sequence(readings []bool) ltl.Formula {
	position := 0
	return ltl.ThunkNamed("p", func() (bool, error) {
		value := readings[position]
		position++
		return value, nil
	})
}

func TestSingleStepConvictsAGoalTheEngineSeesReached(t *testing.T) {
	readings := []bool{false, false, true, true}

	if engine := observe(ltl.EventuallyWithinSteps(sequence(readings), 4), 4); engine != 0 {
		t.Fatalf(
			"the engine latched at %d, and the goal is reached inside its window",
			engine,
		)
	}
	if reduced := observe(singleStepFormula(ltl.EventuallyWithinSteps(sequence(readings), 4)), 4); reduced != 2 {
		t.Errorf(
			"single-step latched at %d; an obligation armed at the first observation must not reach the third",
			reduced,
		)
	}
}

func TestTruncatesWindowIgnoresAnObligationThatAlreadyFits(t *testing.T) {
	property := ltl.Always(
		ltl.Implies(ltl.Now(constant(true)), ltl.Next(constant(true))),
	)

	if truncatesWindow(property) {
		t.Error("a next spans two observations already and is not truncated")
	}
}

func TestPropertyClassAndFormComeFromTheTopLevel(t *testing.T) {
	safety := ltl.Always(constant(true))
	liveness := ltl.EventuallyWithinSteps(constant(true), 575)

	if got := propertyClass(safety); got != "safety" {
		t.Errorf("class of an always: got %q", got)
	}
	if got := propertyClass(liveness); got != "liveness" {
		t.Errorf("class of an eventually: got %q", got)
	}
	if got := topLevelForm(liveness); got != "eventually within 575 steps" {
		t.Errorf("form: got %q", got)
	}
	if got := topLevelForm(ltl.EventuallyWithin(constant(true), 3*time.Second)); got != "eventually within 3s" {
		t.Errorf("form: got %q", got)
	}
}

func TestSingleStepCannotExpressAWindowLongerThanATriple(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		property  ltl.Formula
		expresses bool
	}{
		{"reachability goal in steps", ltl.EventuallyWithinSteps(constant(true), 575), false},
		{"reachability goal in time", ltl.EventuallyWithin(constant(true), 3*time.Second), false},
		{"unbounded reachability goal", ltl.Eventually(constant(true)), false},
		{"window a triple spans exactly", ltl.EventuallyWithinSteps(constant(true), tripleWindow), true},
		{"deadline nested under an always", ltl.Always(ltl.Implies(
			ltl.Now(constant(true)), ltl.EventuallyWithin(constant(true), 3*time.Second))), false},
		{"one-step obligation under an always", ltl.Always(ltl.Implies(
			ltl.Now(constant(true)), ltl.Next(constant(true)))), true},
		{"predicate under an always", ltl.Always(constant(true)), true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if singleStepExpresses(testCase.property) != testCase.expresses {
				t.Errorf("single-step expresses %s: got %t, want %t",
					testCase.name, !testCase.expresses, testCase.expresses)
			}
		})
	}
}

// A shorter window under a negation asks for less rather than more, so the
// triple's refutations of it stay sound and the property stays expressible.
// Reporting it as inexpressible would push a defect the single-step oracle can
// genuinely catch into the temporal-only column.
func TestSingleStepStillExpressesANegatedWindow(t *testing.T) {
	property := ltl.Always(
		ltl.Not(ltl.EventuallyWithinSteps(constant(false), 575)),
	)

	if !singleStepExpresses(property) {
		t.Error(
			"a window shortened under a negation is weakened, not strengthened",
		)
	}
	if truncatesWindow(property) {
		t.Error(
			"the truncation marker must not fire where shortening only weakens",
		)
	}
}

func TestSingleStateCannotExpressWhatItsRewriteEmpties(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		property  ltl.Formula
		expresses bool
	}{
		{"predicate at one observation", ltl.Always(constant(true)), true},
		{"reachability goal", ltl.EventuallyWithinSteps(constant(true), 575), false},
		{"next obligation under an always", ltl.Always(ltl.Implies(
			ltl.Now(constant(true)), ltl.Next(constant(true)))), false},
		{"deadline under an always", ltl.Always(ltl.Implies(
			ltl.Now(constant(true)), ltl.EventuallyWithin(constant(true), 3*time.Second))), false},
		{"negated eventually under an always", ltl.Always(ltl.Not(ltl.Eventually(constant(false)))), false},
		{"predicate conjoined with a next", ltl.Always(ltl.And(constant(true), ltl.Next(constant(true)))), true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if singleStateExpresses(testCase.property) != testCase.expresses {
				t.Errorf("single-state expresses %s: got %t, want %t",
					testCase.name, !testCase.expresses, testCase.expresses)
			}
		})
	}
}
