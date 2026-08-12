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

func TestClassify_CleanRunIsCensoredAtTheBudget(t *testing.T) {
	item := classify(runRecord{Seed: 4, Steps: 50, DurationMillis: 1000}, 50)
	if item.ExcludedBecause != "" {
		t.Fatalf("excluded because %q", item.ExcludedBecause)
	}
	if item.Violated {
		t.Error("clean run marked as violated")
	}
	current := arm{Budget: 50, Runs: []classifiedRun{item}}
	observations := current.observations()
	if len(observations) != 1 || observations[0].Event || observations[0].Steps != 50 {
		t.Errorf("observations %+v, want one censored observation at 50", observations)
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
	if !item.Violated || item.OriginStep != 50 || !item.ClampedToBudget {
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

func TestGroupArms_PoolsDirectoriesSharingAnArmAndReportsMissingSeeds(t *testing.T) {
	root := t.TempDir()
	writeCampaign(t, filepath.Join(root, "north"), map[string]any{
		"arm": "seeded", "max_steps": 40, "seeds": []int{1, 2, 3},
	}, []map[string]any{
		{"seed": 1, "exit_code": 0, "steps": 40},
		{"seed": 2, "exit_code": 0, "steps": 9, "first_violation_origin_step": 9},
	})
	writeCampaign(t, filepath.Join(root, "south"), map[string]any{
		"arm": "seeded", "max_steps": 40, "seeds": []int{4},
	}, []map[string]any{
		{"seed": 4, "exit_code": 0, "steps": 40},
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
		[]map[string]any{{"seed": 1, "exit_code": 0, "steps": 40}})
	writeCampaign(t, filepath.Join(root, "b"), map[string]any{"arm": "seeded", "max_steps": 80, "seeds": []int{2}},
		[]map[string]any{{"seed": 2, "exit_code": 0, "steps": 80}})

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
	if err := os.WriteFile(filepath.Join(directory, recordsFileName), []byte("{\"seed\":1}\nnot json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := groupArms([]string{directory})
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("error %v, want the offending line number", err)
	}
}
