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
	// observations counts the states this evaluator actually reduced, which is
	// what a `within(n, "steps")` window is measured in. It differs from steps
	// whenever the caller's numbering skipped an observation, and the two are
	// told apart in the serialized AST by expiresAtObservation.
	observations int
	// oneShot marks a root that is armed once at the first observation rather
	// than re-asserted at every one; armed records that it has been.
	oneShot bool
	armed   bool
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
	normalized := nnf(formula)
	return &Evaluator{root: normalized, oneShot: isOneShotRoot(normalized)}
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
	e.observations++

	obligations := make([]obligation, 0, len(e.pending)+1)
	obligations = append(obligations, e.pending...)
	if formula, ok := e.instantiateRoot(); ok {
		obligations = append(obligations, obligation{formula: formula, origin: step})
	}
	e.pending = e.pending[:0]

	for _, entry := range obligations {
		result := reduce(entry.formula, now, e.observations)
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
// Equal describe() keys mean the same operators over the same predicates with
// the same remaining bounds, so the merged obligations reduce identically on
// every future and dropping one cannot hide a violation. Distinct predicates
// never merge because every thunk's construction-time identity is part of its
// key, whether or not the caller named it.
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

// instantiateRoot returns the obligation to register for this observation, and
// whether there is one at all. A one-shot root is armed only at the first
// observation; a recurring root is re-asserted at every one.
func (e *Evaluator) instantiateRoot() (Formula, bool) {
	if !e.oneShot {
		return rootObligation(e.root), true
	}
	if e.armed {
		return nil, false
	}
	e.armed = true
	return e.root, true
}

// isOneShotRoot reports whether a root formula is a single obligation for the
// whole run rather than one instance per observation.
//
// A root that carries its own horizon is one-shot: an eventually is a
// reachability goal ("this happens at some point"), and a bounded always is a
// single window. Re-instantiating either at every step would monitor a
// different property -- G F<=n(p) instead of F<=n(p), and G(p) instead of
// G<=n(p), the latter because a re-instantiated window restarts and never
// closes -- and would leave one live obligation per step behind.
//
// Every other root keeps the implicit-always reading: an unbounded always
// re-instantiates its inner (which is what gives each instance its own origin
// step), and a bare predicate or connective is re-asserted each observation.
func isOneShotRoot(root Formula) bool {
	switch concrete := root.(type) {
	case EventuallyFormula:
		return true
	case AlwaysFormula:
		return concrete.HasStepBound || concrete.HasDeadline || concrete.Duration > 0
	default:
		return false
	}
}

// rootObligation returns the formula a recurring root instantiates at each
// step. An outer Always is stripped so its inner is re-evaluated every step;
// any other root formula is itself re-instantiated each step (matching the
// v0.1 semantics where a bare Thunk is re-observed on every call).
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

// reduce advances one obligation against the current state. `now` is the
// observation's wall clock and `observation` its index in the sequence of
// states this evaluator reduced; the two are the clocks a duration-bounded and
// a step-bounded window are resolved against.
func reduce(formula Formula, now time.Time, observation int) reduceResult {
	switch concrete := formula.(type) {
	case PureFormula:
		if concrete.Value {
			return holds()
		}
		return violatedWith(concrete, "pure false")

	case ErrorFormula:
		// A thrown predicate substituted into a residual at the trace
		// boundary. Reducing it re-reports the same failure rather than
		// crashing the run.
		return violatedByError(concrete, concrete.Message)

	case ThunkFormula:
		result, err := concrete.predicate()
		if err != nil {
			return violatedByError(concrete, err.Error())
		}
		if result {
			return holds()
		}
		return violatedWith(concrete, "predicate false")

	case NowFormula:
		return reduce(concrete.Inner, now, observation)

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
		if concrete.HasStepBound && !concrete.HasExpiryObservation {
			concrete.ExpiryObservation = observation + concrete.StepBound - 1
			concrete.HasExpiryObservation = true
		}
		innerResult := reduce(concrete.Inner, now, observation)
		if innerResult.status == statusHolds {
			return holds()
		}
		// The window is measured in observations at which the inner could have
		// discharged, so an inner that is merely pending has not discharged and
		// the window closing on it is a violation.
		if concrete.HasExpiryObservation && observation >= concrete.ExpiryObservation {
			return violatedFrom(innerResult, concrete, "eventually bound exhausted")
		}
		if concrete.HasDeadline && !now.Before(concrete.Deadline) {
			return violatedFrom(innerResult, concrete, "eventually deadline reached")
		}
		next := concrete
		// F(inner) unrolls to inner or X F(inner). A pending inner is a
		// deferred way of satisfying the promise, so it is kept as a disjunct
		// rather than dropped; dropping it is what made an inner that only
		// resolves on a later step unsatisfiable, and it is the mirror of the
		// conjunct Always keeps below.
		if innerResult.status == statusPending {
			return pending(OrFormula{Left: innerResult.formula, Right: next})
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
		}, now, observation)

	case OrFormula:
		left := reduce(concrete.Left, now, observation)
		right := reduce(concrete.Right, now, observation)
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
		left := reduce(concrete.Left, now, observation)
		right := reduce(concrete.Right, now, observation)
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
		inner := reduce(concrete.Inner, now, observation)
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
		if concrete.HasStepBound && !concrete.HasExpiryObservation {
			concrete.ExpiryObservation = observation + concrete.StepBound - 1
			concrete.HasExpiryObservation = true
		}
		innerResult := reduce(concrete.Inner, now, observation)
		if innerResult.status == statusViolated {
			return violatedFrom(innerResult, concrete, "always inner violated")
		}
		// A bounded Always must reduce exactly as its dual does, so that
		// G<=n(f) and not F<=n(not f) agree on every trace. The dual of "the
		// window closed on an inner that never definitely held, so violate" is
		// "the window closed on an inner that was never definitely breached, so
		// hold". A pending inner has not been breached inside the window, so it
		// discharges vacuously here exactly as its negation violates on the
		// Eventually side.
		if concrete.HasExpiryObservation && observation >= concrete.ExpiryObservation {
			return holds()
		}
		if concrete.HasDeadline && !now.Before(concrete.Deadline) {
			return holds()
		}
		next := concrete
		if innerResult.status == statusHolds {
			return pending(next)
		}
		return pending(AndFormula{Left: innerResult.formula, Right: next})
	}

	panic(fmt.Sprintf("ltl: unsupported formula type %T", formula))
}
