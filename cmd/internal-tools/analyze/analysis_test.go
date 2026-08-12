package main

import (
	"math"
	"testing"
	"time"
)

func violatingRun(seed int64, steps, origin int, properties ...string) classifiedRun {
	return classifiedRun{
		Seed:               seed,
		Steps:              steps,
		DurationMillis:     60_000,
		OriginStep:         origin,
		Violated:           true,
		ViolatedProperties: properties,
	}
}

func cleanRun(seed int64, steps int) classifiedRun {
	return classifiedRun{Seed: seed, Steps: steps, DurationMillis: 60_000}
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
	result := analyse([]arm{
		{Name: "a", Budget: 30, Runs: []classifiedRun{violatingRun(1, 4, 4), violatingRun(2, 6, 6)}},
		{Name: "b", Budget: 30, Runs: []classifiedRun{cleanRun(1, 30), cleanRun(2, 30)}},
		{Name: "c", Budget: 30, Runs: []classifiedRun{{Seed: 1, ExcludedBecause: reasonNonzeroExit}}},
	}, time.Unix(0, 0).UTC())

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
	result := analyse([]arm{
		{Name: "a", Budget: 30, Runs: []classifiedRun{violatingRun(1, 4, 4)}},
	}, time.Unix(0, 0).UTC())
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

func manyRuns(count, originStep int) []classifiedRun {
	runs := make([]classifiedRun, 0, count)
	for index := 0; index < count; index++ {
		runs = append(runs, violatingRun(int64(index), originStep+index, originStep+index, "cartTotal"))
	}
	return runs
}
