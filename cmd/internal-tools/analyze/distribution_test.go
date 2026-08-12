package main

import (
	"math"
	"testing"
)

// Chi-square critical values are the standard published table entries: the
// upper-tail probability of each of these statistics is the stated alpha in any
// chi-square table, for example Pearson and Hartley, Biometrika Tables for
// Statisticians, Table 8.
func TestChiSquareUpperTail_MatchesPublishedCriticalValues(t *testing.T) {
	cases := []struct {
		statistic        float64
		degreesOfFreedom int
		expected         float64
	}{
		{3.841459, 1, 0.05},
		{6.634897, 1, 0.01},
		{10.827566, 1, 0.001},
		{5.991465, 2, 0.05},
		{9.210340, 2, 0.01},
		{7.814728, 3, 0.05},
		{11.344867, 3, 0.01},
		{9.487729, 4, 0.05},
		{18.307038, 10, 0.05},
	}
	for _, test := range cases {
		got := chiSquareUpperTail(test.statistic, test.degreesOfFreedom)
		if math.Abs(got-test.expected) > 1e-6 {
			t.Errorf("chiSquareUpperTail(%v, %d) = %v, want %v", test.statistic, test.degreesOfFreedom, got, test.expected)
		}
	}
}

// For one degree of freedom the upper tail has the closed form erfc(sqrt(x/2)),
// which is an independent check on the incomplete gamma routine.
func TestChiSquareUpperTail_AgreesWithClosedFormAtOneDegreeOfFreedom(t *testing.T) {
	for _, statistic := range []float64{0.1, 1, 3.4, 16.79, 40, 120} {
		expected := math.Erfc(math.Sqrt(statistic / 2))
		got := chiSquareUpperTail(statistic, 1)
		if math.Abs(got-expected) > 1e-12*math.Max(1, expected) {
			t.Errorf("chiSquareUpperTail(%v, 1) = %v, want %v", statistic, got, expected)
		}
	}
}

func TestChiSquareUpperTail_ZeroStatisticIsCertain(t *testing.T) {
	if got := chiSquareUpperTail(0, 1); got != 1 {
		t.Errorf("chiSquareUpperTail(0, 1) = %v, want 1", got)
	}
}

// Standard normal quantiles from any published normal table.
func TestStandardNormalUpperTail_MatchesPublishedQuantiles(t *testing.T) {
	cases := []struct {
		z        float64
		expected float64
	}{
		{1.281552, 0.10},
		{1.644854, 0.05},
		{1.959964, 0.025},
		{2.326348, 0.01},
		{2.575829, 0.005},
		{3.090232, 0.001},
		{0, 0.5},
	}
	for _, test := range cases {
		got := standardNormalUpperTail(test.z)
		if math.Abs(got-test.expected) > 1e-6 {
			t.Errorf("standardNormalUpperTail(%v) = %v, want %v", test.z, got, test.expected)
		}
	}
}
