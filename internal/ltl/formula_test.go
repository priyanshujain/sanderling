package ltl

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDescribe_NowNextEventually(t *testing.T) {
	now := Always(Now(Pure(true)))
	if got := Describe(now); !strings.Contains(got, "Now") || !strings.Contains(got, "Always") {
		t.Errorf("Describe(Always(Now(Pure(true)))) = %q", got)
	}
	next := Always(Next(Pure(false)))
	if got := Describe(next); !strings.Contains(got, "Next") {
		t.Errorf("Describe next = %q", got)
	}
	ev := Always(EventuallyWithinSteps(Pure(true), 3))
	if got := Describe(ev); !strings.Contains(got, "Eventually") || !strings.Contains(got, "steps=3") {
		t.Errorf("Describe eventually = %q", got)
	}
}

func TestDescribe_ImpliesOrAndNot(t *testing.T) {
	implies := Implies(Pure(true), Pure(false))
	if got := Describe(implies); !strings.Contains(got, "Implies") {
		t.Errorf("Describe implies = %q", got)
	}
	or := Or(Pure(true), Pure(false))
	if got := Describe(or); !strings.Contains(got, "Or") {
		t.Errorf("Describe or = %q", got)
	}
	and := And(Pure(true), Pure(false))
	if got := Describe(and); !strings.Contains(got, "And") {
		t.Errorf("Describe and = %q", got)
	}
	not := Not(Pure(true))
	if got := Describe(not); !strings.Contains(got, "Not") {
		t.Errorf("Describe not = %q", got)
	}
}

func TestAlways_Now_ViolatesImmediately(t *testing.T) {
	evaluator := NewEvaluator(Always(Now(Pure(false))))
	if got := evaluator.Observe(); got != VerdictViolated {
		t.Errorf("step 1: got %v, want violated", got)
	}
}

func TestAlways_Next_PendingThenViolated(t *testing.T) {
	y := true
	evaluator := NewEvaluator(Always(Next(Thunk(func() bool { return y }))))

	if got := evaluator.Observe(); got != VerdictPending {
		t.Errorf("step 1: got %v, want pending", got)
	}
	y = false
	if got := evaluator.Observe(); got != VerdictViolated {
		t.Errorf("step 2: got %v, want violated", got)
	}
}

func TestAlways_Next_StaysPendingWhileInnerHolds(t *testing.T) {
	evaluator := NewEvaluator(Always(Next(Thunk(func() bool { return true }))))
	for index := range 3 {
		if got := evaluator.ObserveAt(time.Unix(int64(index), 0)); got != VerdictPending {
			t.Errorf("step %d: got %v, want pending", index+1, got)
		}
	}
}

func TestAlways_NowImpliesEventuallyWithin_ViolatesWhenYLate(t *testing.T) {
	// always(now(() => x).implies(eventually(() => y).within(3, "steps")))
	// x = true only at step 1; y = true only at step 4.
	xValues := []bool{true, false, false, false, false}
	yValues := []bool{false, false, false, true, true}
	step := 0
	predX := Thunk(func() bool { return xValues[step] })
	predY := Thunk(func() bool { return yValues[step] })

	formula := Always(Implies(Now(predX), EventuallyWithinSteps(predY, 3)))
	evaluator := NewEvaluator(formula)

	verdicts := make([]Verdict, 0, 5)
	for range 5 {
		verdicts = append(verdicts, evaluator.Observe())
		step++
	}

	// Step 1: X true, eventually(Y, 3) spawned pending. Pending.
	// Step 2: pending eventually decrements (Y false). Pending.
	// Step 3: eventually bound exhausted (Y still false). Violated.
	if verdicts[0] != VerdictPending {
		t.Errorf("step 1: got %v, want pending", verdicts[0])
	}
	if verdicts[1] != VerdictPending {
		t.Errorf("step 2: got %v, want pending", verdicts[1])
	}
	if verdicts[2] != VerdictViolated {
		t.Errorf("step 3: got %v, want violated", verdicts[2])
	}
}

func TestAlways_NowImpliesEventuallyWithin_HoldsWhenYInBound(t *testing.T) {
	// Same formula, y = true at step 3 (within the 3-step bound).
	xValues := []bool{true, false, false}
	yValues := []bool{false, false, true}
	step := 0
	predX := Thunk(func() bool { return xValues[step] })
	predY := Thunk(func() bool { return yValues[step] })

	formula := Always(Implies(Now(predX), EventuallyWithinSteps(predY, 3)))
	evaluator := NewEvaluator(formula)

	verdicts := make([]Verdict, 0, 3)
	for range 3 {
		verdicts = append(verdicts, evaluator.Observe())
		step++
	}

	if verdicts[0] != VerdictPending {
		t.Errorf("step 1: got %v, want pending", verdicts[0])
	}
	if verdicts[1] != VerdictPending {
		t.Errorf("step 2: got %v, want pending", verdicts[1])
	}
	if verdicts[2] != VerdictHolds {
		t.Errorf("step 3: got %v, want holds", verdicts[2])
	}
}

func TestEventually_DeadlineViolation(t *testing.T) {
	base := time.Unix(0, 0)
	deadline := base.Add(1 * time.Second)
	formula := Always(EventuallyBefore(Pure(false), deadline))
	evaluator := NewEvaluator(formula)

	// Well before deadline: pending.
	if got := evaluator.ObserveAt(base.Add(100 * time.Millisecond)); got != VerdictPending {
		t.Errorf("pre-deadline: got %v, want pending", got)
	}
	// At or past deadline: violated.
	if got := evaluator.ObserveAt(base.Add(2 * time.Second)); got != VerdictViolated {
		t.Errorf("post-deadline: got %v, want violated", got)
	}
}

func TestEventually_RelativeDurationResolvesOnFirstReduce(t *testing.T) {
	base := time.Unix(0, 0)
	// One-shot Eventually (not wrapped in Always) with a 1s relative deadline.
	evaluator := NewEvaluator(EventuallyWithin(Pure(false), 1*time.Second))

	if got := evaluator.ObserveAt(base); got != VerdictPending {
		t.Errorf("creation step: got %v, want pending", got)
	}
	if got := evaluator.ObserveAt(base.Add(500 * time.Millisecond)); got != VerdictPending {
		t.Errorf("mid-window: got %v, want pending", got)
	}
	if got := evaluator.ObserveAt(base.Add(2 * time.Second)); got != VerdictViolated {
		t.Errorf("past-window: got %v, want violated", got)
	}
}

func TestOr_OneBranchHolds(t *testing.T) {
	evaluator := NewEvaluator(Always(Or(Pure(false), Pure(true))))
	if got := evaluator.Observe(); got != VerdictHolds {
		t.Errorf("or(false,true): got %v, want holds", got)
	}
}

func TestAnd_OneBranchViolatesLatches(t *testing.T) {
	evaluator := NewEvaluator(Always(And(Pure(true), Pure(false))))
	if got := evaluator.Observe(); got != VerdictViolated {
		t.Errorf("and(true,false): got %v, want violated", got)
	}
}

func TestNot_InvertsPure(t *testing.T) {
	holds := NewEvaluator(Always(Not(Pure(false))))
	if got := holds.Observe(); got != VerdictHolds {
		t.Errorf("not(false): got %v, want holds", got)
	}
	violates := NewEvaluator(Always(Not(Pure(true))))
	if got := violates.Observe(); got != VerdictViolated {
		t.Errorf("not(true): got %v, want violated", got)
	}
}

func TestVerdict_StringPending(t *testing.T) {
	if got := VerdictPending.String(); got != "pending" {
		t.Errorf("VerdictPending.String() = %q", got)
	}
}

func TestMarshalJSON_AlwaysImpliesEventually(t *testing.T) {
	formula := Always(Implies(Now(Pure(true)), EventuallyWithinSteps(Pure(false), 3)))
	body, err := json.Marshal(formula)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"op":"always","arg":{"op":"implies","left":{"op":"now","arg":{"op":"true"}},"right":{"op":"eventually","arg":{"op":"false"},"within":{"amount":3,"unit":"steps"}}}}`
	if string(body) != want {
		t.Errorf("marshal mismatch:\n got: %s\nwant: %s", body, want)
	}
}

func TestMarshalJSON_AndOrNot(t *testing.T) {
	formula := And(Or(Pure(true), Pure(false)), Not(Pure(true)))
	body, _ := json.Marshal(formula)
	want := `{"op":"and","left":{"op":"or","left":{"op":"true"},"right":{"op":"false"}},"right":{"op":"not","arg":{"op":"true"}}}`
	if string(body) != want {
		t.Errorf("and/or/not marshal mismatch:\n got: %s\nwant: %s", body, want)
	}
}

func TestMarshalJSON_EventuallyMillisecondsAndDeadline(t *testing.T) {
	body, _ := json.Marshal(EventuallyWithin(Pure(true), 250*time.Millisecond))
	if !strings.Contains(string(body), `"unit":"milliseconds"`) || !strings.Contains(string(body), `"amount":250`) {
		t.Errorf("milliseconds within wrong: %s", body)
	}
	deadline := time.UnixMilli(1700000000000)
	body, _ = json.Marshal(EventuallyBefore(Pure(true), deadline))
	if !strings.Contains(string(body), `"unit":"deadline"`) || !strings.Contains(string(body), `"amount":1700000000000`) {
		t.Errorf("deadline within wrong: %s", body)
	}
}

func TestMarshalJSON_NextAndThunkAndError(t *testing.T) {
	body, _ := json.Marshal(Next(Pure(true)))
	if string(body) != `{"op":"next","arg":{"op":"true"}}` {
		t.Errorf("next marshal wrong: %s", body)
	}
	body, _ = json.Marshal(Thunk(func() bool { return true }))
	if string(body) != `{"op":"predicate"}` {
		t.Errorf("thunk marshal wrong: %s", body)
	}
	body, _ = json.Marshal(ErrorFormula{Message: "bad"})
	if string(body) != `{"op":"error","message":"bad"}` {
		t.Errorf("error marshal wrong: %s", body)
	}
}

func TestResidual_HoldsViolatedPending(t *testing.T) {
	holdsEval := NewEvaluator(Always(Pure(true)))
	holdsEval.Observe()
	if got := holdsEval.Residual(); got != (PureFormula{Value: true}) {
		t.Errorf("holds residual = %v, want true", got)
	}

	violEval := NewEvaluator(Always(Now(Pure(false))))
	violEval.Observe()
	if got := violEval.Residual(); got != (PureFormula{Value: false}) {
		t.Errorf("violated residual = %v, want false", got)
	}

	pendingEval := NewEvaluator(Always(Next(Pure(true))))
	pendingEval.Observe()
	body, err := json.Marshal(pendingEval.Residual())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"op":"and"`) && !strings.Contains(string(body), `"op":"true"`) {
		t.Errorf("pending residual unexpected: %s", body)
	}
}
