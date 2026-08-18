package main

import "math"

// gehanResult is one pairwise comparison of two arms of right-censored runs: an
// effect size, the run pairs that have no order between them, and the test of
// the same statistic against the null of equal hazards.
type gehanResult struct {
	FirstSize  int
	SecondSize int
	Statistic  float64
	A12        float64
	Unordered  int
	PValue     float64
}

// outlives orders two runs the only way right-censoring allows. A run censored
// at step t violated at no step up to t and stopped for a reason of its own, so
// it outlives a violation at or before t and nothing orders it against a
// violation after t or against another censored run. Comparing the two step
// counts as plain numbers instead reads a run the wall clock stopped at step 12
// as one that violated at step 12.
func outlives(left, right observation) int {
	switch {
	case left.Event && right.Event:
		switch {
		case left.Steps > right.Steps:
			return 1
		case left.Steps < right.Steps:
			return -1
		}
	case left.Event:
		if right.Steps >= left.Steps {
			return -1
		}
	case right.Event:
		if left.Steps >= right.Steps {
			return 1
		}
	}
	return 0
}

// atRiskWeight is Gehan's weight: an event counts for as many runs as were still
// at risk when it happened. It is what makes the weighted log-rank statistic the
// same quantity as the pairwise count below, so the effect size and the p-value
// are one statistic rather than two that can disagree.
func atRiskWeight(atRisk float64) float64 { return atRisk }

// gehanTest is the Gehan-Breslow generalized Wilcoxon test: the rank-sum
// carried over to right-censored samples by scoring every pair of runs by which
// one outlived the other and leaving the pairs censoring cannot order out of the
// count. Gehan (1965), "A Generalized Wilcoxon Test for Comparing Arbitrarily
// Singly-Censored Samples", Biometrika 52(1-2), 203-223; Breslow (1970).
//
// Statistic is that count, U, and A12 is it over the number of pairs: the share
// of run pairs in which the first arm survived longer, an unordered pair
// counting as half. With nothing censored the two are exactly the Mann-Whitney U
// and the Vargha-Delaney A12 the uncensored rank-sum reports. Where censoring
// leaves a pair unordered, the half it contributes is the null value, so an
// unordered pair can only pull the effect size toward 0.5 and can never
// manufacture a direction.
//
// The p-value is the same statistic standardized: the weighted log-rank with
// Gehan's weight has this U for its statistic, and its variance is the
// conditional hypergeometric one summed over event times, which is what keeps
// the test honest when the arms censor on different schedules. The permutation
// variance Gehan originally paired with the statistic does not.
func gehanTest(first, second []observation) gehanResult {
	result := gehanResult{
		FirstSize:  len(first),
		SecondSize: len(second),
		Statistic:  math.NaN(),
		A12:        math.NaN(),
		PValue:     math.NaN(),
	}
	if len(first) == 0 || len(second) == 0 {
		return result
	}
	outlived := 0.0
	for _, left := range first {
		for _, right := range second {
			switch outlives(left, right) {
			case 1:
				outlived++
			case 0:
				outlived += 0.5
				result.Unordered++
			}
		}
	}
	result.Statistic = outlived
	result.A12 = outlived / float64(len(first)*len(second))
	test := weightedLogRank([]string{"first", "second"}, [][]observation{first, second}, atRiskWeight)
	result.PValue = test.PValue
	return result
}
