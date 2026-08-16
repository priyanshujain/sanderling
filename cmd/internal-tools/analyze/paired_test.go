package main

import (
	"math"
	"testing"
)

// Hollander and Wolfe (1973), 29f: Hamilton depression scale factor
// measurements on nine patients, first at admission and again after tranquilizer
// treatment. R's wilcox.test help page uses exactly these vectors as its paired
// example and reports
//
//	wilcox.test(x, y, paired = TRUE, alternative = "greater")
//	## V = 40, p-value = 0.01953
var (
	depressionAtAdmission  = []float64{1.83, 0.50, 1.62, 2.48, 1.68, 1.88, 1.55, 3.06, 1.30}
	depressionAfterOneWeek = []float64{0.878, 0.647, 0.598, 2.05, 1.06, 1.29, 1.06, 3.14, 1.29}
)

func differencesOf(first, second []float64) []float64 {
	differences := make([]float64, len(first))
	for index := range first {
		differences[index] = first[index] - second[index]
	}
	return differences
}

func TestSignedRank_MatchesPublishedDepressionResult(t *testing.T) {
	result := signedRank(differencesOf(depressionAtAdmission, depressionAfterOneWeek))

	if result.Statistic != 40 {
		t.Errorf("statistic %v, want 40", result.Statistic)
	}
	if !result.Exact {
		t.Error("expected the exact null distribution for nine untied differences")
	}
	upper := exactSignedRankUpperTail(40, 9)
	if math.Abs(upper-0.01953) > 5e-6 {
		t.Errorf("one-sided p-value %.6f, want 0.01953", upper)
	}
	if math.Abs(result.PValue-2*0.01953125) > 1e-9 {
		t.Errorf("two-sided p-value %.6f, want %.6f", result.PValue, 2*0.01953125)
	}
}

// Reversing the pairs mirrors the statistic about n(n+1)/2 and leaves the
// two-sided p-value alone, which R reports as V = 5 on the same data.
func TestSignedRank_ReversedPairsMirrorTheStatistic(t *testing.T) {
	forward := signedRank(differencesOf(depressionAtAdmission, depressionAfterOneWeek))
	reversed := signedRank(differencesOf(depressionAfterOneWeek, depressionAtAdmission))
	if reversed.Statistic != 5 {
		t.Errorf("reversed statistic %v, want 5", reversed.Statistic)
	}
	if math.Abs(reversed.PValue-forward.PValue) > 1e-12 {
		t.Errorf("reversed p-value %v, want %v", reversed.PValue, forward.PValue)
	}
}

// The exact null distribution must be a proper distribution: 2^n sign
// assignments in total, symmetric about n(n+1)/4.
func TestExactSignedRankCounts_FormAProperSymmetricDistribution(t *testing.T) {
	counts := exactSignedRankCounts(8)
	total := 0.0
	for _, count := range counts {
		total += count
	}
	if total != 256 {
		t.Errorf("counts sum to %v, want 2^8 = 256", total)
	}
	for index := range counts {
		if counts[index] != counts[len(counts)-1-index] {
			t.Errorf("count at %d is %v but %v at the mirrored point", index, counts[index], counts[len(counts)-1-index])
		}
	}
}

// The tie-corrected variance is checked against the exact permutation variance
// of the statistic, computed here by enumerating every sign assignment over the
// observed midranks. That is an independent calculation rather than a second
// call into the implementation under test.
func TestSignedRankVariance_MatchesExactPermutationVariance(t *testing.T) {
	cases := [][]float64{
		{1, -2, 3, -4, 5, 6, -7, 8},
		{12, -12, 12, 12, -5, 5, 30, -30},
		{-400, 400, 400, -400, 400, 400, 400, 400},
		{3, 3, 3, -3, -3, 7, 7, 9, 9},
	}
	for _, differences := range cases {
		magnitudes := make([]float64, len(differences))
		for index, difference := range differences {
			magnitudes[index] = math.Abs(difference)
		}
		ranks, tieGroups := midRanks(magnitudes)
		mean, variance := permutationMomentsOfSignedRank(ranks)
		size := float64(len(ranks))
		if expected := size * (size + 1) / 4; math.Abs(mean-expected) > 1e-9 {
			t.Errorf("permutation mean %v for %v, want %v", mean, differences, expected)
		}
		if got := signedRankVariance(len(ranks), tieGroups); math.Abs(got-variance) > 1e-9 {
			t.Errorf("variance %v for %v, want the permutation variance %v", got, differences, variance)
		}
	}
}

// With ties present the normal approximation is the only branch available, so
// it is checked against the exact permutation p-value of the same statistic on
// the same data.
func TestSignedRank_TiedDifferencesTrackTheExactPermutationPValue(t *testing.T) {
	cases := [][]float64{
		{40, 40, 40, -12, 33, 40, 40, -3, 40, 21, 40, 40},
		{-5, -5, -5, -5, 9, 9, 2, 2, -1, -1, 40, 40},
		{40, 40, -12, 33, 40, -3, 21, 40, 15, -9},
	}
	for _, differences := range cases {
		result := signedRank(differences)
		if result.Exact {
			t.Errorf("%v used the exact null distribution despite ties", differences)
		}
		exact := permutationSignedRankTwoSided(differences)
		if math.Abs(result.PValue-exact) > 0.03 {
			t.Errorf("normal approximation p %.4f for %v, want near the permutation p %.4f",
				result.PValue, differences, exact)
		}
	}
}

// Every difference the same size is the degenerate end of the tie correction,
// and the normal approximation is genuinely poor there: the exact randomization
// p-value on this sample is 0.3438 against the approximation's 0.2273. The tool
// keeps R's formula rather than the randomization p-value so that a reviewer
// running wilcox.test on the same differences reads the same number, and the
// expected value here is that published formula worked through by hand:
//
//	n = 10, one tied group of 10, V = 7 * 5.5 = 38.5
//	mean     = 10 * 11 / 4 = 27.5
//	variance = 10 * 11 * 21 / 24 - (10^3 - 10) / 48 = 96.25 - 20.625 = 75.625
//	z        = (38.5 - 27.5 - 0.5) / sqrt(75.625)
func TestSignedRank_EveryDifferenceTheSameSizeFollowsTheDocumentedFormula(t *testing.T) {
	differences := []float64{7, 7, 7, 7, 7, 7, 7, -7, -7, -7}
	result := signedRank(differences)

	if result.Statistic != 38.5 {
		t.Errorf("statistic %v, want 38.5", result.Statistic)
	}
	if got := signedRankVariance(10, []int{10}); math.Abs(got-75.625) > 1e-12 {
		t.Errorf("variance %v, want 75.625", got)
	}
	expected := 2 * standardNormalUpperTail(10.5/math.Sqrt(75.625))
	if math.Abs(result.PValue-expected) > 1e-12 {
		t.Errorf("p-value %v, want %v", result.PValue, expected)
	}
	if randomization := permutationSignedRankTwoSided(differences); math.Abs(randomization-0.3438) > 5e-4 {
		t.Errorf("randomization p-value %.4f, want 0.3438", randomization)
	}
}

// R drops zero differences before ranking and tests what is left, so a pair
// where both arms took the same number of steps carries no direction and must
// not be ranked as though it did.
func TestSignedRank_ZeroDifferencesAreDropped(t *testing.T) {
	result := signedRank([]float64{0, 0, 3, -1, 2})
	if result.Pairs != 5 || result.NonZero != 3 {
		t.Errorf("pairs %d non-zero %d, want 5 and 3", result.Pairs, result.NonZero)
	}
	// Ranks over |{3, 1, 2}| are 3, 1, 2, and the positive differences hold 3
	// and 2.
	if result.Statistic != 5 {
		t.Errorf("statistic %v, want 5", result.Statistic)
	}
	if result.Exact {
		t.Error("used the exact null distribution despite dropped zeros")
	}
}

func TestSignedRank_EveryDifferenceZero(t *testing.T) {
	result := signedRank([]float64{0, 0, 0})
	if result.NonZero != 0 {
		t.Errorf("non-zero pairs %d, want 0", result.NonZero)
	}
	if !math.IsNaN(result.PValue) || !math.IsNaN(result.Statistic) {
		t.Errorf("result %+v, want everything undefined", result)
	}
}

// permutationMomentsOfSignedRank enumerates every sign assignment and returns
// the mean and variance of the statistic over them.
func permutationMomentsOfSignedRank(ranks []float64) (float64, float64) {
	values := signedRankPermutationValues(ranks)
	mean := 0.0
	for _, value := range values {
		mean += value
	}
	mean /= float64(len(values))
	variance := 0.0
	for _, value := range values {
		variance += (value - mean) * (value - mean)
	}
	return mean, variance / float64(len(values))
}

// permutationSignedRankTwoSided is the exact randomization p-value: the
// proportion of sign assignments whose statistic is at least as far from the
// null mean as the observed one.
func permutationSignedRankTwoSided(differences []float64) float64 {
	magnitudes := make([]float64, 0, len(differences))
	observed := 0.0
	for _, difference := range differences {
		if difference == 0 {
			continue
		}
		magnitudes = append(magnitudes, math.Abs(difference))
	}
	ranks, _ := midRanks(magnitudes)
	position := 0
	for _, difference := range differences {
		if difference == 0 {
			continue
		}
		if difference > 0 {
			observed += ranks[position]
		}
		position++
	}
	size := float64(len(ranks))
	mean := size * (size + 1) / 4
	values := signedRankPermutationValues(ranks)
	extreme := 0
	for _, value := range values {
		if math.Abs(value-mean) >= math.Abs(observed-mean)-1e-9 {
			extreme++
		}
	}
	return float64(extreme) / float64(len(values))
}

func signedRankPermutationValues(ranks []float64) []float64 {
	values := make([]float64, 0, 1<<len(ranks))
	for assignment := 0; assignment < 1<<len(ranks); assignment++ {
		total := 0.0
		for index, rank := range ranks {
			if assignment&(1<<index) != 0 {
				total += rank
			}
		}
		values = append(values, total)
	}
	return values
}

func TestPairArms_MatchesSeedsAndHoldsCensoredRunsAtTheBudget(t *testing.T) {
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
	// Differences are 30-10, 40-12 and 25-25: the clean run enters at the
	// budget rather than being dropped, and the equal pair is a tie.
	if comparison.MedianDifference != 20 {
		t.Errorf("median difference %v, want 20", comparison.MedianDifference)
	}
	if comparison.Sign != 1 {
		t.Errorf("sign %d, want +1 for the arm that violated later", comparison.Sign)
	}
	if comparison.SecondSooner != 2 || comparison.FirstSooner != 0 || comparison.Tied != 1 {
		t.Errorf("counts %+v, want two favouring the second arm and one tie", comparison)
	}
	// Two pairs of three favour the second arm and one is tied, so the
	// within-pair effect size is (2 + 0.5) / 3.
	if math.Abs(comparison.A12-2.5/3) > 1e-12 {
		t.Errorf("a12 within pairs %v, want %v", comparison.A12, 2.5/3)
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
	if forward.MedianDifference != -reversed.MedianDifference {
		t.Errorf("median differences %v and %v, want opposites", forward.MedianDifference, reversed.MedianDifference)
	}
	if math.Abs(forward.PValue-reversed.PValue) > 1e-12 {
		t.Errorf("p-values %v and %v, want the same two-sided value", forward.PValue, reversed.PValue)
	}
	if math.Abs(forward.A12+reversed.A12-1) > 1e-12 {
		t.Errorf("a12 %v and %v, want them to sum to 1", forward.A12, reversed.A12)
	}
}
