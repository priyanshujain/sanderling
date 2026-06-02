package ltl

// nnf rewrites a formula into negation normal form: every NotFormula is pushed
// down until it wraps only an opaque leaf (a ThunkFormula or ErrorFormula).
// Temporal operators are dualized along the way (Always <-> Eventually) so the
// evaluator never has to reduce a negated temporal obligation, which it cannot
// do soundly across steps.
func nnf(formula Formula) Formula {
	switch concrete := formula.(type) {
	case NotFormula:
		return pushNot(concrete.Inner)
	case AlwaysFormula:
		next := concrete
		next.Inner = nnf(concrete.Inner)
		return next
	case EventuallyFormula:
		next := concrete
		next.Inner = nnf(concrete.Inner)
		return next
	case NextFormula:
		return NextFormula{Inner: nnf(concrete.Inner)}
	case NowFormula:
		return NowFormula{Inner: nnf(concrete.Inner)}
	case AndFormula:
		return AndFormula{Left: nnf(concrete.Left), Right: nnf(concrete.Right)}
	case OrFormula:
		return OrFormula{Left: nnf(concrete.Left), Right: nnf(concrete.Right)}
	case ImpliesFormula:
		// a -> b is rewritten to (not a) or b so the consequent is always
		// reduced live each step. Keeping it as ImpliesFormula let a pending
		// (temporal) antecedent defer the whole implication and silently drop a
		// consequent that was false at the current step.
		return OrFormula{
			Left:  pushNot(concrete.Antecedent),
			Right: nnf(concrete.Consequent),
		}
	default:
		return formula
	}
}

// pushNot returns the negation normal form of NOT f.
func pushNot(formula Formula) Formula {
	switch concrete := formula.(type) {
	case PureFormula:
		return PureFormula{Value: !concrete.Value}
	case ThunkFormula:
		return NotFormula{Inner: concrete}
	case ErrorFormula:
		return NotFormula{Inner: concrete}
	case NotFormula:
		return nnf(concrete.Inner)
	case AndFormula:
		return OrFormula{Left: pushNot(concrete.Left), Right: pushNot(concrete.Right)}
	case OrFormula:
		return AndFormula{Left: pushNot(concrete.Left), Right: pushNot(concrete.Right)}
	case ImpliesFormula:
		return AndFormula{
			Left:  nnf(concrete.Antecedent),
			Right: pushNot(concrete.Consequent),
		}
	case NowFormula:
		return NowFormula{Inner: pushNot(concrete.Inner)}
	case NextFormula:
		return NextFormula{Inner: pushNot(concrete.Inner)}
	case AlwaysFormula:
		return EventuallyFormula{
			Inner:        pushNot(concrete.Inner),
			StepBound:    concrete.StepBound,
			HasStepBound: concrete.HasStepBound,
			Duration:     concrete.Duration,
			Deadline:     concrete.Deadline,
			HasDeadline:  concrete.HasDeadline,
		}
	case EventuallyFormula:
		return AlwaysFormula{
			Inner:        pushNot(concrete.Inner),
			StepBound:    concrete.StepBound,
			HasStepBound: concrete.HasStepBound,
			Duration:     concrete.Duration,
			Deadline:     concrete.Deadline,
			HasDeadline:  concrete.HasDeadline,
		}
	default:
		return NotFormula{Inner: formula}
	}
}
