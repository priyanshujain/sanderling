package ltl

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func alwaysFalse() func() (bool, error) {
	return func() (bool, error) { return false, nil }
}

// A step-bounded window counts the observations the evaluator reduced, and the
// residual has to keep saying which window the spec authored rather than the
// part of it that is left. Bug class: the replay UI renders "within N steps"
// straight off the residual, so a shrinking N tells the reader the spec asked
// for a window it never asked for.
func TestStepBoundedEventually_ResidualKeepsAuthoredWindow(t *testing.T) {
	evaluator := NewEvaluator(EventuallyWithinSteps(ThunkNamed("p", alwaysFalse()), 5))
	for index := range 3 {
		if got := evaluator.ObserveAt(time.Unix(int64(index), 0)); got != VerdictPending {
			t.Fatalf("observation %d: got %v, want pending", index+1, got)
		}
	}

	body, err := json.Marshal(evaluator.Residual())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"unit":"steps"`) || !strings.Contains(string(body), `"amount":5`) {
		t.Errorf("authored window lost after reduction: %s", body)
	}
	if !strings.Contains(string(body), `"expiresAtObservation":5`) {
		t.Errorf("resolved expiry missing: %s", body)
	}
}

// Two obligations spawned at different observations from one `within(n,
// "steps")` window close at different observations, and the serialized AST has
// to keep them apart the way a resolved deadline keeps two duration-bounded
// ones apart. Bug class: the trace shows one node where the evaluator holds
// several distinct obligations.
func TestStepBoundedEventually_ObligationsSerializeApart(t *testing.T) {
	evaluator := NewEvaluator(Always(EventuallyWithinSteps(ThunkNamed("p", alwaysFalse()), 3)))
	for index := range 2 {
		if got := evaluator.ObserveAt(time.Unix(int64(index), 0)); got != VerdictPending {
			t.Fatalf("observation %d: got %v, want pending", index+1, got)
		}
	}

	body, err := json.Marshal(evaluator.Residual())
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Count(text, `"amount":3`) != 2 {
		t.Errorf("both obligations should report the authored window of 3: %s", text)
	}
	if !strings.Contains(text, `"expiresAtObservation":3`) || !strings.Contains(text, `"expiresAtObservation":4`) {
		t.Errorf("obligations armed at different observations share a closing observation: %s", text)
	}
}

// A bounded Always is the dual of a bounded Eventually, so its window resolves
// and serializes the same way.
func TestStepBoundedAlways_ResidualKeepsAuthoredWindow(t *testing.T) {
	formula := AlwaysFormula{
		Inner:        ThunkNamed("p", func() (bool, error) { return true, nil }),
		StepBound:    4,
		HasStepBound: true,
	}
	evaluator := NewEvaluator(formula)
	for index := range 2 {
		if got := evaluator.ObserveAt(time.Unix(int64(index), 0)); got != VerdictPending {
			t.Fatalf("observation %d: got %v, want pending", index+1, got)
		}
	}

	body, err := json.Marshal(evaluator.Residual())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"amount":4`) {
		t.Errorf("authored window lost after reduction: %s", body)
	}
	if !strings.Contains(string(body), `"expiresAtObservation":4`) {
		t.Errorf("resolved expiry missing: %s", body)
	}
}

// A step the verifier skipped (a transitional tree, an empty hierarchy) never
// reached the evaluator, so the property was given no chance to discharge
// there and the window must not charge for it. The runner's step numbering
// only labels the witness; it does not drive the window.
func TestStepBoundedEventually_SkippedRunnerStepsDoNotConsumeWindow(t *testing.T) {
	observed := 0
	inner := ThunkNamed("p", func() (bool, error) {
		observed++
		return observed == 3, nil
	})
	evaluator := NewEvaluator(EventuallyWithinSteps(inner, 3))

	var verdict Verdict
	for _, runnerStep := range []int{1, 7, 19} {
		verdict = evaluator.ObserveAtStep(time.Unix(int64(runnerStep), 0), runnerStep)
	}
	if verdict != VerdictHolds {
		t.Errorf("three observations inside a three-observation window: got %v, want holds", verdict)
	}
}

// The witness still carries the runner's numbering, so a report names the step
// that armed the obligation even though the window counted observations.
func TestStepBoundedEventually_WitnessCarriesRunnerStep(t *testing.T) {
	evaluator := NewEvaluator(EventuallyWithinSteps(ThunkNamed("p", alwaysFalse()), 2))
	for _, runnerStep := range []int{4, 11} {
		evaluator.ObserveAtStep(time.Unix(int64(runnerStep), 0), runnerStep)
	}

	witness := evaluator.Violation()
	if witness == nil {
		t.Fatal("no violation recorded")
	}
	if witness.Step != 4 {
		t.Errorf("witness Step = %d, want the runner step that armed the obligation (4)", witness.Step)
	}
}

// An undischarged bounded eventually is a broken liveness promise at run end
// whatever unit bounded it. Bug class: choosing "steps" over "seconds" quietly
// turning an unmet obligation into a vacuous pass.
func TestFinalize_StepBoundedEventuallyMatchesWallClock(t *testing.T) {
	byStep, stepEvaluator := runAndFinalize(EventuallyWithinSteps(ThunkNamed("p", alwaysFalse()), 50), 3)
	byClock, clockEvaluator := runAndFinalize(EventuallyWithin(ThunkNamed("p", alwaysFalse()), time.Hour), 3)

	if byStep != VerdictViolated || byClock != VerdictViolated {
		t.Fatalf("step bound = %v, wall clock = %v, want both violated", byStep, byClock)
	}
	stepWitness, clockWitness := stepEvaluator.Violation(), clockEvaluator.Violation()
	if stepWitness == nil || clockWitness == nil {
		t.Fatal("both undischarged obligations must carry a witness")
	}
	if stepWitness.Reason != clockWitness.Reason {
		t.Errorf("reasons diverge: step %q, wall clock %q", stepWitness.Reason, clockWitness.Reason)
	}
	if stepWitness.Step != clockWitness.Step {
		t.Errorf("origin steps diverge: step %d, wall clock %d", stepWitness.Step, clockWitness.Step)
	}
}

// The reason the unit exists. Two action-selection policies get the same
// 300-step budget and reach the same state at the same step, but the model
// policy takes 359 seconds where the seeded policy takes 47 because it makes a
// provider call per step. A wall-clock bound fails the slow policy on elapsed
// time alone; the same window written in steps decides both policies alike.
func TestStepBound_SlowPolicyDoesNotFailOnTimeAlone(t *testing.T) {
	const budget = 300
	const satisfiedAtObservation = 260
	seededCadence := 47 * time.Second / budget
	modelCadence := 359 * time.Second / budget

	run := func(cadence time.Duration, bound func(Formula) Formula) Verdict {
		observed := 0
		inner := ThunkNamed("someTransactionExists", func() (bool, error) {
			observed++
			return observed >= satisfiedAtObservation, nil
		})
		evaluator := NewEvaluator(bound(inner))
		base := time.Unix(1780000000, 0)
		for index := range budget {
			verdict := evaluator.ObserveAtStep(base.Add(time.Duration(index)*cadence), index+1)
			if verdict != VerdictPending {
				return verdict
			}
		}
		return evaluator.Finalize()
	}

	byClock := func(inner Formula) Formula { return EventuallyWithin(inner, 300*time.Second) }
	bySteps := func(inner Formula) Formula { return EventuallyWithinSteps(inner, 1915) }

	if got := run(seededCadence, byClock); got != VerdictHolds {
		t.Errorf("wall-clock bound under the seeded policy: got %v, want holds", got)
	}
	if got := run(modelCadence, byClock); got != VerdictViolated {
		t.Errorf("wall-clock bound under the model policy: got %v, want violated (the false positive this unit removes)", got)
	}
	if got := run(seededCadence, bySteps); got != VerdictHolds {
		t.Errorf("step bound under the seeded policy: got %v, want holds", got)
	}
	if got := run(modelCadence, bySteps); got != VerdictHolds {
		t.Errorf("step bound under the model policy: got %v, want holds", got)
	}
}
