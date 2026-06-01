package ltl

import (
	"testing"
	"testing/quick"
	"time"
)

// leaf builds a small set of representative atomic formulas indexed by a seed.
func leafFor(seed uint8) Formula {
	switch seed % 3 {
	case 0:
		return Pure(true)
	case 1:
		return Pure(false)
	default:
		return ThunkNamed("p", func() (bool, error) { return true, nil })
	}
}

func TestNNF_DoubleNegationIsIdentity(t *testing.T) {
	law := func(seed uint8) bool {
		leaf := leafFor(seed)
		doubled := nnf(Not(Not(leaf)))
		direct := nnf(leaf)
		return doubled.describe() == direct.describe()
	}
	if err := quick.Check(law, nil); err != nil {
		t.Error(err)
	}
}

func TestNNF_NotAlwaysIsEventuallyNot(t *testing.T) {
	law := func(seed uint8) bool {
		leaf := leafFor(seed)
		negated := nnf(Not(Always(leaf)))
		expected := nnf(Eventually(Not(leaf)))
		return negated.describe() == expected.describe()
	}
	if err := quick.Check(law, nil); err != nil {
		t.Error(err)
	}
}

func TestNNF_NotEventuallyIsAlwaysNot(t *testing.T) {
	law := func(seed uint8) bool {
		leaf := leafFor(seed)
		negated := nnf(Not(Eventually(leaf)))
		expected := nnf(Always(Not(leaf)))
		return negated.describe() == expected.describe()
	}
	if err := quick.Check(law, nil); err != nil {
		t.Error(err)
	}
}

func TestNNF_BoundedEventuallyDualKeepsBound(t *testing.T) {
	negated := nnf(Not(EventuallyWithinSteps(Pure(true), 4)))
	always, ok := negated.(AlwaysFormula)
	if !ok {
		t.Fatalf("expected AlwaysFormula, got %T", negated)
	}
	if !always.HasStepBound || always.StepBound != 4 {
		t.Errorf("bound not preserved: %+v", always)
	}
}

func TestNNF_PushesNotToThunkLeaf(t *testing.T) {
	formula := nnf(Always(Not(Always(ThunkNamed("p", func() (bool, error) { return true, nil })))))
	always, ok := formula.(AlwaysFormula)
	if !ok {
		t.Fatalf("expected AlwaysFormula, got %T", formula)
	}
	eventually, ok := always.Inner.(EventuallyFormula)
	if !ok {
		t.Fatalf("expected inner EventuallyFormula, got %T", always.Inner)
	}
	not, ok := eventually.Inner.(NotFormula)
	if !ok {
		t.Fatalf("expected NotFormula leaf, got %T", eventually.Inner)
	}
	if _, ok := not.Inner.(ThunkFormula); !ok {
		t.Errorf("expected Not to wrap a Thunk, got %T", not.Inner)
	}
}

func TestNNF_NotAlwaysTrueViaEvaluatorReportsViolated(t *testing.T) {
	evaluator := NewEvaluator(Always(Not(Always(ThunkNamed("p", func() (bool, error) { return true, nil })))))
	for index := range 4 {
		if got := evaluator.ObserveAt(time.Unix(int64(index), 0)); got == VerdictViolated {
			t.Fatalf("step %d latched violated prematurely", index)
		}
	}
	if got := evaluator.Finalize(); got != VerdictViolated {
		t.Errorf("Finalize = %v, want violated", got)
	}
}
