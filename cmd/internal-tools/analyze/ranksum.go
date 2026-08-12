package main

import (
	"math"
	"slices"
)

type rankSumResult struct {
	FirstSize  int     `json:"first_size"`
	SecondSize int     `json:"second_size"`
	Statistic  float64 `json:"mann_whitney_u"`
	A12        float64 `json:"a12"`
	PValue     float64 `json:"p_value"`
	Exact      bool    `json:"exact"`
}

// exactRankSumLimit matches R's wilcox.test: the exact null distribution is
// used only when both samples are below this size and nothing is tied.
const exactRankSumLimit = 50

// vargaDelaneyA12 is the probability that a value drawn from first exceeds one
// drawn from second, counting a tie as half:
//
//	A = P(X > Y) + 0.5 * P(X = Y)
//
// Vargha and Delaney (2000), "A Critique and Improvement of the CL Common
// Language Effect Size Statistics of McGraw and Wong", Journal of Educational
// and Behavioral Statistics 25(2), 101-132.
func vargaDelaneyA12(first, second []float64) float64 {
	if len(first) == 0 || len(second) == 0 {
		return math.NaN()
	}
	total := 0.0
	for _, left := range first {
		for _, right := range second {
			switch {
			case left > right:
				total++
			case left == right:
				total += 0.5
			}
		}
	}
	return total / float64(len(first)*len(second))
}

// rankSum is the two-sided Wilcoxon rank-sum (Mann-Whitney) test. The reported
// statistic is U for the first sample, the same quantity R's wilcox.test calls
// W. The exact null distribution is used when there are no ties and both
// samples are small; otherwise the normal approximation is used with the
// continuity correction and the tie correction to the variance.
func rankSum(first, second []float64) rankSumResult {
	firstSize, secondSize := len(first), len(second)
	result := rankSumResult{
		FirstSize:  firstSize,
		SecondSize: secondSize,
		Statistic:  math.NaN(),
		A12:        math.NaN(),
		PValue:     math.NaN(),
	}
	if firstSize == 0 || secondSize == 0 {
		return result
	}
	pooled := make([]float64, 0, firstSize+secondSize)
	pooled = append(pooled, first...)
	pooled = append(pooled, second...)
	ranks, tieGroups := midRanks(pooled)

	rankTotal := 0.0
	for index := 0; index < firstSize; index++ {
		rankTotal += ranks[index]
	}
	statistic := rankTotal - float64(firstSize)*float64(firstSize+1)/2
	result.Statistic = statistic
	result.A12 = vargaDelaneyA12(first, second)

	if len(tieGroups) == 0 && firstSize < exactRankSumLimit && secondSize < exactRankSumLimit {
		result.Exact = true
		result.PValue = exactRankSumTwoSided(statistic, firstSize, secondSize)
		return result
	}
	result.PValue = normalRankSumTwoSided(statistic, firstSize, secondSize, tieGroups)
	return result
}

// normalRankSumTwoSided follows the large-sample branch of R's wilcox.test:
//
//	sigma^2 = (m*n/12) * ((N+1) - sum(t^3 - t) / (N*(N-1)))
//
// where t runs over the sizes of the tied groups. The 0.5 shift toward the null
// mean is the continuity correction.
func normalRankSumTwoSided(statistic float64, firstSize, secondSize int, tieGroups []int) float64 {
	sizeProduct := float64(firstSize) * float64(secondSize)
	variance := rankSumVariance(firstSize, secondSize, tieGroups)
	if variance <= 0 {
		return 1
	}
	centered := statistic - sizeProduct/2
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

func rankSumVariance(firstSize, secondSize int, tieGroups []int) float64 {
	sizeProduct := float64(firstSize) * float64(secondSize)
	total := float64(firstSize + secondSize)
	tieAdjustment := 0.0
	for _, size := range tieGroups {
		count := float64(size)
		tieAdjustment += count*count*count - count
	}
	return (sizeProduct / 12) * ((total + 1) - tieAdjustment/(total*(total-1)))
}

// exactRankSumTwoSided doubles the smaller exact tail, as R's wilcox.test does.
func exactRankSumTwoSided(statistic float64, firstSize, secondSize int) float64 {
	counts := exactRankSumCounts(firstSize, secondSize)
	total := 0.0
	for _, count := range counts {
		total += count
	}
	value := int(math.Round(statistic))
	tail := 0.0
	if statistic > float64(firstSize*secondSize)/2 {
		for u := value; u < len(counts); u++ {
			tail += counts[u]
		}
	} else {
		for u := 0; u <= value && u < len(counts); u++ {
			tail += counts[u]
		}
	}
	return math.Min(2*tail/total, 1)
}

// exactRankSumUpperTail is P(U >= statistic) under the null with no ties.
func exactRankSumUpperTail(statistic float64, firstSize, secondSize int) float64 {
	counts := exactRankSumCounts(firstSize, secondSize)
	total, tail := 0.0, 0.0
	for u, count := range counts {
		total += count
		if float64(u) >= statistic {
			tail += count
		}
	}
	return tail / total
}

// exactRankSumCounts returns the number of untied assignments producing each
// value of U from 0 to firstSize*secondSize. U equals the sum of the zero-based
// pooled positions held by the first sample, less firstSize*(firstSize-1)/2, so
// the count is a subset-sum tally over those positions.
func exactRankSumCounts(firstSize, secondSize int) []float64 {
	maximum := firstSize * secondSize
	offset := firstSize * (firstSize - 1) / 2
	high := maximum + offset
	table := make([][]float64, firstSize+1)
	for index := range table {
		table[index] = make([]float64, high+1)
	}
	table[0][0] = 1
	for position := 0; position < firstSize+secondSize; position++ {
		for chosen := min(position+1, firstSize); chosen >= 1; chosen-- {
			row, previous := table[chosen], table[chosen-1]
			for sum := high; sum >= position; sum-- {
				if previous[sum-position] != 0 {
					row[sum] += previous[sum-position]
				}
			}
		}
	}
	counts := make([]float64, maximum+1)
	for u := range counts {
		counts[u] = table[firstSize][u+offset]
	}
	return counts
}

// midRanks ranks values from 1, averaging the ranks within a tied group, and
// also returns the size of every group of size two or more.
func midRanks(values []float64) ([]float64, []int) {
	order := make([]int, len(values))
	for index := range order {
		order[index] = index
	}
	slices.SortStableFunc(order, func(left, right int) int {
		switch {
		case values[left] < values[right]:
			return -1
		case values[left] > values[right]:
			return 1
		default:
			return 0
		}
	})
	ranks := make([]float64, len(values))
	var tieGroups []int
	for start := 0; start < len(order); {
		end := start + 1
		for end < len(order) && values[order[end]] == values[order[start]] {
			end++
		}
		shared := float64(start+1+end) / 2
		for index := start; index < end; index++ {
			ranks[order[index]] = shared
		}
		if end-start > 1 {
			tieGroups = append(tieGroups, end-start)
		}
		start = end
	}
	return ranks, tieGroups
}
