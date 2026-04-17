package ltl

import "fmt"

// Formula is the AST of a temporal logic property. v0.1 supports only Always
// over Pure/Thunk leaves; eventually, next, and bounded operators are
// deferred to v0.2+.
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

func Always(inner Formula) Formula { return AlwaysFormula{Inner: inner} }

func Pure(value bool) Formula { return PureFormula{Value: value} }

func Thunk(function func() bool) Formula { return ThunkFormula{Func: function} }

func (AlwaysFormula) isFormula() {}
func (PureFormula) isFormula()   {}
func (ThunkFormula) isFormula()  {}

func (a AlwaysFormula) describe() string { return "Always(" + a.Inner.describe() + ")" }
func (p PureFormula) describe() string   { return fmt.Sprintf("Pure(%t)", p.Value) }
func (ThunkFormula) describe() string    { return "Thunk(...)" }

// Describe returns a debug-friendly representation of the formula.
func Describe(formula Formula) string { return formula.describe() }
