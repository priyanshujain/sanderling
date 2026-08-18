package main

import (
	"fmt"
	"math"
	"slices"
)

type signedRankResult struct {
	Pairs     int     `json:"pairs"`
	NonZero   int     `json:"non_zero_pairs"`
	Statistic float64 `json:"signed_rank_v"`
	PValue    float64 `json:"p_value"`
	Exact     bool    `json:"exact"`
}

// exactSignedRankLimit matches R's wilcox.test: the exact null distribution is
// used only below this many non-zero differences, and only when nothing is tied.
const exactSignedRankLimit = 50

// signedRank is the two-sided Wilcoxon signed-rank test over paired
// differences. Zero differences are dropped before ranking and the statistic is
// the sum of the ranks carried by the positive differences, which is the
// quantity R's wilcox.test calls V. Wilcoxon (1945), "Individual Comparisons by
// Ranking Methods", Biometrics Bulletin 1(6), 80-83.
func signedRank(differences []float64) signedRankResult {
	result := signedRankResult{
		Pairs:     len(differences),
		Statistic: math.NaN(),
		PValue:    math.NaN(),
	}
	var magnitudes []float64
	var positive []bool
	for _, difference := range differences {
		if difference == 0 {
			continue
		}
		magnitudes = append(magnitudes, math.Abs(difference))
		positive = append(positive, difference > 0)
	}
	result.NonZero = len(magnitudes)
	if result.NonZero == 0 {
		return result
	}
	ranks, tieGroups := midRanks(magnitudes)
	statistic := 0.0
	for index, rank := range ranks {
		if positive[index] {
			statistic += rank
		}
	}
	result.Statistic = statistic

	droppedZeros := len(differences) != result.NonZero
	if len(tieGroups) == 0 && !droppedZeros && result.NonZero < exactSignedRankLimit {
		result.Exact = true
		result.PValue = exactSignedRankTwoSided(statistic, result.NonZero)
		return result
	}
	result.PValue = normalSignedRankTwoSided(statistic, result.NonZero, tieGroups)
	return result
}

// normalSignedRankTwoSided follows the large-sample branch of R's wilcox.test:
//
//	mean     = n(n+1)/4
//	variance = n(n+1)(2n+1)/24 - sum(t^3 - t)/48
//
// where t runs over the sizes of the groups tied on the absolute difference.
// The 0.5 shift toward the null mean is the continuity correction.
func normalSignedRankTwoSided(statistic float64, count int, tieGroups []int) float64 {
	variance := signedRankVariance(count, tieGroups)
	if variance <= 0 {
		return 1
	}
	size := float64(count)
	centered := statistic - size*(size+1)/4
	correction := 0.0
	switch {
	case centered > 0:
		correction = 0.5
	case centered < 0:
		correction = -0.5
	}
	z := (centered - correction) / math.Sqrt(variance)
	tail := math.Min(standardNormalUpperTail(z), standardNormalUpperTail(-z))
	return math.Min(2*tail, 1)
}

func signedRankVariance(count int, tieGroups []int) float64 {
	size := float64(count)
	tieAdjustment := 0.0
	for _, group := range tieGroups {
		tied := float64(group)
		tieAdjustment += tied*tied*tied - tied
	}
	return size*(size+1)*(2*size+1)/24 - tieAdjustment/48
}

// exactSignedRankTwoSided doubles the smaller exact tail, as R's wilcox.test
// does.
func exactSignedRankTwoSided(statistic float64, count int) float64 {
	if statistic > float64(count)*float64(count+1)/4 {
		return math.Min(2*exactSignedRankUpperTail(statistic, count), 1)
	}
	return math.Min(2*exactSignedRankLowerTail(statistic, count), 1)
}

// exactSignedRankUpperTail is P(V >= statistic) under the null with no ties.
func exactSignedRankUpperTail(statistic float64, count int) float64 {
	counts := exactSignedRankCounts(count)
	total, tail := 0.0, 0.0
	for value, weight := range counts {
		total += weight
		if float64(value) >= statistic {
			tail += weight
		}
	}
	return tail / total
}

func exactSignedRankLowerTail(statistic float64, count int) float64 {
	counts := exactSignedRankCounts(count)
	total, tail := 0.0, 0.0
	for value, weight := range counts {
		total += weight
		if float64(value) <= statistic {
			tail += weight
		}
	}
	return tail / total
}

// exactSignedRankCounts returns the number of sign assignments producing each
// value of V from 0 to n(n+1)/2. V is the sum of the ranks held by the positive
// differences, so the count is a subset-sum tally over the ranks 1 to n.
func exactSignedRankCounts(count int) []float64 {
	high := count * (count + 1) / 2
	table := make([]float64, high+1)
	table[0] = 1
	for rank := 1; rank <= count; rank++ {
		for sum := high; sum >= rank; sum-- {
			if table[sum-rank] != 0 {
				table[sum] += table[sum-rank]
			}
		}
	}
	return table
}

// pairedComparison is the seed-matched contrast the actuation ablation reports.
// The difference is the first arm's steps to first violation less the second's,
// so a positive median means the second arm reached its first violation sooner,
// and Sign carries that direction as a number the decision rule can read.
type pairedComparison struct {
	First            string  `json:"first"`
	Second           string  `json:"second"`
	Pairs            int     `json:"pairs"`
	UnpairedSeeds    []int64 `json:"unpaired_seeds,omitempty"`
	MedianDifference float64 `json:"median_step_difference"`
	Sign             int     `json:"sign"`
	FirstSooner      int     `json:"first_sooner"`
	SecondSooner     int     `json:"second_sooner"`
	Tied             int     `json:"tied"`
	// A12 is the within-pair form of the Vargha-Delaney effect size, the share
	// of matched seeds on which the first arm took more steps, counting a tie as
	// half. A matched design has no reason to compare the two arms as pooled
	// bags of runs when each seed has a partner.
	A12        float64 `json:"a12_within_pairs"`
	Statistic  float64 `json:"signed_rank_v"`
	PValue     float64 `json:"p_value"`
	HolmPValue float64 `json:"holm_p_value"`
	Exact      bool    `json:"exact"`
}

// pairArms matches the two arms by seed and contrasts them pair by pair.
// Censored runs enter at the steps they ran, the same convention the unpaired
// comparison uses. A seed usable in one arm and not the other is named rather
// than dropped silently, because that is a host that lost a run and it is what
// the campaign manifest exists to make visible.
func pairArms(first, second arm) (pairedComparison, error) {
	firstBySeed, err := usableBySeed(first)
	if err != nil {
		return pairedComparison{}, err
	}
	secondBySeed, err := usableBySeed(second)
	if err != nil {
		return pairedComparison{}, err
	}

	comparison := pairedComparison{
		First:      first.Name,
		Second:     second.Name,
		A12:        math.NaN(),
		Statistic:  math.NaN(),
		PValue:     math.NaN(),
		HolmPValue: math.NaN(),
	}
	var differences []float64
	for _, seed := range sortedSeeds(firstBySeed, secondBySeed) {
		left, inFirst := firstBySeed[seed]
		right, inSecond := secondBySeed[seed]
		if !inFirst || !inSecond {
			comparison.UnpairedSeeds = append(comparison.UnpairedSeeds, seed)
			continue
		}
		difference := observationOf(left, first.Budget).Steps - observationOf(right, second.Budget).Steps
		differences = append(differences, difference)
		switch {
		case difference < 0:
			comparison.FirstSooner++
		case difference > 0:
			comparison.SecondSooner++
		default:
			comparison.Tied++
		}
	}
	comparison.Pairs = len(differences)
	if comparison.Pairs == 0 {
		return comparison, nil
	}

	comparison.MedianDifference = medianOf(differences)
	switch {
	case comparison.MedianDifference > 0:
		comparison.Sign = 1
	case comparison.MedianDifference < 0:
		comparison.Sign = -1
	}
	comparison.A12 = (float64(comparison.SecondSooner) + 0.5*float64(comparison.Tied)) / float64(comparison.Pairs)
	test := signedRank(differences)
	comparison.Statistic = test.Statistic
	comparison.PValue = test.PValue
	comparison.Exact = test.Exact
	return comparison, nil
}

func usableBySeed(current arm) (map[int64]classifiedRun, error) {
	bySeed := map[int64]classifiedRun{}
	for _, item := range current.Runs {
		if item.ExcludedBecause != "" {
			continue
		}
		if _, repeated := bySeed[item.Seed]; repeated {
			return nil, fmt.Errorf("arm %q has more than one usable run for seed %d: a seed-matched "+
				"comparison cannot choose between them", current.Name, item.Seed)
		}
		bySeed[item.Seed] = item
	}
	return bySeed, nil
}

func sortedSeeds(sets ...map[int64]classifiedRun) []int64 {
	var seeds []int64
	seen := map[int64]bool{}
	for _, set := range sets {
		for seed := range set {
			if seen[seed] {
				continue
			}
			seen[seed] = true
			seeds = append(seeds, seed)
		}
	}
	slices.Sort(seeds)
	return seeds
}

func medianOf(values []float64) float64 {
	sorted := slices.Sorted(slices.Values(values))
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}
