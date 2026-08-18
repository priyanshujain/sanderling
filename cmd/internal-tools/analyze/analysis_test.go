package main

import (
	"io"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func violatingRun(seed int64, steps, origin int, properties ...string) classifiedRun {
	return classifiedRun{
		Seed:               seed,
		Steps:              steps,
		Actions:            steps,
		MonotonicMillis:    60_000,
		OriginStep:         origin,
		EventStep:          origin,
		Violated:           true,
		ViolatedProperties: properties,
	}
}

func cleanRun(seed int64, steps int) classifiedRun {
	return classifiedRun{Seed: seed, Steps: steps, Actions: steps, MonotonicMillis: 60_000}
}

func TestSummarize_ArmWhereNoRunViolated(t *testing.T) {
	summary := summarize(arm{
		Name:   "quiet",
		Budget: 40,
		Runs:   []classifiedRun{cleanRun(1, 40), cleanRun(2, 40), cleanRun(3, 40)},
	})
	if summary.Usable != 3 || summary.Censored != 3 || summary.Violated != 0 {
		t.Errorf("summary %+v, want three censored runs", summary)
	}
	if summary.MedianStepsToFirstViolation != nil {
		t.Errorf("median %v, want undefined", *summary.MedianStepsToFirstViolation)
	}
	if summary.ViolationRate == nil || *summary.ViolationRate != 0 {
		t.Errorf("violation rate %v, want 0", summary.ViolationRate)
	}
	if summary.DistinctDefects != 0 || summary.SingletonFraction != nil {
		t.Errorf("defects %d singleton fraction %v, want none", summary.DistinctDefects, summary.SingletonFraction)
	}
}

func TestSummarize_ArmWhereEveryRunViolated(t *testing.T) {
	summary := summarize(arm{
		Name:   "loud",
		Budget: 40,
		Runs: []classifiedRun{
			violatingRun(1, 5, 5, "cartTotal"),
			violatingRun(2, 9, 9, "cartTotal"),
			violatingRun(3, 11, 11, "cartTotal", "backNavigation"),
		},
	})
	if summary.Violated != 3 || summary.Censored != 0 {
		t.Errorf("summary %+v, want three events", summary)
	}
	if summary.MedianStepsToFirstViolation == nil || *summary.MedianStepsToFirstViolation != 9 {
		t.Errorf("median %v, want 9", summary.MedianStepsToFirstViolation)
	}
	if *summary.ViolationRate != 1 {
		t.Errorf("violation rate %v, want 1", *summary.ViolationRate)
	}
	if summary.Detections != 4 || summary.DistinctDefects != 2 {
		t.Errorf("detections %d distinct %d, want 4 and 2", summary.Detections, summary.DistinctDefects)
	}
	// backNavigation appears in one run of three, cartTotal in all three.
	if summary.SingletonDefects != 1 || math.Abs(*summary.SingletonFraction-0.5) > 1e-12 {
		t.Errorf("singletons %d fraction %v, want 1 and 0.5", summary.SingletonDefects, summary.SingletonFraction)
	}
	// 25 actions over 3 minutes.
	if math.Abs(*summary.DefectsPerThousandActions-160) > 1e-9 {
		t.Errorf("defects per thousand actions %v, want 160", *summary.DefectsPerThousandActions)
	}
	if math.Abs(*summary.DefectsPerHour-80) > 1e-9 {
		t.Errorf("defects per hour %v, want 80", *summary.DefectsPerHour)
	}
}

// A step that chose no action, and a step whose action was never dispatched,
// left the app untouched. Counting them would inflate the denominator of every
// per-action rate, and the inflation differs by arm so it does not cancel.
func TestSummarize_CountsDispatchedActionsNotSteps(t *testing.T) {
	summary := summarize(arm{
		Name:   "declines",
		Budget: 40,
		Runs: []classifiedRun{
			{Seed: 1, Steps: 40, Actions: 10, MonotonicMillis: 3_600_000,
				Violated: true, OriginStep: 12, ViolatedProperties: []string{"cartTotal"}},
			{Seed: 2, Steps: 40, Actions: 6, MonotonicMillis: 3_600_000},
		},
	})
	if summary.TotalSteps != 80 {
		t.Errorf("total steps %d, want 80", summary.TotalSteps)
	}
	if summary.TotalActions != 16 {
		t.Errorf("total actions %d, want 16 dispatched of 80 steps", summary.TotalActions)
	}
	if summary.DefectsPerThousandActions == nil {
		t.Fatal("no defects per thousand actions")
	}
	expected := 1000.0 / 16.0
	if math.Abs(*summary.DefectsPerThousandActions-expected) > 1e-9 {
		t.Errorf("defects per thousand actions %v, want %v", *summary.DefectsPerThousandActions, expected)
	}
}

func TestSummarize_ArmThatDispatchedNothingHasNoPerActionRate(t *testing.T) {
	summary := summarize(arm{
		Name:   "inert",
		Budget: 20,
		Runs: []classifiedRun{
			{Seed: 1, Steps: 20, Actions: 0, MonotonicMillis: 3_600_000,
				Violated: true, OriginStep: 3, ViolatedProperties: []string{"cartTotal"}},
		},
	})
	if summary.TotalActions != 0 || summary.TotalSteps != 20 {
		t.Errorf("steps %d actions %d, want 20 and 0", summary.TotalSteps, summary.TotalActions)
	}
	if summary.DefectsPerThousandActions != nil {
		t.Errorf("defects per thousand actions %v, want none with nothing dispatched", *summary.DefectsPerThousandActions)
	}
	if summary.DefectsPerHour == nil || *summary.DefectsPerHour != 1 {
		t.Errorf("defects per hour %v, want 1: the run still consumed an hour", summary.DefectsPerHour)
	}
}

func runHoursFor(t *testing.T, record map[string]any) float64 {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "campaign")
	record["seed"] = 1
	writeCampaign(t, directory,
		map[string]any{"arm": "seeded", "max_steps": 50, "seeds": []int{1}},
		[]map[string]any{record})
	arms, err := groupArms([]string{directory})
	if err != nil {
		t.Fatal(err)
	}
	return summarize(arms[0]).TotalRunHours
}

// A host asleep mid-run advanced the wall clock while testing nothing, so the
// sleep has no place in the denominator of a per-hour rate.
func TestSummarize_RunHoursCountTheTimeWorkedNotTheTimeThatPassed(t *testing.T) {
	hours := runHoursFor(t, map[string]any{
		"exit_code": 0, "steps": 50, "actions": 50,
		"monotonic_millis": 3_600_000, "wall_clock_millis": 5_400_000,
	})
	if hours != 1 {
		t.Errorf("run hours %v, want the 1 hour worked rather than the 1.5 hours that passed", hours)
	}
}

// Campaigns recorded before the two clocks were split gave the same monotonic
// reading the name duration_millis, and their hours still have to count.
func TestSummarize_RunHoursReadCampaignsWrittenBeforeTheClocksWereSplit(t *testing.T) {
	hours := runHoursFor(t, map[string]any{
		"exit_code": 0, "steps": 50, "actions": 50, "duration_millis": 3_600_000,
	})
	if hours != 1 {
		t.Errorf("run hours %v, want 1 from the older duration_millis field", hours)
	}
}

func TestSummarize_ArmWithNoUsableRunsAfterExclusions(t *testing.T) {
	summary := summarize(arm{
		Name:   "broken",
		Budget: 40,
		Runs: []classifiedRun{
			{Seed: 1, ExcludedBecause: reasonTimedOut},
			{Seed: 2, ExcludedBecause: reasonNonzeroExit},
			{Seed: 3, ExcludedBecause: reasonNonzeroExit},
		},
	})
	if summary.Usable != 0 || summary.Excluded != 3 {
		t.Errorf("summary %+v, want no usable runs and three exclusions", summary)
	}
	if summary.ExcludedByReason[reasonNonzeroExit] != 2 || summary.ExcludedByReason[reasonTimedOut] != 1 {
		t.Errorf("exclusions %v", summary.ExcludedByReason)
	}
	if summary.ViolationRate != nil || summary.MedianStepsToFirstViolation != nil {
		t.Error("reported a rate or a median for an arm with nothing in it")
	}
	if summary.DefectsPerThousandActions != nil || summary.DefectsPerHour != nil {
		t.Error("reported a yield rate with no actions and no time")
	}
	if len(summary.SurvivalCurve) != 0 {
		t.Errorf("survival curve %v, want empty", summary.SurvivalCurve)
	}
}

func TestSummarize_SingleRunArm(t *testing.T) {
	summary := summarize(arm{Name: "one", Budget: 40, Runs: []classifiedRun{violatingRun(1, 6, 6, "cartTotal")}})
	if summary.Usable != 1 || summary.Violated != 1 {
		t.Errorf("summary %+v", summary)
	}
	if summary.MedianStepsToFirstViolation == nil || *summary.MedianStepsToFirstViolation != 6 {
		t.Errorf("median %v, want 6", summary.MedianStepsToFirstViolation)
	}
	if summary.SingletonDefects != 1 || *summary.SingletonFraction != 1 {
		t.Errorf("singletons %d fraction %v, want 1 and 1", summary.SingletonDefects, summary.SingletonFraction)
	}
}

// Excluded runs must not reach the survival data at all, and the counts must
// keep them visible.
func TestAnalyse_ExcludedRunsNeverBecomeObservations(t *testing.T) {
	current := arm{
		Name:   "mixed",
		Budget: 30,
		Runs: []classifiedRun{
			violatingRun(1, 8, 8, "cartTotal"),
			cleanRun(2, 30),
			{Seed: 3, ExcludedBecause: reasonTimedOut},
		},
	}
	observations := current.observations()
	if len(observations) != 2 {
		t.Fatalf("%d observations, want 2", len(observations))
	}
	summary := summarize(current)
	if summary.Usable != 2 || summary.Excluded != 1 || summary.Violated != 1 || summary.Censored != 1 {
		t.Errorf("summary %+v", summary)
	}
}

func TestAnalyse_ArmWithNoUsableRunsIsReportedButNotTested(t *testing.T) {
	result, err := analyse([]arm{
		{Name: "a", Budget: 30, Runs: []classifiedRun{violatingRun(1, 4, 4), violatingRun(2, 6, 6)}},
		{Name: "b", Budget: 30, Runs: []classifiedRun{cleanRun(1, 30), cleanRun(2, 30)}},
		{Name: "c", Budget: 30, Runs: []classifiedRun{{Seed: 1, ExcludedBecause: reasonNonzeroExit}}},
	}, time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Arms) != 3 {
		t.Fatalf("%d arms reported, want all 3", len(result.Arms))
	}
	if result.LogRank == nil || len(result.LogRank.Groups) != 2 {
		t.Fatalf("log-rank %+v, want the two testable arms", result.LogRank)
	}
	if len(result.Pairwise) != 1 {
		t.Fatalf("%d comparisons, want 1", len(result.Pairwise))
	}
	if len(result.Notes) == 0 {
		t.Error("no note explaining the dropped arm")
	}
}

// With a single testable arm there is nothing to compare against, and the tool
// must say so instead of producing a statistic.
func TestAnalyse_SingleArmHasNoTests(t *testing.T) {
	result, err := analyse([]arm{
		{Name: "a", Budget: 30, Runs: []classifiedRun{violatingRun(1, 4, 4)}},
	}, time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if result.LogRank != nil || len(result.Pairwise) != 0 {
		t.Errorf("log-rank %+v pairwise %v, want neither", result.LogRank, result.Pairwise)
	}
}

// Holm is applied within the family of pairwise comparisons, so with three arms
// the smallest raw p-value is multiplied by three.
func TestComparePairs_AppliesHolmWithinTheFamily(t *testing.T) {
	arms := []arm{
		{Name: "a", Budget: 40, Runs: manyRuns(12, 4)},
		{Name: "b", Budget: 40, Runs: manyRuns(12, 20)},
		{Name: "c", Budget: 40, Runs: manyRuns(12, 36)},
	}
	pairs := comparePairs(arms)
	if len(pairs) != 3 {
		t.Fatalf("%d comparisons, want 3", len(pairs))
	}
	raw := make([]float64, len(pairs))
	for index, pair := range pairs {
		raw[index] = pair.PValue
		if pair.HolmPValue < pair.PValue-1e-12 {
			t.Errorf("%s vs %s: holm p %v below raw p %v", pair.First, pair.Second, pair.HolmPValue, pair.PValue)
		}
	}
	expected := holm(raw)
	for index, pair := range pairs {
		if math.Abs(pair.HolmPValue-expected[index]) > 1e-12 {
			t.Errorf("comparison %d holm p %v, want %v", index, pair.HolmPValue, expected[index])
		}
	}
}

// a12 above one half means the first arm needed more steps before its first
// violation, so the arm that finds defects sooner sits below one half.
func TestComparePairs_A12DirectionFollowsStepCounts(t *testing.T) {
	pairs := comparePairs([]arm{
		{Name: "slow", Budget: 40, Runs: manyRuns(6, 30)},
		{Name: "fast", Budget: 40, Runs: manyRuns(6, 4)},
	})
	if pairs[0].A12 <= 0.5 {
		t.Errorf("a12 %v for the slower arm listed first, want above 0.5", pairs[0].A12)
	}
}

func writeCleanCampaign(t *testing.T, directory, name string, budget, steps, runs int) {
	t.Helper()
	seeds := make([]int, 0, runs)
	records := make([]map[string]any, 0, runs)
	for seed := 1; seed <= runs; seed++ {
		seeds = append(seeds, seed)
		records = append(records, map[string]any{
			"seed": seed, "exit_code": 0, "steps": steps, "actions": steps, "monotonic_millis": 60_000,
		})
	}
	writeCampaign(t, directory, map[string]any{"arm": name, "max_steps": budget, "seeds": seeds}, records)
}

// Arms censored at different budgets are not on the same clock: every clean run
// of the wider arm outranks every clean run of the narrower one whatever the
// app did, so the rank-sum and the paired test reach a foregone conclusion the
// log-rank in the same report contradicts. groupArms already refuses this
// within one arm, and comparing across arms is the same hazard.
func TestRun_RefusesToCompareArmsCensoredAtDifferentBudgets(t *testing.T) {
	cases := []struct {
		name      string
		wideSteps int
		arguments []string
	}{
		{name: "identical runs under different budgets", wideSteps: 100},
		{name: "each arm run to its own budget", wideSteps: 400},
		{name: "paired", wideSteps: 400, arguments: []string{"--paired"}},
	}
	for _, test := range cases {
		root := t.TempDir()
		wide := filepath.Join(root, "wide")
		narrow := filepath.Join(root, "narrow")
		writeCleanCampaign(t, wide, "wide", 400, test.wideSteps, 30)
		writeCleanCampaign(t, narrow, "narrow", 100, 100, 30)

		err := run(append(test.arguments, wide, narrow), io.Discard, io.Discard)
		if err == nil {
			t.Fatalf("%s: arms censored at 400 and at 100 steps were compared without complaint", test.name)
		}
		for _, fragment := range []string{"wide", "400", "narrow", "100", "different budgets"} {
			if !strings.Contains(err.Error(), fragment) {
				t.Errorf("%s: error %q is missing %q", test.name, err, fragment)
			}
		}
	}
}

func writeSourcedCampaign(t *testing.T, directory, name string, steps, unattributed int) {
	t.Helper()
	const budget, runs, actions = 40, 6, 20
	seeds := make([]int, 0, runs)
	records := make([]map[string]any, 0, runs)
	for seed := 1; seed <= runs; seed++ {
		seeds = append(seeds, seed)
		records = append(records, map[string]any{
			"seed": seed, "exit_code": 0, "steps": steps, "actions": actions,
			"monotonic_millis": 60_000, "unattributed_actions": unattributed,
		})
	}
	writeCampaign(t, directory, map[string]any{"arm": name, "max_steps": budget, "seeds": seeds}, records)
}

// An arm whose actions name no producer counts whatever the spec's setup
// dispatched in the denominator of every per-action rate, and an arm whose
// actions name one leaves the login out of it. The two denominators measure
// different things, so a test that ranks one arm against the other reads a
// difference in what was counted as a difference in what the arms found.
func TestRun_RefusesToCompareArmsWhoseActionsWereCountedDifferently(t *testing.T) {
	cases := []struct {
		name      string
		arguments []string
	}{
		{name: "independent samples"},
		{name: "paired", arguments: []string{"--paired"}},
	}
	for _, test := range cases {
		root := t.TempDir()
		attributed := filepath.Join(root, "attributed")
		legacy := filepath.Join(root, "legacy")
		writeSourcedCampaign(t, attributed, "attributed", 40, 0)
		writeSourcedCampaign(t, legacy, "legacy", 30, 20)

		err := run(append(test.arguments, attributed, legacy), io.Discard, io.Discard)
		if err == nil {
			t.Fatalf("%s: an arm of unknown provenance was tested against an attributed one without complaint", test.name)
		}
		for _, fragment := range []string{"attributed", "legacy", "120", "unknown provenance"} {
			if !strings.Contains(err.Error(), fragment) {
				t.Errorf("%s: error %q is missing %q", test.name, err, fragment)
			}
		}
	}
}

// Two arms recorded before actions named their producer are on the same
// denominator as each other, diluted the same way, so they compare.
func TestRun_ComparesTwoArmsThatBothNameNoProducer(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	writeSourcedCampaign(t, first, "first", 40, 20)
	writeSourcedCampaign(t, second, "second", 30, 20)

	if err := run([]string{first, second}, io.Discard, io.Discard); err != nil {
		t.Fatalf("two arms of the same unknown provenance were refused: %v", err)
	}
}

func manyRuns(count, originStep int) []classifiedRun {
	runs := make([]classifiedRun, 0, count)
	for index := 0; index < count; index++ {
		runs = append(runs, violatingRun(int64(index), originStep+index, originStep+index, "cartTotal"))
	}
	return runs
}
