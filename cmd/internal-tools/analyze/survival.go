package main

import (
	"math"
	"slices"
)

// observation is one run reduced to what the survival analysis needs: the step
// count at which it left the risk set, and whether it left because a violation
// was found (an event) or because the run ended without one (right-censored). A run that failed or timed out is neither and never
// reaches this type.
type observation struct {
	Steps float64
	Event bool
}

type survivalPoint struct {
	Steps    float64 `json:"steps"`
	AtRisk   int     `json:"at_risk"`
	Events   int     `json:"events"`
	Censored int     `json:"censored"`
	Survival float64 `json:"survival"`
}

// kaplanMeier is the product-limit estimate of Kaplan and Meier (1958), one row
// per distinct observed step count. Runs censored at a step count tied with an
// event are counted in the risk set for that event, which is the standard
// convention.
func kaplanMeier(observations []observation) []survivalPoint {
	if len(observations) == 0 {
		return nil
	}
	remaining := len(observations)
	survival := 1.0
	var curve []survivalPoint
	for _, steps := range distinctSteps(observations) {
		events, censored := 0, 0
		for _, item := range observations {
			if item.Steps != steps {
				continue
			}
			if item.Event {
				events++
			} else {
				censored++
			}
		}
		atRisk := remaining
		if events > 0 {
			survival *= 1 - float64(events)/float64(atRisk)
		}
		curve = append(curve, survivalPoint{
			Steps:    steps,
			AtRisk:   atRisk,
			Events:   events,
			Censored: censored,
			Survival: survival,
		})
		remaining -= events + censored
	}
	return curve
}

// medianSurvival is the smallest step count at which the estimate falls to or
// below one half. It is undefined whenever fewer than half the runs violate,
// and the second return value says so: substituting a mean there would report a
// number the data does not contain.
func medianSurvival(curve []survivalPoint) (float64, bool) {
	return quantileSurvival(curve, 0.5)
}

func distinctSteps(observations []observation) []float64 {
	steps := make([]float64, 0, len(observations))
	for _, item := range observations {
		steps = append(steps, item.Steps)
	}
	slices.Sort(steps)
	return slices.Compact(steps)
}

type logRankResult struct {
	Groups           []string  `json:"groups"`
	Sizes            []int     `json:"sizes"`
	Observed         []float64 `json:"observed"`
	Expected         []float64 `json:"expected"`
	ChiSquare        float64   `json:"chi_square"`
	DegreesOfFreedom int       `json:"degrees_of_freedom"`
	PValue           float64   `json:"p_value"`
}

// logRank is the k-sample Mantel-Haenszel log-rank test, the member of the
// weighted family that counts every event time alike. Mantel (1966); Peto and
// Peto (1972).
func logRank(names []string, groups [][]observation) logRankResult {
	return weightedLogRank(names, groups, func(atRisk float64) float64 { return 1 })
}

// weightedLogRank is the family the log-rank belongs to. At every distinct event
// time it contrasts observed with expected events under the null of equal
// hazards, weights that difference by weight(atRisk), and combines the k-1
// independent weighted differences through their covariance matrix:
// chi-square = U' V^-1 U on k-1 degrees of freedom. Observed and Expected stay
// event counts whatever the weight, because a weighted count is not one.
// Klein and Moeschberger, Survival Analysis, 2nd ed., section 7.3.
func weightedLogRank(names []string, groups [][]observation, weight func(atRisk float64) float64) logRankResult {
	var keptNames []string
	var kept [][]observation
	for index, group := range groups {
		if len(group) == 0 {
			continue
		}
		keptNames = append(keptNames, names[index])
		kept = append(kept, group)
	}
	names, groups = keptNames, kept

	count := len(groups)
	result := logRankResult{
		Groups:           names,
		Sizes:            make([]int, count),
		Observed:         make([]float64, count),
		Expected:         make([]float64, count),
		DegreesOfFreedom: count - 1,
		PValue:           math.NaN(),
	}
	if count < 2 {
		return result
	}
	var pooled []observation
	for index, group := range groups {
		result.Sizes[index] = len(group)
		pooled = append(pooled, group...)
	}

	covariance := make([][]float64, count)
	for index := range covariance {
		covariance[index] = make([]float64, count)
	}
	atRisk := make([]float64, count)
	deaths := make([]float64, count)
	weightedDifference := make([]float64, count)

	for _, steps := range distinctSteps(pooled) {
		totalAtRisk, totalDeaths := 0.0, 0.0
		for index, group := range groups {
			atRisk[index], deaths[index] = 0, 0
			for _, item := range group {
				if item.Steps >= steps {
					atRisk[index]++
				}
				if item.Steps == steps && item.Event {
					deaths[index]++
				}
			}
			totalAtRisk += atRisk[index]
			totalDeaths += deaths[index]
		}
		if totalDeaths == 0 {
			continue
		}
		weightAtStep := weight(totalAtRisk)
		for index := range groups {
			expected := totalDeaths * atRisk[index] / totalAtRisk
			result.Observed[index] += deaths[index]
			result.Expected[index] += expected
			weightedDifference[index] += weightAtStep * (deaths[index] - expected)
		}
		if totalAtRisk <= 1 {
			continue
		}
		scale := weightAtStep * weightAtStep * totalDeaths * (totalAtRisk - totalDeaths) / (totalAtRisk - 1)
		for row := range groups {
			share := atRisk[row] / totalAtRisk
			covariance[row][row] += scale * share * (1 - share)
			for column := range groups {
				if column == row {
					continue
				}
				covariance[row][column] -= scale * share * atRisk[column] / totalAtRisk
			}
		}
	}

	reduced := make([][]float64, count-1)
	difference := make([]float64, count-1)
	for row := 0; row < count-1; row++ {
		reduced[row] = make([]float64, count-1)
		copy(reduced[row], covariance[row][:count-1])
		difference[row] = weightedDifference[row]
	}
	solution, ok := solveLinearSystem(reduced, difference)
	if !ok {
		result.ChiSquare = 0
		result.PValue = 1
		return result
	}
	statistic := 0.0
	for index := range difference {
		statistic += difference[index] * solution[index]
	}
	if statistic < 0 || math.IsNaN(statistic) {
		statistic = 0
	}
	result.ChiSquare = statistic
	result.PValue = chiSquareUpperTail(statistic, result.DegreesOfFreedom)
	return result
}

// solveLinearSystem solves matrix*x = vector by Gaussian elimination with
// partial pivoting, reporting failure rather than a value when the matrix is
// singular, which is what a group with no events at all produces.
func solveLinearSystem(matrix [][]float64, vector []float64) ([]float64, bool) {
	size := len(vector)
	work := make([][]float64, size)
	for row := range work {
		work[row] = make([]float64, size+1)
		copy(work[row], matrix[row])
		work[row][size] = vector[row]
	}
	for column := 0; column < size; column++ {
		pivot := column
		for row := column + 1; row < size; row++ {
			if math.Abs(work[row][column]) > math.Abs(work[pivot][column]) {
				pivot = row
			}
		}
		if math.Abs(work[pivot][column]) < 1e-12 {
			return nil, false
		}
		work[column], work[pivot] = work[pivot], work[column]
		for row := column + 1; row < size; row++ {
			factor := work[row][column] / work[column][column]
			for next := column; next <= size; next++ {
				work[row][next] -= factor * work[column][next]
			}
		}
	}
	solution := make([]float64, size)
	for row := size - 1; row >= 0; row-- {
		total := work[row][size]
		for column := row + 1; column < size; column++ {
			total -= work[row][column] * solution[column]
		}
		solution[row] = total / work[row][row]
	}
	return solution, true
}
