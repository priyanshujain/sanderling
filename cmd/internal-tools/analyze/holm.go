package main

import (
	"math"
	"slices"
)

// holm applies the Holm (1979) step-down correction within one family of
// comparisons, enforcing monotonicity across the sorted p-values the way R's
// p.adjust does. Holm, "A Simple Sequentially Rejective Multiple Test
// Procedure", Scandinavian Journal of Statistics 6(2), 65-70.
func holm(pValues []float64) []float64 {
	count := len(pValues)
	adjusted := make([]float64, count)
	order := make([]int, count)
	for index := range order {
		order[index] = index
	}
	slices.SortStableFunc(order, func(left, right int) int {
		switch {
		case pValues[left] < pValues[right]:
			return -1
		case pValues[left] > pValues[right]:
			return 1
		default:
			return 0
		}
	})
	running := 0.0
	for position, index := range order {
		scaled := float64(count-position) * pValues[index]
		running = math.Max(running, scaled)
		adjusted[index] = math.Min(running, 1)
	}
	return adjusted
}
