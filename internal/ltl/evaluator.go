package ltl

import "fmt"

type Verdict int

const (
	VerdictHolds Verdict = iota
	VerdictViolated
)

func (v Verdict) String() string {
	switch v {
	case VerdictHolds:
		return "holds"
	case VerdictViolated:
		return "violated"
	default:
		return fmt.Sprintf("verdict(%d)", int(v))
	}
}

// Evaluator folds a formula across observed steps. v0.1 semantics:
// Always(P) is satisfied if P held at every observed step; once P is false,
// the verdict latches to Violated.
type Evaluator struct {
	formula  Formula
	violated bool
}

func NewEvaluator(formula Formula) *Evaluator {
	return &Evaluator{formula: formula}
}

// Observe evaluates the formula against the current state and returns the
// running verdict. Once Violated, subsequent calls keep returning Violated
// regardless of what later observations look like.
func (e *Evaluator) Observe() Verdict {
	if e.violated {
		return VerdictViolated
	}
	if !holdsAtCurrentStep(e.formula) {
		e.violated = true
		return VerdictViolated
	}
	return VerdictHolds
}

func holdsAtCurrentStep(formula Formula) bool {
	switch concrete := formula.(type) {
	case AlwaysFormula:
		return holdsAtCurrentStep(concrete.Inner)
	case PureFormula:
		return concrete.Value
	case ThunkFormula:
		return concrete.Func()
	default:
		panic(fmt.Sprintf("ltl: unsupported formula type %T", formula))
	}
}
