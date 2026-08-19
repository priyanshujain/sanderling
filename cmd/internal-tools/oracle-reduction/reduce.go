package main

import (
	"fmt"
	"time"

	"github.com/priyanshujain/sanderling/internal/ltl"
)

// tripleWindow is the horizon a property triple has: an obligation armed at one
// observation must discharge at the next, and nothing outlives that.
const tripleWindow = 2

// singleStateFormula is what a checker holding one observation can refute.
// Every operator that defers or repeats an obligation is replaced by the
// constant that makes the formula around it trivially satisfied, so the only
// refutation left is a predicate read at a single observation. The outermost
// always survives because it is what says "check this at each observation",
// which costs no history.
func singleStateFormula(formula ltl.Formula) ltl.Formula {
	if always, ok := formula.(ltl.AlwaysFormula); ok {
		always.Inner = stateless(always.Inner, true)
		return always
	}
	return stateless(formula, true)
}

// singleStepFormula is the property triple: one step of history, and no
// obligation surviving past the next observation. A next keeps its one-step
// deferral, and an eventually of any window shrinks to the two observations a
// triple spans.
func singleStepFormula(formula ltl.Formula) ltl.Formula {
	if always, ok := formula.(ltl.AlwaysFormula); ok {
		always.Inner = oneStep(always.Inner, true)
		return always
	}
	return oneStep(formula, true)
}

// singleStateExpresses and singleStepExpresses decide, from the property's form
// alone, whether the reduced oracle can still state the property after its
// rewrite. An oracle that cannot state a property does not get to refute it,
// and is reported as silent on it rather than as either verdict.
//
// Two shapes defeat a reduction. The rewrite shortens an obligation window the
// oracle's horizon cannot hold, leaving it to refute a property strictly
// stronger than the one the author wrote. Or the rewrite leaves a formula whose
// verdict no longer depends on the trace, leaving it to report the same answer
// everywhere. Either way the column would carry a verdict about a property
// nobody wrote, which is worth less than an admission that the oracle is out of
// its depth.
func singleStateExpresses(formula ltl.Formula) bool {
	return dependsOnTrace(singleStateFormula(formula))
}

func singleStepExpresses(formula ltl.Formula) bool {
	return !truncatesWindow(formula) &&
		dependsOnTrace(singleStepFormula(formula))
}

// truncatesWindow reports whether the single-step rewrite had to shorten a
// window to fit a triple. Where it did, the reduced oracle is checking a
// stronger property than the author wrote, so a refutation of it is not the
// same event as a refutation of the property.
func truncatesWindow(formula ltl.Formula) bool {
	if always, ok := formula.(ltl.AlwaysFormula); ok {
		return shortensWindow(always.Inner, true)
	}
	return shortensWindow(formula, true)
}

// shortensWindow walks the same shape oneStep rewrites, and reports only the
// shortening that strengthens the formula. Polarity is what separates the two:
// a shorter window under an even number of negations demands the same thing
// sooner, so a refutation of it need not be a refutation of the property, while
// under an odd number it asks for less and its refutations stay sound. The
// operators oneStep erases rather than shortens are erased to the constant its
// position is satisfied by, which also only asks for less.
func shortensWindow(formula ltl.Formula, positive bool) bool {
	switch concrete := formula.(type) {
	case ltl.EventuallyFormula:
		return positive && windowOutlastsTriple(concrete)
	case ltl.NowFormula:
		return shortensWindow(concrete.Inner, positive)
	case ltl.NotFormula:
		return shortensWindow(concrete.Inner, !positive)
	case ltl.AndFormula:
		return shortensWindow(concrete.Left, positive) || shortensWindow(concrete.Right, positive)
	case ltl.OrFormula:
		return shortensWindow(concrete.Left, positive) || shortensWindow(concrete.Right, positive)
	case ltl.ImpliesFormula:
		return shortensWindow(concrete.Antecedent, !positive) || shortensWindow(concrete.Consequent, positive)
	default:
		return false
	}
}

// windowOutlastsTriple asks whether an obligation can still be open after the
// two observations a triple spans. A window counted in time is outside what a
// triple can state whatever its length, because a triple has no clock: it can
// say "at the next observation" and nothing about when that arrives.
func windowOutlastsTriple(formula ltl.EventuallyFormula) bool {
	if formula.HasStepBound {
		return formula.StepBound > tripleWindow
	}
	return true
}

// dependsOnTrace reports whether a rewritten formula can still read the trace.
// Constants fold through the connectives, and one that folds away to a constant
// answers the same on every trace: silence that reads as "did not refute", or a
// refutation of everything. Neither is a verdict about the run.
func dependsOnTrace(formula ltl.Formula) bool {
	_, constant := fold(formula).(ltl.PureFormula)
	return !constant
}

// fold propagates the constants the rewrites substituted for erased operators.
// An obligation whose inner formula folded to false keeps its shape, because a
// deferred false is a refutation still owed; only the side that can no longer
// fail folds away.
func fold(formula ltl.Formula) ltl.Formula {
	switch concrete := formula.(type) {
	case ltl.AlwaysFormula:
		concrete.Inner = fold(concrete.Inner)
		if pure, ok := concrete.Inner.(ltl.PureFormula); ok {
			return pure
		}
		return concrete
	case ltl.EventuallyFormula:
		concrete.Inner = fold(concrete.Inner)
		if isPure(concrete.Inner, true) {
			return ltl.Pure(true)
		}
		return concrete
	case ltl.NextFormula:
		inner := fold(concrete.Inner)
		if isPure(inner, true) {
			return ltl.Pure(true)
		}
		return ltl.Next(inner)
	case ltl.NowFormula:
		inner := fold(concrete.Inner)
		if _, ok := inner.(ltl.PureFormula); ok {
			return inner
		}
		return ltl.Now(inner)
	case ltl.NotFormula:
		inner := fold(concrete.Inner)
		if pure, ok := inner.(ltl.PureFormula); ok {
			return ltl.Pure(!pure.Value)
		}
		return ltl.Not(inner)
	case ltl.AndFormula:
		left, right := fold(concrete.Left), fold(concrete.Right)
		switch {
		case isPure(left, false) || isPure(right, false):
			return ltl.Pure(false)
		case isPure(left, true):
			return right
		case isPure(right, true):
			return left
		}
		return ltl.And(left, right)
	case ltl.OrFormula:
		left, right := fold(concrete.Left), fold(concrete.Right)
		switch {
		case isPure(left, true) || isPure(right, true):
			return ltl.Pure(true)
		case isPure(left, false):
			return right
		case isPure(right, false):
			return left
		}
		return ltl.Or(left, right)
	case ltl.ImpliesFormula:
		antecedent, consequent := fold(concrete.Antecedent), fold(concrete.Consequent)
		switch {
		case isPure(antecedent, false) || isPure(consequent, true):
			return ltl.Pure(true)
		case isPure(antecedent, true):
			return consequent
		}
		return ltl.Implies(antecedent, consequent)
	default:
		return formula
	}
}

func isPure(formula ltl.Formula, value bool) bool {
	pure, ok := formula.(ltl.PureFormula)
	return ok && pure.Value == value
}

// stateless erases every temporal operator. The replacement constant follows
// the position's polarity: under an even number of negations a temporal
// sub-formula is dropped as satisfied, and under an odd number as failed, so
// that in both cases its negation cannot refute anything either.
func stateless(formula ltl.Formula, positive bool) ltl.Formula {
	switch concrete := formula.(type) {
	case ltl.AlwaysFormula, ltl.NextFormula, ltl.EventuallyFormula:
		return ltl.Pure(positive)
	case ltl.NowFormula:
		return ltl.Now(stateless(concrete.Inner, positive))
	case ltl.NotFormula:
		return ltl.Not(stateless(concrete.Inner, !positive))
	case ltl.AndFormula:
		return ltl.And(stateless(concrete.Left, positive), stateless(concrete.Right, positive))
	case ltl.OrFormula:
		return ltl.Or(stateless(concrete.Left, positive), stateless(concrete.Right, positive))
	case ltl.ImpliesFormula:
		return ltl.Implies(
			stateless(concrete.Antecedent, !positive),
			stateless(concrete.Consequent, positive),
		)
	default:
		return formula
	}
}

func oneStep(formula ltl.Formula, positive bool) ltl.Formula {
	switch concrete := formula.(type) {
	case ltl.AlwaysFormula:
		return ltl.Pure(positive)
	case ltl.NextFormula:
		return ltl.Next(stateless(concrete.Inner, positive))
	case ltl.EventuallyFormula:
		return ltl.EventuallyWithinSteps(stateless(concrete.Inner, positive), tripleWindow)
	case ltl.NowFormula:
		return ltl.Now(oneStep(concrete.Inner, positive))
	case ltl.NotFormula:
		return ltl.Not(oneStep(concrete.Inner, !positive))
	case ltl.AndFormula:
		return ltl.And(oneStep(concrete.Left, positive), oneStep(concrete.Right, positive))
	case ltl.OrFormula:
		return ltl.Or(oneStep(concrete.Left, positive), oneStep(concrete.Right, positive))
	case ltl.ImpliesFormula:
		return ltl.Implies(
			oneStep(concrete.Antecedent, !positive),
			oneStep(concrete.Consequent, positive),
		)
	default:
		return formula
	}
}

// propertyClass splits safety from liveness by the property's top-level form:
// a reachability goal is liveness, everything else is a safety obligation
// re-asserted at each observation.
func propertyClass(formula ltl.Formula) string {
	if _, ok := formula.(ltl.EventuallyFormula); ok {
		return "liveness"
	}
	return "safety"
}

func topLevelForm(formula ltl.Formula) string {
	switch concrete := formula.(type) {
	case ltl.AlwaysFormula:
		return "always" + boundSuffix(concrete.HasStepBound, concrete.StepBound, concrete.Duration)
	case ltl.EventuallyFormula:
		return "eventually" + boundSuffix(concrete.HasStepBound, concrete.StepBound, concrete.Duration)
	default:
		return "predicate"
	}
}

func boundSuffix(
	hasStepBound bool,
	stepBound int,
	duration time.Duration,
) string {
	switch {
	case hasStepBound:
		return fmt.Sprintf(" within %d steps", stepBound)
	case duration > 0:
		return fmt.Sprintf(" within %s", duration)
	default:
		return ""
	}
}
