package main

// quantileSurvival is the smallest step count at which the product-limit
// estimate falls to or below 1-fraction, which is the fraction-th quantile of
// steps to first violation. It is undefined whenever the curve never falls that
// far, which is what an arm where most runs exhaust the budget produces, and the
// second return value says so rather than substituting a number the data does
// not contain.
func quantileSurvival(curve []survivalPoint, fraction float64) (float64, bool) {
	threshold := 1 - fraction
	for _, point := range curve {
		if point.Survival <= threshold {
			return point.Steps, true
		}
	}
	return 0, false
}
