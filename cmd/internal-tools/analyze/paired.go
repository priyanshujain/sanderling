package main

import (
	"fmt"
	"math"
	"slices"
)

// signTest is the exact two-sided sign test over matched pairs. Under the null
// that neither arm reaches its first violation sooner, a pair whose order the
// censoring determines falls either way with probability one half, so the count
// is binomial and the two-sided p-value doubles the smaller tail. Pairs left
// with no order carry no information and are not trials.
//
// It is the seed-matched form of the comparison the unpaired test makes, and
// it is what the log-rank stratified by seed reduces to with one run per arm in
// each stratum. The magnitude-based alternatives are not available: a
// difference in steps needs both runs to have violated, and a paired test built
// on scores of censored times, the paired Prentice-Wilcoxon among them, is
// centred at zero under the null only when the two arms censor alike, which is
// exactly what the wall clock stops them from doing.
func signTest(favouringFirst, favouringSecond int) float64 {
	trials := favouringFirst + favouringSecond
	if trials == 0 {
		return math.NaN()
	}
	smaller := min(favouringFirst, favouringSecond)
	tail := 0.0
	for count := 0; count <= smaller; count++ {
		tail += math.Exp(logBinomialCoefficient(trials, count) - float64(trials)*math.Ln2)
	}
	return math.Min(2*tail, 1)
}

func logBinomialCoefficient(trials, chosen int) float64 {
	all, _ := math.Lgamma(float64(trials + 1))
	picked, _ := math.Lgamma(float64(chosen + 1))
	rest, _ := math.Lgamma(float64(trials-chosen) + 1)
	return all - picked - rest
}

// pairedComparison is the seed-matched contrast the actuation ablation reports.
// A pair is scored the way the unpaired comparison scores one, by which run
// outlived the other, so Sign is +1 when the second arm is the one seen to
// violate sooner across the pairs whose order censoring determines.
type pairedComparison struct {
	First         string  `json:"first"`
	Second        string  `json:"second"`
	Pairs         int     `json:"pairs"`
	UnpairedSeeds []int64 `json:"unpaired_seeds,omitempty"`
	Sign          int     `json:"sign"`
	FirstSooner   int     `json:"first_sooner"`
	SecondSooner  int     `json:"second_sooner"`
	// Unordered is the pairs the censoring leaves in no order, either because
	// both runs ended clean or because the run that stopped first stopped before
	// the other violated. They are not evidence either way and are not trials.
	Unordered int `json:"unordered_pairs"`
	// MedianDifference is in steps and is undefined unless some pair has both
	// runs violating, which is the only shape a difference in steps can be read
	// off. BothViolated says how many pairs it summarizes, because it describes
	// those pairs and not the sample.
	MedianDifference *float64 `json:"median_step_difference"`
	BothViolated     int      `json:"both_violated_pairs"`
	// A12 is the within-pair form of the Vargha-Delaney effect size, the share
	// of matched seeds on which the first arm took more steps, an unordered pair
	// counting as half. A matched design has no reason to compare the two arms
	// as pooled bags of runs when each seed has a partner.
	A12        float64 `json:"a12_within_pairs"`
	PValue     float64 `json:"p_value"`
	HolmPValue float64 `json:"holm_p_value"`
}

// pairArms matches the two arms by seed and contrasts them pair by pair. A seed
// usable in one arm and not the other is named rather than dropped silently,
// because that is a host that lost a run and it is what the campaign manifest
// exists to make visible.
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
		leftRun := observationOf(left, first.Budget)
		rightRun := observationOf(right, second.Budget)
		comparison.Pairs++
		switch outlives(leftRun, rightRun) {
		case 1:
			comparison.SecondSooner++
		case -1:
			comparison.FirstSooner++
		default:
			comparison.Unordered++
		}
		if leftRun.Event && rightRun.Event {
			comparison.BothViolated++
			differences = append(differences, leftRun.Steps-rightRun.Steps)
		}
	}
	if comparison.Pairs == 0 {
		return comparison, nil
	}

	if len(differences) > 0 {
		median := medianOf(differences)
		comparison.MedianDifference = &median
	}
	switch {
	case comparison.SecondSooner > comparison.FirstSooner:
		comparison.Sign = 1
	case comparison.FirstSooner > comparison.SecondSooner:
		comparison.Sign = -1
	}
	comparison.A12 = (float64(comparison.SecondSooner) + 0.5*float64(comparison.Unordered)) / float64(comparison.Pairs)
	comparison.PValue = signTest(comparison.FirstSooner, comparison.SecondSooner)
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
