package main

import (
	"math"
	"testing"
)

// The two-sided sign test is R's binom.test(k, n) at p = 0.5, which is the
// doubled tail of a symmetric binomial and can be worked out by hand from the
// coefficients: 2 * sum(C(n, i), i <= min(k, n-k)) / 2^n.
func TestSignTest_MatchesTheBinomialTail(t *testing.T) {
	cases := []struct {
		first, second int
		want          float64
	}{
		{0, 10, 2.0 / 1024},
		{1, 9, 2 * 11.0 / 1024},
		{3, 7, 2 * 176.0 / 1024},
		{5, 5, 1},
		{0, 1, 1},
		{2, 0, 0.5},
	}
	for _, test := range cases {
		got := signTest(test.first, test.second)
		if math.Abs(got-test.want) > 1e-12 {
			t.Errorf("sign test on %d against %d gives %v, want %v", test.first, test.second, got, test.want)
		}
		if reversed := signTest(test.second, test.first); math.Abs(reversed-got) > 1e-12 {
			t.Errorf("sign test on %d against %d gives %v reversed and %v forward",
				test.first, test.second, reversed, got)
		}
	}
}

// A campaign runs tens of seeds, not tens of thousands, but the tail is summed
// through log-gamma rather than through factorials so that a lopsided family
// stays a number rather than becoming an overflow.
func TestSignTest_LargeCountsStayFinite(t *testing.T) {
	if got := signTest(0, 200); got <= 0 || got > 1e-59 {
		t.Errorf("sign test on 0 against 200 gives %v, want a positive value around 2^-199", got)
	}
	if got := signTest(100, 100); math.Abs(got-1) > 1e-12 {
		t.Errorf("sign test on an even split gives %v, want 1", got)
	}
}

func TestSignTest_NoOrderedPairHasNoTest(t *testing.T) {
	if got := signTest(0, 0); !math.IsNaN(got) {
		t.Errorf("sign test with nothing to test gives %v, want undefined", got)
	}
}

func TestPairArms_ScoresEachPairByWhichRunOutlivedTheOther(t *testing.T) {
	pre := arm{Name: "pre", Budget: 40, Runs: []classifiedRun{
		violatingRun(1, 30, 30, "doubleTapCharges"),
		cleanRun(2, 40),
		violatingRun(3, 25, 25, "doubleTapCharges"),
	}}
	post := arm{Name: "post", Budget: 40, Runs: []classifiedRun{
		violatingRun(1, 10, 10, "doubleTapCharges"),
		violatingRun(2, 12, 12, "doubleTapCharges"),
		violatingRun(3, 25, 25, "doubleTapCharges"),
	}}

	comparison, err := pairArms(pre, post)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Pairs != 3 {
		t.Fatalf("%d pairs, want 3", comparison.Pairs)
	}
	// Seed 1 violated at 30 against 10 and seed 2 was still clean at 40 when its
	// partner violated at 12, so both go to the second arm; seed 3 violated on
	// the same step in both and has no order.
	if comparison.SecondSooner != 2 || comparison.FirstSooner != 0 || comparison.Unordered != 1 {
		t.Errorf("counts %+v, want two favouring the second arm and one unordered", comparison)
	}
	if comparison.Sign != 1 {
		t.Errorf("sign %d, want +1 for the arm that violated later", comparison.Sign)
	}
	if math.Abs(comparison.A12-2.5/3) > 1e-12 {
		t.Errorf("a12 within pairs %v, want %v", comparison.A12, 2.5/3)
	}
	// Only seeds 1 and 3 have a difference in steps to take a median of, 20 and
	// 0: the pair holding a clean run has no difference either arm supports.
	if comparison.BothViolated != 2 || comparison.MedianDifference == nil || *comparison.MedianDifference != 10 {
		t.Errorf("median difference %v over %d pair(s), want 10 over 2",
			comparison.MedianDifference, comparison.BothViolated)
	}
	if want := signTest(0, 2); comparison.PValue == nil || *comparison.PValue != want {
		t.Errorf("p %v, want the sign test's %v over the two ordered pairs", comparison.PValue, want)
	}
}

// Two clean runs are two runs that were still going when they stopped, whatever
// step each stopped on, so the pair says nothing and is not a trial.
func TestPairArms_PairsOfCleanRunsAreNotEvidence(t *testing.T) {
	early := arm{Name: "early", Budget: 400, Runs: []classifiedRun{cleanRun(1, 12), cleanRun(2, 14)}}
	late := arm{Name: "late", Budget: 400, Runs: []classifiedRun{cleanRun(1, 400), cleanRun(2, 380)}}

	comparison, err := pairArms(early, late)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Unordered != 2 || comparison.Sign != 0 {
		t.Errorf("comparison %+v, want both pairs unordered and no direction", comparison)
	}
	if comparison.PValue != nil {
		t.Errorf("p %v, want undefined with no ordered pair", *comparison.PValue)
	}
	if comparison.MedianDifference != nil {
		t.Errorf("median difference %v, want undefined where no pair has two violations",
			*comparison.MedianDifference)
	}
	if comparison.A12 != 0.5 {
		t.Errorf("a12 within pairs %v, want 0.5", comparison.A12)
	}
}

// A run excluded as missing data cannot be paired against anything, and the
// seed it came from has to be named rather than silently shrinking the sample.
func TestPairArms_NamesSeedsUsableInOneArmOnly(t *testing.T) {
	pre := arm{Name: "pre", Budget: 40, Runs: []classifiedRun{
		violatingRun(1, 30, 30, "p"),
		{Seed: 2, ExcludedBecause: reasonTimedOut},
		violatingRun(3, 20, 20, "p"),
	}}
	post := arm{Name: "post", Budget: 40, Runs: []classifiedRun{
		violatingRun(1, 10, 10, "p"),
		violatingRun(2, 11, 11, "p"),
	}}

	comparison, err := pairArms(pre, post)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Pairs != 1 {
		t.Fatalf("%d pairs, want 1", comparison.Pairs)
	}
	if len(comparison.UnpairedSeeds) != 2 || comparison.UnpairedSeeds[0] != 2 || comparison.UnpairedSeeds[1] != 3 {
		t.Errorf("unpaired seeds %v, want [2 3]", comparison.UnpairedSeeds)
	}
}

func TestPairArms_RefusesTwoUsableRunsForOneSeed(t *testing.T) {
	pooled := arm{Name: "pre", Budget: 40, Runs: []classifiedRun{
		violatingRun(1, 30, 30, "p"),
		violatingRun(1, 12, 12, "p"),
	}}
	post := arm{Name: "post", Budget: 40, Runs: []classifiedRun{violatingRun(1, 10, 10, "p")}}

	if _, err := pairArms(pooled, post); err == nil {
		t.Fatal("paired two arms where one seed ran twice")
	}
}

// The paired comparison is the ablation's decision rule, so the direction it
// reports has to survive the arms being passed the other way round.
func TestPairArms_DirectionReversesWithTheArms(t *testing.T) {
	pre := arm{Name: "pre", Budget: 400, Runs: []classifiedRun{
		cleanRun(1, 400), cleanRun(2, 400), violatingRun(3, 380, 380, "p"),
		cleanRun(4, 400), violatingRun(5, 350, 350, "p"),
	}}
	post := arm{Name: "post", Budget: 400, Runs: []classifiedRun{
		violatingRun(1, 40, 40, "p"), violatingRun(2, 90, 90, "p"), violatingRun(3, 60, 60, "p"),
		violatingRun(4, 120, 120, "p"), violatingRun(5, 30, 30, "p"),
	}}

	forward, err := pairArms(pre, post)
	if err != nil {
		t.Fatal(err)
	}
	reversed, err := pairArms(post, pre)
	if err != nil {
		t.Fatal(err)
	}
	if forward.Sign != 1 || reversed.Sign != -1 {
		t.Errorf("signs %+d and %+d, want +1 then -1", forward.Sign, reversed.Sign)
	}
	if *forward.MedianDifference != -*reversed.MedianDifference {
		t.Errorf("median differences %v and %v, want opposites",
			*forward.MedianDifference, *reversed.MedianDifference)
	}
	if math.Abs(*forward.PValue-*reversed.PValue) > 1e-12 {
		t.Errorf("p-values %v and %v, want the same two-sided value", *forward.PValue, *reversed.PValue)
	}
	if math.Abs(forward.A12+reversed.A12-1) > 1e-12 {
		t.Errorf("a12 %v and %v, want them to sum to 1", forward.A12, reversed.A12)
	}
}
