package main

import (
	"math"
	"testing"
)

// R's wilcox.test on the Hollander and Wolfe (1973), 69f chorioamnion data
// reports W = 35 with an exact two-sided p-value of 0.2544; the one-sided
// greater alternative that the help page uses reports the same W with
// p-value = 0.1272.
func TestRankSum_MatchesPublishedChorioamnionResult(t *testing.T) {
	result := rankSum(chorioamnionTerm, chorioamnionEarly)

	if result.Statistic != 35 {
		t.Errorf("statistic %v, want 35", result.Statistic)
	}
	if !result.Exact {
		t.Error("expected the exact null distribution for untied samples this small")
	}
	if math.Abs(result.PValue-0.2544) > 5e-5 {
		t.Errorf("two-sided p-value %.6f, want 0.2544", result.PValue)
	}
	upper := exactRankSumUpperTail(35, len(chorioamnionTerm), len(chorioamnionEarly))
	if math.Abs(upper-0.1272) > 5e-5 {
		t.Errorf("one-sided p-value %.6f, want 0.1272", upper)
	}
}

// A12 is P(X > Y) + 0.5 P(X = Y), which is U/(mn). With the published W = 35
// and sample sizes 10 and 5 the effect size is 35/50 = 0.70. The expected value
// is therefore the published Mann-Whitney statistic combined with the published
// definition in Vargha and Delaney (2000), not a number this tool produced.
func TestVargaDelaneyA12_MatchesPublishedChorioamnionStatistic(t *testing.T) {
	got := vargaDelaneyA12(chorioamnionTerm, chorioamnionEarly)
	if math.Abs(got-0.70) > 1e-12 {
		t.Errorf("A12 = %v, want 0.70", got)
	}
	if reversed := vargaDelaneyA12(chorioamnionEarly, chorioamnionTerm); math.Abs(reversed-0.30) > 1e-12 {
		t.Errorf("reversed A12 = %v, want 0.30", reversed)
	}
}

// Identical samples are stochastically equal, which Vargha and Delaney define
// as A = 0.5, and complete separation gives 1 and 0.
func TestVargaDelaneyA12_BoundaryCases(t *testing.T) {
	same := []float64{1, 2, 3, 4}
	if got := vargaDelaneyA12(same, same); got != 0.5 {
		t.Errorf("A12 of a sample against itself = %v, want 0.5", got)
	}
	if got := vargaDelaneyA12([]float64{5, 6, 7}, []float64{1, 2}); got != 1 {
		t.Errorf("A12 with complete dominance = %v, want 1", got)
	}
	if got := vargaDelaneyA12([]float64{1, 2}, []float64{5, 6, 7}); got != 0 {
		t.Errorf("A12 with complete subordination = %v, want 0", got)
	}
}

// The counting definition and the rank-sum route must agree, including when the
// samples are tied against each other, which is the case the evaluation data is
// always in because censored runs are all held at the budget.
func TestVargaDelaneyA12_AgreesWithRankSumStatistic(t *testing.T) {
	cases := [][2][]float64{
		{{1, 2, 3}, {2, 3, 4}},
		{{40, 40, 40, 12}, {40, 7, 3}},
		{{5}, {5, 5, 5}},
		{{9, 9, 9}, {9, 9, 9}},
	}
	for _, test := range cases {
		result := rankSum(test[0], test[1])
		expected := result.Statistic / float64(len(test[0])*len(test[1]))
		if math.Abs(result.A12-expected) > 1e-12 {
			t.Errorf("A12 %v for %v vs %v, want U/(mn) = %v", result.A12, test[0], test[1], expected)
		}
	}
}

// The tie-corrected variance is checked against the exact permutation variance
// of the statistic, computed here by enumerating every way to split the pooled
// midranks. That is an independent calculation, not a second call into the
// implementation under test.
func TestRankSumVariance_MatchesExactPermutationVariance(t *testing.T) {
	cases := [][]float64{
		{1, 2, 3, 4, 5, 6, 7, 8},
		{40, 40, 40, 40, 12, 7, 3, 3},
		{5, 5, 5, 5, 5, 5, 5, 9},
		{2, 2, 3, 3, 3, 4, 9, 9, 9},
	}
	for _, pooled := range cases {
		firstSize := len(pooled) / 2
		ranks, tieGroups := midRanks(pooled)
		mean, variance := permutationMomentsOfRankSum(ranks, firstSize)
		expectedMean := float64(firstSize*(len(pooled)-firstSize)) / 2
		if math.Abs(mean-expectedMean) > 1e-9 {
			t.Errorf("%v: permutation mean %v, want %v", pooled, mean, expectedMean)
		}
		got := rankSumVariance(firstSize, len(pooled)-firstSize, tieGroups)
		if math.Abs(got-variance) > 1e-9 {
			t.Errorf("%v: tie-corrected variance %v, want the permutation variance %v", pooled, got, variance)
		}
	}
}

// permutationMomentsOfRankSum enumerates every subset of the given size and
// returns the mean and variance of the Mann-Whitney statistic over them.
func permutationMomentsOfRankSum(ranks []float64, firstSize int) (float64, float64) {
	offset := float64(firstSize) * float64(firstSize+1) / 2
	var values []float64
	chosen := make([]int, 0, firstSize)
	var walk func(start int)
	walk = func(start int) {
		if len(chosen) == firstSize {
			total := 0.0
			for _, index := range chosen {
				total += ranks[index]
			}
			values = append(values, total-offset)
			return
		}
		for index := start; index < len(ranks); index++ {
			chosen = append(chosen, index)
			walk(index + 1)
			chosen = chosen[:len(chosen)-1]
		}
	}
	walk(0)
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

func TestMidRanks_AveragesTiedGroups(t *testing.T) {
	ranks, tieGroups := midRanks([]float64{3, 1, 3, 2, 3})
	expected := []float64{4, 1, 4, 2, 4}
	for index, want := range expected {
		if ranks[index] != want {
			t.Errorf("rank %d = %v, want %v", index, ranks[index], want)
		}
	}
	if len(tieGroups) != 1 || tieGroups[0] != 3 {
		t.Errorf("tie groups %v, want [3]", tieGroups)
	}
}

func TestRankSum_TiedSamplesUseTheNormalApproximation(t *testing.T) {
	result := rankSum([]float64{1, 2, 3, 4}, []float64{3, 4, 5, 6})
	if result.Exact {
		t.Error("used the exact null distribution despite ties")
	}
	if math.IsNaN(result.PValue) || result.PValue < 0 || result.PValue > 1 {
		t.Errorf("p-value %v", result.PValue)
	}
}

// Every observation identical carries no information, and the test must say so
// rather than dividing by a zero variance.
func TestRankSum_AllValuesIdentical(t *testing.T) {
	result := rankSum([]float64{40, 40, 40}, []float64{40, 40, 40, 40})
	if result.PValue != 1 {
		t.Errorf("p-value %v, want 1", result.PValue)
	}
	if result.A12 != 0.5 {
		t.Errorf("A12 %v, want 0.5", result.A12)
	}
}

func TestRankSum_SingleObservationPerSample(t *testing.T) {
	result := rankSum([]float64{3}, []float64{9})
	if result.Statistic != 0 {
		t.Errorf("statistic %v, want 0", result.Statistic)
	}
	if result.A12 != 0 {
		t.Errorf("A12 %v, want 0", result.A12)
	}
	if math.IsNaN(result.PValue) || result.PValue > 1 {
		t.Errorf("p-value %v", result.PValue)
	}
}

func TestRankSum_EmptySampleHasNoStatistic(t *testing.T) {
	result := rankSum(nil, []float64{1, 2, 3})
	if !math.IsNaN(result.PValue) || !math.IsNaN(result.A12) {
		t.Errorf("result %+v, want everything undefined", result)
	}
}

// The exact null distribution must be a proper distribution: the counts sum to
// the binomial coefficient and the distribution is symmetric about mn/2.
func TestExactRankSumCounts_FormAProperSymmetricDistribution(t *testing.T) {
	counts := exactRankSumCounts(4, 6)
	total := 0.0
	for _, count := range counts {
		total += count
	}
	if total != 210 {
		t.Errorf("counts sum to %v, want C(10,4) = 210", total)
	}
	for index := range counts {
		mirrored := counts[len(counts)-1-index]
		if counts[index] != mirrored {
			t.Errorf("count at %d is %v but %v at the mirrored point", index, counts[index], mirrored)
		}
	}
}
