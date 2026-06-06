package verifier

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/priyanshujain/sanderling/internal/ltl"
)

// TestCombinators_VerdictTransitions loads real specs through the goja runtime
// using the chainable LTL combinators (implies/or/and/not + now + within steps)
// and drives them across snapshots. Bug class: a user spec built from these
// combinators silently mis-evaluates (wrong verdict at the wrong step).
func TestCombinators_VerdictTransitions(t *testing.T) {
	const heads = `
globalThis.p = __sanderling__.extract(state => state.snapshots["p"] ?? false, "p");
globalThis.q = __sanderling__.extract(state => state.snapshots["q"] ?? false, "q");
`
	type step struct {
		p, q string
		want ltl.Verdict
	}
	cases := []struct {
		name  string
		body  string
		steps []step
	}{
		{
			name: "implies",
			body: `globalThis.properties = { r: __sanderling__.always(__sanderling__.now(()=>p.current).implies(__sanderling__.now(()=>q.current))) };`,
			steps: []step{
				{"false", "false", ltl.VerdictHolds}, // antecedent false -> vacuously holds
				{"true", "true", ltl.VerdictHolds},
				{"true", "false", ltl.VerdictViolated},  // p true, q false
				{"false", "false", ltl.VerdictViolated}, // sticky
			},
		},
		{
			name: "or",
			body: `globalThis.properties = { r: __sanderling__.always(__sanderling__.now(()=>p.current).or(__sanderling__.now(()=>q.current))) };`,
			steps: []step{
				{"true", "false", ltl.VerdictHolds},
				{"false", "true", ltl.VerdictHolds},
				{"false", "false", ltl.VerdictViolated},
			},
		},
		{
			name: "and",
			body: `globalThis.properties = { r: __sanderling__.always(__sanderling__.now(()=>p.current).and(__sanderling__.now(()=>q.current))) };`,
			steps: []step{
				{"true", "true", ltl.VerdictHolds},
				{"true", "false", ltl.VerdictViolated}, // one conjunct false
			},
		},
		{
			name: "not",
			body: `globalThis.properties = { r: __sanderling__.always(__sanderling__.now(()=>p.current).not()) };`,
			steps: []step{
				{"false", "false", ltl.VerdictHolds},
				{"true", "false", ltl.VerdictViolated},
			},
		},
		{
			name: "within_steps_deadline",
			body: `globalThis.properties = { r: __sanderling__.always(__sanderling__.eventually(()=>p.current).within(2,'steps')) };`,
			steps: []step{
				{"false", "false", ltl.VerdictPending},  // obligation open
				{"false", "false", ltl.VerdictViolated}, // deadline blown, never fired
			},
		},
		{
			name: "within_steps_satisfied",
			body: `globalThis.properties = { r: __sanderling__.always(__sanderling__.eventually(()=>p.current).within(2,'steps')) };`,
			steps: []step{
				{"false", "false", ltl.VerdictPending},
				{"true", "false", ltl.VerdictHolds}, // fired before deadline
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			verifier := newVerifier(t)
			mustLoad(t, verifier, heads+testCase.body)
			for i, s := range testCase.steps {
				if err := verifier.PushSnapshot(SnapshotInput{
					Snapshots: Snapshots{"p": json.RawMessage(s.p), "q": json.RawMessage(s.q)},
					StepIndex: i + 1,
				}); err != nil {
					t.Fatal(err)
				}
				if got := verifier.EvaluateProperties()["r"]; got != s.want {
					t.Errorf("step %d (p=%s q=%s): got %v, want %v", i+1, s.p, s.q, got, s.want)
				}
			}
		})
	}
}

// TestWithin_InvalidUnitPanics verifies an unrecognized within() unit surfaces
// as a spec load error rather than silently constructing an unbounded
// eventually. Bug class: a typo'd unit ('ms'/'s') would otherwise build a
// formula that never enforces its deadline.
func TestWithin_InvalidUnitPanics(t *testing.T) {
	for _, unit := range []string{"ms", "s", "minutes", ""} {
		verifier := newVerifier(t)
		src := `globalThis.properties = { r: __sanderling__.always(__sanderling__.eventually(()=>true).within(2,'` + unit + `')) };`
		err := verifier.Load(src)
		if err == nil {
			t.Errorf("unit %q: expected load error, got nil", unit)
			continue
		}
		if !strings.Contains(err.Error(), "within unit must be") {
			t.Errorf("unit %q: error = %v, want within-unit diagnostic", unit, err)
		}
	}
}
