package ltl

import (
	"fmt"
	"time"
)

type Verdict int

const (
	VerdictHolds Verdict = iota
	VerdictViolated
	VerdictPending
)

func (v Verdict) String() string {
	switch v {
	case VerdictHolds:
		return "holds"
	case VerdictViolated:
		return "violated"
	case VerdictPending:
		return "pending"
	default:
		return fmt.Sprintf("verdict(%d)", int(v))
	}
}

// Evaluator reduces a formula across observed steps using residual-formula
// semantics. Each step either resolves pending obligations (to holds or
// violated) or carries them forward as residuals. Once a single obligation
// violates, the overall verdict latches to Violated.
type Evaluator struct {
	root     Formula
	pending  []Formula
	violated bool
}

func NewEvaluator(formula Formula) *Evaluator {
	return &Evaluator{root: formula}
}

// Observe evaluates the formula against the current state and returns the
// running verdict. Uses the real wall clock for deadline-bound operators;
// callers that need reproducible time should use ObserveAt.
func (e *Evaluator) Observe() Verdict {
	return e.ObserveAt(time.Now())
}

// ObserveAt is like Observe but takes the current step time explicitly.
func (e *Evaluator) ObserveAt(now time.Time) Verdict {
	if e.violated {
		return VerdictViolated
	}

	fresh := rootObligation(e.root)
	obligations := append(e.pending, fresh)
	e.pending = e.pending[:0]

	for _, obligation := range obligations {
		result := reduce(obligation, now)
		switch result.status {
		case statusHolds:
			// drop
		case statusViolated:
			e.violated = true
			e.pending = nil
			return VerdictViolated
		case statusPending:
			e.pending = append(e.pending, result.formula)
		}
	}

	if len(e.pending) > 0 {
		return VerdictPending
	}
	return VerdictHolds
}

// Residual returns a single Formula describing what the evaluator still has
// to prove after the most recent ObserveAt. PureFormula{true} means the
// property holds for the run so far; PureFormula{false} means it has latched
// to violated. When obligations are still pending, they are folded together
// with AndFormula in the order they were registered so the JSON AST reflects
// the same order the evaluator processes them in.
func (e *Evaluator) Residual() Formula {
	if e.violated {
		return PureFormula{Value: false}
	}
	if len(e.pending) == 0 {
		return PureFormula{Value: true}
	}
	combined := e.pending[0]
	for _, formula := range e.pending[1:] {
		combined = AndFormula{Left: combined, Right: formula}
	}
	return combined
}

// rootObligation returns the formula to instantiate at each step. An outer
// Always is stripped so its inner is re-evaluated every step; any other root
// formula is itself re-instantiated each step (matching the v0.1 semantics
// where a bare Thunk is re-observed on every call).
func rootObligation(root Formula) Formula {
	if always, ok := root.(AlwaysFormula); ok {
		return always.Inner
	}
	return root
}

type residualStatus int

const (
	statusHolds residualStatus = iota
	statusViolated
	statusPending
)

type reduceResult struct {
	status  residualStatus
	formula Formula
}

func holds() reduceResult    { return reduceResult{status: statusHolds} }
func violated() reduceResult { return reduceResult{status: statusViolated} }
func pending(f Formula) reduceResult {
	return reduceResult{status: statusPending, formula: f}
}

func reduce(formula Formula, now time.Time) reduceResult {
	switch concrete := formula.(type) {
	case PureFormula:
		if concrete.Value {
			return holds()
		}
		return violated()

	case ThunkFormula:
		if concrete.Func() {
			return holds()
		}
		return violated()

	case NowFormula:
		return reduce(concrete.Inner, now)

	case NextFormula:
		// Next defers the inner obligation to the following step without
		// evaluating it now.
		return pending(concrete.Inner)

	case EventuallyFormula:
		// First-reduction deadline resolution: if the formula was built with
		// a relative duration, fix the absolute deadline to (now + duration)
		// so subsequent reductions compare against a stable value.
		if !concrete.HasDeadline && concrete.Duration > 0 {
			concrete.Deadline = now.Add(concrete.Duration)
			concrete.HasDeadline = true
		}
		innerResult := reduce(concrete.Inner, now)
		if innerResult.status == statusHolds {
			return holds()
		}
		if concrete.HasStepBound && concrete.StepBound <= 1 {
			return violated()
		}
		if concrete.HasDeadline && !now.Before(concrete.Deadline) {
			return violated()
		}
		next := concrete
		if concrete.HasStepBound {
			next.StepBound = concrete.StepBound - 1
		}
		return pending(next)

	case ImpliesFormula:
		antecedent := reduce(concrete.Antecedent, now)
		switch antecedent.status {
		case statusHolds:
			return reduce(concrete.Consequent, now)
		case statusViolated:
			return holds()
		case statusPending:
			return pending(ImpliesFormula{
				Antecedent: antecedent.formula,
				Consequent: concrete.Consequent,
			})
		}

	case OrFormula:
		left := reduce(concrete.Left, now)
		right := reduce(concrete.Right, now)
		if left.status == statusHolds || right.status == statusHolds {
			return holds()
		}
		if left.status == statusViolated && right.status == statusViolated {
			return violated()
		}
		if left.status == statusViolated {
			return pending(right.formula)
		}
		if right.status == statusViolated {
			return pending(left.formula)
		}
		return pending(OrFormula{Left: left.formula, Right: right.formula})

	case AndFormula:
		left := reduce(concrete.Left, now)
		right := reduce(concrete.Right, now)
		if left.status == statusViolated || right.status == statusViolated {
			return violated()
		}
		if left.status == statusHolds && right.status == statusHolds {
			return holds()
		}
		if left.status == statusHolds {
			return pending(right.formula)
		}
		if right.status == statusHolds {
			return pending(left.formula)
		}
		return pending(AndFormula{Left: left.formula, Right: right.formula})

	case NotFormula:
		inner := reduce(concrete.Inner, now)
		switch inner.status {
		case statusHolds:
			return violated()
		case statusViolated:
			return holds()
		case statusPending:
			return pending(NotFormula{Inner: inner.formula})
		}

	case AlwaysFormula:
		innerResult := reduce(concrete.Inner, now)
		if innerResult.status == statusViolated {
			return violated()
		}
		next := AlwaysFormula{Inner: concrete.Inner}
		if innerResult.status == statusHolds {
			return pending(next)
		}
		return pending(AndFormula{Left: innerResult.formula, Right: next})
	}

	panic(fmt.Sprintf("ltl: unsupported formula type %T", formula))
}
