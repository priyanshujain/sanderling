package ltl

import (
	"math/rand/v2"
	"testing"
	"testing/quick"
	"time"
)

// dualityTrace holds the per-step truth of each predicate. The thunks read the
// current step out of the trace rather than counting their own invocations, so
// a formula and its negation see the same values no matter how many times each
// predicate is reduced.
type dualityTrace struct {
	step   int
	values [2][]bool
}

func newDualityTrace(random *rand.Rand, steps int) *dualityTrace {
	trace := &dualityTrace{}
	for index := range trace.values {
		trace.values[index] = make([]bool, steps)
		for step := range trace.values[index] {
			trace.values[index][step] = random.IntN(2) == 0
		}
	}
	return trace
}

func (t *dualityTrace) atoms() []Formula {
	return []Formula{
		ThunkNamed("p0", func() (bool, error) { return t.values[0][t.step], nil }),
		ThunkNamed("p1", func() (bool, error) { return t.values[1][t.step], nil }),
		Pure(true),
		Pure(false),
	}
}

// randomFormula draws a formula of at most the given depth over the atoms.
// Every operator the evaluator can reduce is reachable, including the bounded
// Always that only nnf produces.
func randomFormula(random *rand.Rand, depth int, atoms []Formula) Formula {
	if depth == 0 {
		return atoms[random.IntN(len(atoms))]
	}
	inner := func() Formula { return randomFormula(random, depth-1, atoms) }
	bound := random.IntN(3) + 1
	switch random.IntN(12) {
	case 0, 1:
		return atoms[random.IntN(len(atoms))]
	case 2:
		return Not(inner())
	case 3:
		return Next(inner())
	case 4:
		return Now(inner())
	case 5:
		return And(inner(), inner())
	case 6:
		return Or(inner(), inner())
	case 7:
		return Implies(inner(), inner())
	case 8:
		return Always(inner())
	case 9:
		return Eventually(inner())
	case 10:
		if random.IntN(2) == 0 {
			return EventuallyWithinSteps(inner(), bound)
		}
		return EventuallyWithin(inner(), time.Duration(bound)*time.Second)
	default:
		if random.IntN(2) == 0 {
			return AlwaysFormula{Inner: inner(), StepBound: bound, HasStepBound: true}
		}
		return AlwaysFormula{Inner: inner(), Duration: time.Duration(bound) * time.Second}
	}
}

// TestNNF_ExcludedMiddleHoldsOnRandomTraces is the semantic counterpart to the
// describe()-comparing laws in nnf_test.go: those check that nnf produces the
// syntax the dual laws predict, this checks that the syntax it produces means
// the same thing. If any operator pair reduces non-dually then some trace makes
// a formula and its own nnf-negation both violate, so their disjunction
// violates and their conjunction holds.
func TestNNF_ExcludedMiddleHoldsOnRandomTraces(t *testing.T) {
	const steps = 8
	law := func(seed uint64) bool {
		random := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
		trace := newDualityTrace(random, steps)
		formula := randomFormula(random, 3, trace.atoms())

		tautology := NewEvaluator(Or(formula, nnf(Not(formula))))
		contradiction := NewEvaluator(And(formula, nnf(Not(formula))))
		for index := range steps {
			trace.step = index
			now := time.Unix(int64(index), 0)
			if tautology.ObserveAtStep(now, index+1) == VerdictViolated {
				t.Logf("seed %d: %s violated at step %d", seed, Describe(formula), index+1)
				return false
			}
			if contradiction.ObserveAtStep(now, index+1) == VerdictHolds {
				t.Logf("seed %d: %s held at step %d", seed, Describe(formula), index+1)
				return false
			}
		}
		return tautology.Finalize() != VerdictViolated
	}
	if err := quick.Check(law, &quick.Config{MaxCount: 2000}); err != nil {
		t.Error(err)
	}
}

// TestNNF_BoundedAlwaysOverNextIsDual is the counterexample that showed nnf was
// not semantics preserving: phi = G<=1(X p) with p true only at the first step.
// Before the bounded operators were made dual, phi violated at step 2 and
// nnf(not phi) violated at step 1, so both a formula and its negation failed on
// one trace. Exactly one of the pair may violate.
func TestNNF_BoundedAlwaysOverNextIsDual(t *testing.T) {
	step := 0
	predicate := ThunkNamed("p", func() (bool, error) { return step == 0, nil })
	formula := AlwaysFormula{Inner: Next(predicate), StepBound: 1, HasStepBound: true}
	negated := nnf(Not(formula))

	run := func(root Formula) Verdict {
		step = 0
		evaluator := NewEvaluator(root)
		for index := range 3 {
			step = index
			if evaluator.ObserveAtStep(time.Unix(int64(index), 0), index+1) == VerdictViolated {
				return VerdictViolated
			}
		}
		return evaluator.Finalize()
	}

	if got := run(formula); got == VerdictViolated {
		t.Errorf("G<=1(X p) = %v; the window closes on a deferred check, which is not a breach", got)
	}
	if got := run(negated); got != VerdictViolated {
		t.Errorf("nnf(not G<=1(X p)) = %v, want violated", got)
	}
	if got := run(Or(formula, negated)); got == VerdictViolated {
		t.Errorf("excluded middle violated: %v", got)
	}
	if got := run(And(formula, negated)); got != VerdictViolated {
		t.Errorf("phi and not phi = %v, want violated", got)
	}
}

// TestEventually_PendingInnerSurvivesAsDisjunct locks the conjunct/disjunct
// mirror the duality rests on: Always keeps a pending inner as a conjunct of
// its residual, so Eventually must keep one as a disjunct. Dropping it made an
// inner that can only discharge on a later step unsatisfiable.
func TestEventually_PendingInnerSurvivesAsDisjunct(t *testing.T) {
	values := []bool{false, true, false}
	step := 0
	formula := EventuallyWithinSteps(Next(ThunkNamed("p", func() (bool, error) {
		return values[step], nil
	})), 2)
	evaluator := NewEvaluator(formula)

	if got := evaluator.ObserveAtStep(time.Unix(0, 0), 1); got != VerdictPending {
		t.Fatalf("step 1: got %v, want pending", got)
	}
	step = 1
	if got := evaluator.ObserveAtStep(time.Unix(1, 0), 2); got != VerdictHolds {
		t.Errorf("step 2: got %v, want holds (X p armed at step 1 discharged here)", got)
	}
}
