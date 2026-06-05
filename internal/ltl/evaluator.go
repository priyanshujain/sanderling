// Package ltl evaluates linear temporal logic formulas incrementally over observed steps.
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
	root      Formula
	pending   []obligation
	violated  bool
	steps     int
	violation *Violation
}

// obligation pairs a residual formula with the step that spawned it, so a
// deferred check (a next, a pending eventually) that fails on a later step can
// be attributed to the step that created the obligation.
type obligation struct {
	formula Formula
	origin  int
}

// Violation is the witness for a latched verdict: the failing sub-formula, a
// human-readable reason, and the step the failed obligation originated at. For
// an immediate predicate failure that is the observation step itself; for a
// deferred obligation (next, eventually) it is the earlier step that spawned
// it, the one that caused the violation. A thrown predicate carries the goja
// error text as its reason and sets IsError; a plain false carries "predicate
// false"; Finalize fills it for liveness obligations that never discharged.
type Violation struct {
	Formula Formula
	Reason  string
	Step    int
	IsError bool
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

// ObserveAt is like Observe but takes the current step time explicitly. Steps
// are numbered by an internal counter starting at 1; callers whose step
// numbering can skip observations should use ObserveAtStep instead.
func (e *Evaluator) ObserveAt(now time.Time) Verdict {
	return e.ObserveAtStep(now, e.steps+1)
}

// ObserveAtStep is like ObserveAt but labels the observation with the caller's
// step index, so violation witnesses carry the caller's numbering even when
// some steps were never observed (for example transitional steps the verifier
// skips).
func (e *Evaluator) ObserveAtStep(now time.Time, step int) Verdict {
	if e.violated {
		return VerdictViolated
	}
	e.steps = step

	fresh := obligation{formula: rootObligation(e.root), origin: step}
	obligations := append(e.pending, fresh)
	e.pending = e.pending[:0]

	for _, entry := range obligations {
		result := reduce(entry.formula, now)
		switch result.status {
		case statusHolds:
			// drop
		case statusViolated:
			e.violated = true
			e.pending = nil
			e.violation = result.witness
			if e.violation != nil {
				e.violation.Step = entry.origin
			}
			return VerdictViolated
		case statusPending:
			e.pending = append(e.pending, obligation{formula: result.formula, origin: entry.origin})
		}
	}

	e.pending = collapse(e.pending)

	if len(e.pending) > 0 {
		return VerdictPending
	}
	return VerdictHolds
}

// collapse removes structurally-identical obligations, keeping the first
// occurrence in order so the surviving entry carries the earliest origin step.
// Distinct predicates never merge because ThunkFormula's name participates in
// its describe() key, so deduping cannot hide a violation.
func collapse(obligations []obligation) []obligation {
	if len(obligations) < 2 {
		return obligations
	}
	seen := make(map[string]struct{}, len(obligations))
	result := obligations[:0]
	for _, entry := range obligations {
		key := entry.formula.describe()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, entry)
	}
	return result
}

// Finalize reports the terminal verdict for the run. A liveness promise that
// never discharged (an eventually that never fired) resolves to Violated. A
// deferred state check (the residue of a next) has no successor state to
// evaluate against, so it is indefinite and resolves vacuously to Holds: the
// run ended before the obligation could be checked, which is not a failure.
func (e *Evaluator) Finalize() Verdict {
	if e.violated {
		return VerdictViolated
	}
	for _, entry := range e.pending {
		if finalize(entry.formula) == statusViolated {
			e.violated = true
			e.pending = nil
			e.violation = &Violation{
				Formula: entry.formula,
				Reason:  finalizeReason(entry.formula),
				Step:    entry.origin,
			}
			return VerdictViolated
		}
	}
	return VerdictHolds
}

// Violation returns the witness for a latched violation, or nil if the
// evaluator has not violated. The witness is set by ObserveAt at the step a
// reduction first violated, or by Finalize for a liveness obligation that
// never discharged.
func (e *Evaluator) Violation() *Violation {
	return e.violation
}

// finalizeReason describes why an undischarged obligation resolves to violated
// at run end.
func finalizeReason(formula Formula) string {
	switch formula.(type) {
	case EventuallyFormula:
		return "eventually never satisfied"
	default:
		return "liveness obligation unmet at run end"
	}
}

// finalize collapses a pending obligation to its terminal status assuming no
// further steps will occur. Three-valued: a pending thunk or next is the
// residue of a deferred state check with no state left to check, so it is
// indefinite (statusPending) rather than violated; only a liveness promise
// (an eventually that never fired) is a definite end-of-run violation.
// Connectives combine with Kleene semantics so an indefinite sub-formula
// never manufactures a definite verdict.
func finalize(formula Formula) residualStatus {
	switch concrete := formula.(type) {
	case PureFormula:
		if concrete.Value {
			return statusHolds
		}
		return statusViolated
	case ThunkFormula:
		return statusPending
	case EventuallyFormula:
		return statusViolated
	case NextFormula:
		return statusPending
	case AlwaysFormula:
		return statusHolds
	case NowFormula:
		return finalize(concrete.Inner)
	case NotFormula:
		switch finalize(concrete.Inner) {
		case statusViolated:
			return statusHolds
		case statusHolds:
			return statusViolated
		default:
			return statusPending
		}
	case AndFormula:
		left, right := finalize(concrete.Left), finalize(concrete.Right)
		if left == statusViolated || right == statusViolated {
			return statusViolated
		}
		if left == statusPending || right == statusPending {
			return statusPending
		}
		return statusHolds
	case OrFormula:
		left, right := finalize(concrete.Left), finalize(concrete.Right)
		if left == statusHolds || right == statusHolds {
			return statusHolds
		}
		if left == statusPending || right == statusPending {
			return statusPending
		}
		return statusViolated
	case ImpliesFormula:
		return finalize(OrFormula{
			Left:  NotFormula{Inner: concrete.Antecedent},
			Right: concrete.Consequent,
		})
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
	combined := e.pending[0].formula
	for _, entry := range e.pending[1:] {
		combined = AndFormula{Left: combined, Right: entry.formula}
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
	witness *Violation
}

func holds() reduceResult { return reduceResult{status: statusHolds} }

// violatedWith reports a violation that originates at the given sub-formula
// with the given reason. The reason distinguishes a thrown predicate from a
// plain false so callers (and the replay UI) can render the cause.
func violatedWith(formula Formula, reason string) reduceResult {
	return reduceResult{
		status:  statusViolated,
		witness: &Violation{Formula: formula, Reason: reason},
	}
}

// violatedByError reports a violation caused by a predicate that threw. The
// witness keeps the error text as its reason and flags IsError so callers can
// render it as a thrown-predicate error rather than a plain false.
func violatedByError(formula Formula, reason string) reduceResult {
	return reduceResult{
		status:  statusViolated,
		witness: &Violation{Formula: formula, Reason: reason, IsError: true},
	}
}

// violatedFrom propagates a child violation, preferring the child's witness so
// the deepest failing leaf survives. When the child carried no witness the
// fallback formula and reason describe this level instead.
func violatedFrom(child reduceResult, fallback Formula, reason string) reduceResult {
	if child.witness != nil {
		return reduceResult{status: statusViolated, witness: child.witness}
	}
	return violatedWith(fallback, reason)
}

func pending(f Formula) reduceResult {
	return reduceResult{status: statusPending, formula: f}
}

func reduce(formula Formula, now time.Time) reduceResult {
	switch concrete := formula.(type) {
	case PureFormula:
		if concrete.Value {
			return holds()
		}
		return violatedWith(concrete, "pure false")

	case ThunkFormula:
		result, err := concrete.Func()
		if err != nil {
			return violatedByError(concrete, err.Error())
		}
		if result {
			return holds()
		}
		return violatedWith(concrete, "predicate false")

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
			return violatedFrom(innerResult, concrete, "eventually bound exhausted")
		}
		if concrete.HasDeadline && !now.Before(concrete.Deadline) {
			return violatedFrom(innerResult, concrete, "eventually deadline reached")
		}
		next := concrete
		if concrete.HasStepBound {
			next.StepBound = concrete.StepBound - 1
		}
		return pending(next)

	case ImpliesFormula:
		// NewEvaluator runs nnf, which rewrites a -> b to (not a) or b, so this
		// case is unreachable from a normal evaluator. A directly-constructed
		// formula reduced here is evaluated through the same equivalence so a
		// pending antecedent cannot drop the consequent.
		return reduce(OrFormula{
			Left:  pushNot(concrete.Antecedent),
			Right: nnf(concrete.Consequent),
		}, now)

	case OrFormula:
		left := reduce(concrete.Left, now)
		right := reduce(concrete.Right, now)
		if left.status == statusHolds || right.status == statusHolds {
			return holds()
		}
		if left.status == statusViolated && right.status == statusViolated {
			return violatedFrom(left, concrete, "both disjuncts violated")
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
		if left.status == statusViolated {
			return violatedFrom(left, concrete, "conjunct violated")
		}
		if right.status == statusViolated {
			return violatedFrom(right, concrete, "conjunct violated")
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
			return violatedWith(concrete, "negated formula held")
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
			return violatedFrom(innerResult, concrete, "always inner violated")
		}
		// A bounded Always is the dual of a bounded Eventually: once the window
		// closes without a breach it is vacuously satisfied. A pending inner at
		// the closing step is a deferred obligation (a strong next, or an inner
		// liveness that has not discharged); it must be carried so a later step
		// or Finalize resolves it, never dropped to holds.
		if concrete.HasStepBound && concrete.StepBound <= 1 {
			if innerResult.status == statusHolds {
				return holds()
			}
			return pending(innerResult.formula)
		}
		if concrete.HasDeadline && !now.Before(concrete.Deadline) {
			if innerResult.status == statusHolds {
				return holds()
			}
			return pending(innerResult.formula)
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
