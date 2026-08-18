package main

import (
	"math"
	"path/filepath"
	"testing"
)

// A run stops at whichever comes first, the step budget or the campaign's wall
// clock, so an arm that spends more wall clock per step leaves runs censored far
// below the budget. Those runs are not observations of a violation at that step,
// and the comparison between arms has to read them as the bounds they are.

type wallClockRun struct {
	steps    int
	violated bool
}

func stoppedShort(count, steps int) []wallClockRun {
	runs := make([]wallClockRun, 0, count)
	for index := 0; index < count; index++ {
		runs = append(runs, wallClockRun{steps: steps})
	}
	return runs
}

func violatedAt(count, steps int) []wallClockRun {
	runs := make([]wallClockRun, 0, count)
	for index := 0; index < count; index++ {
		runs = append(runs, wallClockRun{steps: steps, violated: true})
	}
	return runs
}

func writeWallClockCampaign(t *testing.T, directory, name string, budget int, runs []wallClockRun) string {
	t.Helper()
	seeds := make([]int, 0, len(runs))
	records := make([]map[string]any, 0, len(runs))
	for index, run := range runs {
		seed := index + 1
		seeds = append(seeds, seed)
		record := map[string]any{
			"seed": seed, "exit_code": 0, "steps": run.steps, "actions": run.steps,
			"monotonic_millis": 60_000,
		}
		if run.violated {
			record["first_violation_origin_step"] = run.steps
			record["violated_properties"] = []string{"plantedProperty"}
		}
		records = append(records, record)
	}
	writeCampaign(t, directory, map[string]any{
		"arm": name, "generator": "seeded", "platform": "android",
		"max_steps": budget, "seeds": seeds,
	}, records)
	return directory
}

func wallClockArms(t *testing.T, early, late []wallClockRun) (string, string) {
	t.Helper()
	root := t.TempDir()
	return writeWallClockCampaign(t, filepath.Join(root, "early"), "early", 400, early),
		writeWallClockCampaign(t, filepath.Join(root, "late"), "late", 400, late)
}

// Twenty runs stopped clean at step 12 against twenty violations at step 100.
// Nothing in the first arm was observed past step 12, so no pair of runs across
// the arms has a determined order and there is no difference to report.
func TestRun_RunsStoppedBeforeEveryEventCarryNoComparison(t *testing.T) {
	earlyDirectory, lateDirectory := wallClockArms(t, stoppedShort(20, 12), violatedAt(20, 100))
	pair := analyseCampaigns(t, earlyDirectory, lateDirectory).Pairwise[0]

	if pair.First != "early" || pair.Second != "late" {
		t.Fatalf("comparison %s vs %s, want early vs late", pair.First, pair.Second)
	}
	if math.Abs(pair.A12-0.5) > 1e-12 {
		t.Errorf("a12 %.4f between an arm censored at 12 and one violating at 100, want 0.5: "+
			"a run that stopped at step 12 never reached step 100", pair.A12)
	}
	if pair.PValue < 0.05 {
		t.Errorf("p %.3e, want no significant difference: the arms were never observed over the same steps",
			pair.PValue)
	}
}

// Where the two arms were observed together, over the first twelve steps, the
// arm the wall clock stopped is the one that did not violate. The effect size
// has to follow that and not the step counts the flattening reads.
func TestRun_EffectSizeFollowsWhatCensoringDetermines(t *testing.T) {
	late := append(violatedAt(6, 5), violatedAt(14, 100)...)
	earlyDirectory, lateDirectory := wallClockArms(t, stoppedShort(20, 12), late)
	pair := analyseCampaigns(t, earlyDirectory, lateDirectory).Pairwise[0]

	if pair.A12 <= 0.5 {
		t.Errorf("a12 %.4f, want above 0.5: six of the late arm's runs violated by step 5 "+
			"and none of the early arm's twenty had violated by step 12", pair.A12)
	}
}
