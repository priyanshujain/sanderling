package ltl

import (
	"fmt"
	"strings"
	"time"
)

// Formula is the AST of a temporal logic property.
type Formula interface {
	isFormula()
	describe() string
}

type AlwaysFormula struct {
	Inner Formula
}

type PureFormula struct {
	Value bool
}

type ThunkFormula struct {
	Func func() bool
}

// NowFormula marks its inner formula for evaluation at the current step only.
// Primarily used so that now(...).implies(...) parses unambiguously.
type NowFormula struct {
	Inner Formula
}

// NextFormula obliges its inner formula to hold at the next step (not this one).
type NextFormula struct {
	Inner Formula
}

// EventuallyFormula obliges its inner formula to hold at some step within the
// given bound. An unbounded eventually never triggers a violation within a
// finite run.
//
// When Duration is non-zero and Deadline is the zero time, the evaluator
// resolves the absolute deadline on first reduction using the observation
// time. This matches the "within N seconds of obligation instantiation"
// semantics used by nested Always(Eventually(...).within(...)) formulas.
type EventuallyFormula struct {
	Inner        Formula
	StepBound    int
	HasStepBound bool
	Duration     time.Duration
	Deadline     time.Time
	HasDeadline  bool
}

type ImpliesFormula struct {
	Antecedent Formula
	Consequent Formula
}

type OrFormula struct {
	Left  Formula
	Right Formula
}

type AndFormula struct {
	Left  Formula
	Right Formula
}

type NotFormula struct {
	Inner Formula
}

func Always(inner Formula) Formula { return AlwaysFormula{Inner: inner} }

func Pure(value bool) Formula { return PureFormula{Value: value} }

func Thunk(function func() bool) Formula { return ThunkFormula{Func: function} }

func Now(inner Formula) Formula { return NowFormula{Inner: inner} }

func Next(inner Formula) Formula { return NextFormula{Inner: inner} }

func Eventually(inner Formula) Formula { return EventuallyFormula{Inner: inner} }

func EventuallyWithinSteps(inner Formula, steps int) Formula {
	return EventuallyFormula{Inner: inner, StepBound: steps, HasStepBound: true}
}

func EventuallyBefore(inner Formula, deadline time.Time) Formula {
	return EventuallyFormula{Inner: inner, Deadline: deadline, HasDeadline: true}
}

func EventuallyWithin(inner Formula, duration time.Duration) Formula {
	return EventuallyFormula{Inner: inner, Duration: duration}
}

func Implies(antecedent, consequent Formula) Formula {
	return ImpliesFormula{Antecedent: antecedent, Consequent: consequent}
}

func Or(left, right Formula) Formula { return OrFormula{Left: left, Right: right} }

func And(left, right Formula) Formula { return AndFormula{Left: left, Right: right} }

func Not(inner Formula) Formula { return NotFormula{Inner: inner} }

func (AlwaysFormula) isFormula()     {}
func (PureFormula) isFormula()       {}
func (ThunkFormula) isFormula()      {}
func (NowFormula) isFormula()        {}
func (NextFormula) isFormula()       {}
func (EventuallyFormula) isFormula() {}
func (ImpliesFormula) isFormula()    {}
func (OrFormula) isFormula()         {}
func (AndFormula) isFormula()        {}
func (NotFormula) isFormula()        {}

func (a AlwaysFormula) describe() string { return "Always(" + a.Inner.describe() + ")" }
func (p PureFormula) describe() string   { return fmt.Sprintf("Pure(%t)", p.Value) }
func (ThunkFormula) describe() string    { return "Thunk(...)" }
func (n NowFormula) describe() string    { return "Now(" + n.Inner.describe() + ")" }
func (n NextFormula) describe() string   { return "Next(" + n.Inner.describe() + ")" }
func (e EventuallyFormula) describe() string {
	parts := []string{e.Inner.describe()}
	if e.HasStepBound {
		parts = append(parts, fmt.Sprintf("steps=%d", e.StepBound))
	}
	if e.HasDeadline {
		parts = append(parts, "deadline="+e.Deadline.Format(time.RFC3339Nano))
	} else if e.Duration > 0 {
		parts = append(parts, "within="+e.Duration.String())
	}
	return "Eventually(" + strings.Join(parts, ", ") + ")"
}
func (i ImpliesFormula) describe() string {
	return "Implies(" + i.Antecedent.describe() + ", " + i.Consequent.describe() + ")"
}
func (o OrFormula) describe() string {
	return "Or(" + o.Left.describe() + ", " + o.Right.describe() + ")"
}
func (a AndFormula) describe() string {
	return "And(" + a.Left.describe() + ", " + a.Right.describe() + ")"
}
func (n NotFormula) describe() string { return "Not(" + n.Inner.describe() + ")" }

// Describe returns a debug-friendly representation of the formula.
func Describe(formula Formula) string { return formula.describe() }
