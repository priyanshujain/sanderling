package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stepPointer(value int) *int { return &value }

func TestClassify_FailedAndTimedOutRunsAreMissingDataNotCensored(t *testing.T) {
	cases := []struct {
		name   string
		record runRecord
		reason string
	}{
		{"launch error", runRecord{LaunchError: "fork/exec: no such file"}, reasonLaunchError},
		{"timed out", runRecord{TimedOut: true, ExitCode: -1}, reasonTimedOut},
		{"nonzero exit", runRecord{ExitCode: 3}, reasonNonzeroExit},
		{"unreadable trace", runRecord{TraceError: "no run directory with meta.json"}, reasonTraceError},
		{"violation at step zero", runRecord{FirstViolationOriginStep: stepPointer(0)}, reasonMalformedStep},
		{"violation without a step", runRecord{ViolatedProperties: []string{"cartTotal"}}, reasonMalformedStep},
	}
	for _, test := range cases {
		item := classify(test.record, 50)
		if item.ExcludedBecause != test.reason {
			t.Errorf("%s: excluded because %q, want %q", test.name, item.ExcludedBecause, test.reason)
		}
	}
}

// A run also ends when the campaign's wall clock does, so a clean run can stop
// well short of the budget. Censoring it at the budget would credit it with
// steps it never ran, and a slower arm loses fewer steps in the same wall clock
// than a fast one, so the credit does not cancel between arms.
func TestClassify_CleanRunIsCensoredAtTheStepsItRan(t *testing.T) {
	cases := []struct {
		name     string
		steps    int
		budget   int
		censored float64
	}{
		{"stopped by the wall clock short of the budget", 12, 400, 12},
		{"ran the whole budget", 400, 400, 400},
		{"recorded more steps than the manifest budget", 420, 400, 400},
	}
	for _, test := range cases {
		item := classify(runRecord{Seed: 4, Steps: test.steps, DurationMillis: 1000}, test.budget)
		if item.ExcludedBecause != "" || item.Violated {
			t.Fatalf("%s: run %+v, want a usable clean run", test.name, item)
		}
		current := arm{Budget: test.budget, Runs: []classifiedRun{item}}
		observations := current.observations()
		if len(observations) != 1 || observations[0].Event || observations[0].Steps != test.censored {
			t.Errorf("%s: observations %+v, want one censored observation at %v",
				test.name, observations, test.censored)
		}
	}
}

func TestClassify_ViolationIsAnEventAtTheOriginStep(t *testing.T) {
	item := classify(runRecord{Seed: 5, Steps: 12, FirstViolationOriginStep: stepPointer(7)}, 50)
	if !item.Violated || item.OriginStep != 7 || item.ClampedToBudget {
		t.Fatalf("run %+v, want an unclamped event at step 7", item)
	}
	current := arm{Budget: 50, Runs: []classifiedRun{item}}
	observations := current.observations()
	if len(observations) != 1 || !observations[0].Event || observations[0].Steps != 7 {
		t.Errorf("observations %+v, want one event at 7", observations)
	}
}

// The run-end finalize line reports at an index one past the last executed step,
// so an origin past the budget is held at the budget and counted rather than
// silently turned into a censored run.
func TestClassify_ViolationPastTheBudgetIsHeldAtTheBudget(t *testing.T) {
	item := classify(runRecord{FirstViolationOriginStep: stepPointer(51)}, 50)
	if !item.Violated || item.EventStep != 50 || !item.ClampedToBudget {
		t.Errorf("run %+v, want a clamped event at 50", item)
	}
}

func writeCampaign(t *testing.T, directory string, declared map[string]any, records []map[string]any) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := json.MarshalIndent(declared, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, manifestFileName), append(body, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	var lines strings.Builder
	for _, record := range records {
		line, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		lines.Write(line)
		lines.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(directory, recordsFileName), []byte(lines.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestClassify_CarriesTheDispatchedActionCount(t *testing.T) {
	actions := 7
	item := classify(runRecord{Seed: 4, Steps: 30, Actions: &actions}, 50)
	if item.Steps != 30 || item.Actions != 7 {
		t.Errorf("run %+v, want 30 steps and 7 actions", item)
	}
}

func TestGroupArms_PoolsDirectoriesSharingAnArmAndReportsMissingSeeds(t *testing.T) {
	root := t.TempDir()
	writeCampaign(t, filepath.Join(root, "north"), map[string]any{
		"arm": "seeded", "max_steps": 40, "seeds": []int{1, 2, 3},
	}, []map[string]any{
		{"seed": 1, "exit_code": 0, "steps": 40, "actions": 33},
		{"seed": 2, "exit_code": 0, "steps": 9, "actions": 8, "first_violation_origin_step": 9},
	})
	writeCampaign(t, filepath.Join(root, "south"), map[string]any{
		"arm": "seeded", "max_steps": 40, "seeds": []int{4},
	}, []map[string]any{
		{"seed": 4, "exit_code": 0, "steps": 40, "actions": 40},
	})

	arms, err := groupArms([]string{filepath.Join(root, "north"), filepath.Join(root, "south")})
	if err != nil {
		t.Fatal(err)
	}
	if len(arms) != 1 {
		t.Fatalf("%d arms, want 1", len(arms))
	}
	if len(arms[0].Runs) != 3 {
		t.Errorf("%d runs, want 3", len(arms[0].Runs))
	}
	if len(arms[0].MissingSeeds) != 1 || arms[0].MissingSeeds[0] != 3 {
		t.Errorf("missing seeds %v, want [3]", arms[0].MissingSeeds)
	}
	if len(arms[0].Directories) != 2 {
		t.Errorf("directories %v, want both", arms[0].Directories)
	}
}

func TestGroupArms_RejectsDisagreeingStepBudgets(t *testing.T) {
	root := t.TempDir()
	writeCampaign(t, filepath.Join(root, "a"), map[string]any{"arm": "seeded", "max_steps": 40, "seeds": []int{1}},
		[]map[string]any{{"seed": 1, "exit_code": 0, "steps": 40, "actions": 40}})
	writeCampaign(t, filepath.Join(root, "b"), map[string]any{"arm": "seeded", "max_steps": 80, "seeds": []int{2}},
		[]map[string]any{{"seed": 2, "exit_code": 0, "steps": 80, "actions": 80}})

	_, err := groupArms([]string{filepath.Join(root, "a"), filepath.Join(root, "b")})
	if err == nil || !strings.Contains(err.Error(), "different budgets") {
		t.Fatalf("error %v, want a refusal to pool different budgets", err)
	}
}

func TestGroupArms_RejectsAMissingStepBudget(t *testing.T) {
	root := t.TempDir()
	writeCampaign(t, filepath.Join(root, "a"), map[string]any{"arm": "seeded", "seeds": []int{1}},
		[]map[string]any{{"seed": 1, "exit_code": 0}})
	_, err := groupArms([]string{filepath.Join(root, "a")})
	if err == nil || !strings.Contains(err.Error(), "censored at") {
		t.Fatalf("error %v, want a complaint about max_steps", err)
	}
}

func TestGroupArms_ReportsBadRecordLines(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "a")
	writeCampaign(t, directory, map[string]any{"arm": "seeded", "max_steps": 40, "seeds": []int{1}}, nil)
	if err := os.WriteFile(filepath.Join(directory, recordsFileName),
		[]byte("{\"seed\":1,\"actions\":0}\nnot json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := groupArms([]string{directory})
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("error %v, want the offending line number", err)
	}
}

// A runs.jsonl written before the campaign counted dispatched actions has no
// such field. Reading the absence as zero would divide by zero, so the whole
// campaign is refused instead.
func TestGroupArms_RefusesRecordsWithoutADispatchedActionCount(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "old-format")
	writeCampaign(t, directory, map[string]any{"arm": "seeded", "max_steps": 40, "seeds": []int{1, 2}},
		[]map[string]any{
			{"seed": 1, "exit_code": 0, "steps": 40, "actions": 40},
			{"seed": 2, "exit_code": 0, "steps": 40},
		})
	_, err := groupArms([]string{directory})
	if err == nil {
		t.Fatal("a runs.jsonl without an action count was accepted")
	}
	for _, fragment := range []string{"line 2", "actions", directory} {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("error %q is missing %q", err, fragment)
		}
	}
}

func TestGroupArms_RefusesAnExcludedRecordWithoutAnActionCount(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "old-format")
	writeCampaign(t, directory, map[string]any{"arm": "seeded", "max_steps": 40, "seeds": []int{1}},
		[]map[string]any{{"seed": 1, "exit_code": 3}})
	if _, err := groupArms([]string{directory}); err == nil {
		t.Fatal("an old-format record was accepted because the run was excluded anyway")
	}
}
