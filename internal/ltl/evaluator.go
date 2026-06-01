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
	steps    int
}

func NewEvaluator(formula Formula) *Evaluator {
	return &Evaluator{root: nnf(formula)}
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
	e.steps++

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

	e.pending = collapse(e.pending)

	if len(e.pending) > 0 {
		return VerdictPending
	}
	return VerdictHolds
}

// collapse removes structurally-identical obligations, keeping the first
// occurrence in order. Distinct predicates never merge because ThunkFormula's
// name participates in its describe() key, so deduping cannot hide a violation.
func collapse(obligations []Formula) []Formula {
	if len(obligations) < 2 {
		return obligations
	}
	seen := make(map[string]struct{}, len(obligations))
	result := obligations[:0]
	for _, obligation := range obligations {
		key := obligation.describe()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, obligation)
	}
	return result
}

// Finalize reports the terminal verdict for the run. Pending obligations that
// can never be discharged by a future step (an unbounded eventually that never
// fired, a strong next with no successor) resolve to Violated; safety
// obligations that were never breached resolve to Holds.
func (e *Evaluator) Finalize() Verdict {
	if e.violated {
		return VerdictViolated
	}
	for _, obligation := range e.pending {
		if finalize(obligation) == statusViolated {
			e.violated = true
			e.pending = nil
			return VerdictViolated
		}
	}
	return VerdictHolds
}

// finalize collapses a pending obligation to its terminal status assuming no
// further steps will occur.
func finalize(formula Formula) residualStatus {
	switch concrete := formula.(type) {
	case PureFormula:
		if concrete.Value {
			return statusHolds
		}
		return statusViolated
	case ThunkFormula:
		return statusViolated
	case EventuallyFormula:
		return statusViolated
	case NextFormula:
		return statusViolated
	case AlwaysFormula:
		return statusHolds
	case NowFormula:
		return finalize(concrete.Inner)
	case NotFormula:
		switch finalize(concrete.Inner) {
		case statusViolated:
			return statusHolds
		default:
			return statusViolated
		}
	case AndFormula:
		if finalize(concrete.Left) == statusViolated || finalize(concrete.Right) == statusViolated {
			return statusViolated
		}
		return statusHolds
	case OrFormula:
		if finalize(concrete.Left) == statusHolds || finalize(concrete.Right) == statusHolds {
			return statusHolds
		}
		return statusViolated
	case ImpliesFormula:
		if finalize(concrete.Antecedent) == statusViolated {
			return statusHolds
		}
		return finalize(concrete.Consequent)
	default:
		return statusHolds
	}
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
		// First-reduction deadline resolution mirrors EventuallyFormula so a
		// relative duration becomes a stable absolute deadline.
		if !concrete.HasDeadline && concrete.Duration > 0 {
			concrete.Deadline = now.Add(concrete.Duration)
			concrete.HasDeadline = true
		}
		innerResult := reduce(concrete.Inner, now)
		if innerResult.status == statusViolated {
			return violated()
		}
		// A bounded Always is the dual of a bounded Eventually: once the window
		// closes without a breach it is vacuously satisfied. At the last step in
		// the window a pending inner cannot be deferred, so it resolves to holds.
		if concrete.HasStepBound && concrete.StepBound <= 1 {
			return holds()
		}
		if concrete.HasDeadline && !now.Before(concrete.Deadline) {
			return holds()
		}
		next := concrete
		next.Inner = concrete.Inner
		if concrete.HasStepBound {
			next.StepBound = concrete.StepBound - 1
		}
		if innerResult.status == statusHolds {
			return pending(next)
		}
		return pending(AndFormula{Left: innerResult.formula, Right: next})
	}

	panic(fmt.Sprintf("ltl: unsupported formula type %T", formula))
}
