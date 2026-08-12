package main

import (
	"math"
	"math/rand"
	"slices"
	"strconv"
	"testing"
)

// The product-limit estimates for the 6-MP arm of Freireich et al. (1963) are
// the worked example reproduced in Collett, Modelling Survival Data in Medical
// Research, and in the standard course treatments of the gehan data:
//
//	t:    6      7      10     13     16     22     23
//	S(t): 0.857  0.807  0.753  0.690  0.627  0.538  0.448
func TestKaplanMeier_MatchesPublishedGehanEstimates(t *testing.T) {
	curve := kaplanMeier(gehanSixMercaptopurine)
	expected := map[float64]float64{
		6: 0.857, 7: 0.807, 10: 0.753, 13: 0.690, 16: 0.627, 22: 0.538, 23: 0.448,
	}
	seen := 0
	for _, point := range curve {
		want, ok := expected[point.Steps]
		if !ok {
			continue
		}
		seen++
		if math.Abs(point.Survival-want) > 5e-4 {
			t.Errorf("S(%v) = %.4f, want %v", point.Steps, point.Survival, want)
		}
	}
	if seen != len(expected) {
		t.Fatalf("matched %d of %d published times", seen, len(expected))
	}
}

// The risk set at each time is the count of runs still under observation, with
// runs censored at a tied time counted as at risk for that event.
func TestKaplanMeier_RiskSetHandlesTiesAndCensoring(t *testing.T) {
	curve := kaplanMeier(gehanSixMercaptopurine)
	expected := map[float64]struct {
		atRisk   int
		events   int
		censored int
	}{
		6:  {21, 3, 1},
		7:  {17, 1, 0},
		9:  {16, 0, 1},
		10: {15, 1, 1},
		13: {12, 1, 0},
		23: {6, 1, 0},
	}
	for _, point := range curve {
		want, ok := expected[point.Steps]
		if !ok {
			continue
		}
		if point.AtRisk != want.atRisk || point.Events != want.events || point.Censored != want.censored {
			t.Errorf("at %v: risk=%d events=%d censored=%d, want risk=%d events=%d censored=%d",
				point.Steps, point.AtRisk, point.Events, point.Censored, want.atRisk, want.events, want.censored)
		}
	}
}

// Published medians: 23 weeks for 6-MP against 8 weeks for placebo (Gehan and
// Freireich, "The 6-MP versus placebo clinical trial in acute leukemia",
// Clinical Trials 8(3), 2011), and 31 against 23 weeks for the aml arms as
// reported by survfit in R's survival package.
func TestMedianSurvival_MatchesPublishedMedians(t *testing.T) {
	cases := []struct {
		name         string
		observations []observation
		expected     float64
	}{
		{"gehan 6-MP", gehanSixMercaptopurine, 23},
		{"gehan placebo", gehanPlacebo, 8},
		{"aml maintained", amlMaintained, 31},
		{"aml nonmaintained", amlNonmaintained, 23},
	}
	for _, test := range cases {
		median, ok := medianSurvival(kaplanMeier(test.observations))
		if !ok {
			t.Errorf("%s: median undefined, want %v", test.name, test.expected)
			continue
		}
		if median != test.expected {
			t.Errorf("%s: median %v, want %v", test.name, median, test.expected)
		}
	}
}

// R's survival package documents this log-rank on the aml data:
//
//	                N Observed Expected (O-E)^2/E (O-E)^2/V
//	x=Maintained   11        7    10.69      1.27       3.4
//	x=Nonmaintained 12      11     7.31      1.86       3.4
//	Chisq= 3.4  on 1 degrees of freedom, p= 0.0653
func TestLogRank_MatchesPublishedAmlResult(t *testing.T) {
	result := logRank([]string{"maintained", "nonmaintained"}, [][]observation{amlMaintained, amlNonmaintained})

	if result.Observed[0] != 7 || result.Observed[1] != 11 {
		t.Errorf("observed %v, want [7 11]", result.Observed)
	}
	if math.Abs(result.Expected[0]-10.69) > 5e-3 || math.Abs(result.Expected[1]-7.31) > 5e-3 {
		t.Errorf("expected %v, want [10.69 7.31]", result.Expected)
	}
	for index, want := range []float64{1.27, 1.86} {
		difference := result.Observed[index] - result.Expected[index]
		got := difference * difference / result.Expected[index]
		if math.Abs(got-want) > 5e-3 {
			t.Errorf("(O-E)^2/E for group %d = %.4f, want %v", index, got, want)
		}
	}
	if math.Abs(result.ChiSquare-3.4) > 5e-2 {
		t.Errorf("chi-square %.4f, want 3.4", result.ChiSquare)
	}
	if math.Abs(result.PValue-0.0653) > 5e-4 {
		t.Errorf("p-value %.6f, want 0.0653", result.PValue)
	}
	if result.DegreesOfFreedom != 1 {
		t.Errorf("degrees of freedom %d, want 1", result.DegreesOfFreedom)
	}
}

// The log-rank on the Freireich 6-MP trial is the textbook worked example:
// observed 9 against 19.25 expected in the treated arm and 21 against 10.75 in
// the control arm, Mantel-Haenszel chi-square 16.79 on 1 degree of freedom,
// p = 4.17e-05. Reported for instance in Rodriguez, Kaplan-Meier and
// Mantel-Haenszel, https://grodri.github.io/survival/gehan, and in Collett.
func TestLogRank_MatchesPublishedGehanResult(t *testing.T) {
	result := logRank([]string{"6-MP", "placebo"}, [][]observation{gehanSixMercaptopurine, gehanPlacebo})

	if result.Observed[0] != 9 || result.Observed[1] != 21 {
		t.Errorf("observed %v, want [9 21]", result.Observed)
	}
	if math.Abs(result.Expected[0]-19.25) > 5e-3 || math.Abs(result.Expected[1]-10.75) > 5e-3 {
		t.Errorf("expected %v, want [19.25 10.75]", result.Expected)
	}
	if math.Abs(result.ChiSquare-16.79) > 5e-3 {
		t.Errorf("chi-square %.4f, want 16.79", result.ChiSquare)
	}
	if math.Abs(result.PValue-4.17e-5) > 5e-8 {
		t.Errorf("p-value %.3e, want 4.17e-05", result.PValue)
	}
}

// An arm with no usable runs contributes nothing and must not consume a degree
// of freedom or make the covariance matrix singular.
func TestLogRank_EmptyGroupIsDropped(t *testing.T) {
	two := logRank([]string{"a", "b"}, [][]observation{amlMaintained, amlNonmaintained})
	three := logRank([]string{"a", "b", "c"}, [][]observation{amlMaintained, amlNonmaintained, nil})
	if math.Abs(two.ChiSquare-three.ChiSquare) > 1e-12 {
		t.Errorf("chi-square %.10f with an empty third group, want %.10f", three.ChiSquare, two.ChiSquare)
	}
	if three.DegreesOfFreedom != 1 {
		t.Errorf("degrees of freedom %d, want 1", three.DegreesOfFreedom)
	}
	if len(three.Groups) != 2 {
		t.Errorf("groups %v, want the empty arm dropped", three.Groups)
	}
}

// No published multi-arm dataset with a printed log-rank chi-square was found
// small enough to embed, so the k-group covariance algebra is checked against
// its own null distribution instead: under the null of equal hazards the
// statistic is asymptotically chi-square on k-1 degrees of freedom, so its mean
// over random relabellings has to sit near k-1. A wrong variance term or a wrong
// degrees-of-freedom count moves this badly.
func TestLogRank_NullMeanTracksDegreesOfFreedom(t *testing.T) {
	pooled := make([]observation, 0, 36)
	for index := 0; index < 36; index++ {
		pooled = append(pooled, observation{Steps: float64(index%17 + 1), Event: index%5 != 0})
	}
	for _, groupCount := range []int{2, 3, 4} {
		generator := rand.New(rand.NewSource(20260812))
		total := 0.0
		const replicates = 4000
		for replicate := 0; replicate < replicates; replicate++ {
			shuffled := slices.Clone(pooled)
			generator.Shuffle(len(shuffled), func(left, right int) {
				shuffled[left], shuffled[right] = shuffled[right], shuffled[left]
			})
			names := make([]string, groupCount)
			groups := make([][]observation, groupCount)
			for index, item := range shuffled {
				groups[index%groupCount] = append(groups[index%groupCount], item)
			}
			for index := range names {
				names[index] = strconv.Itoa(index)
			}
			total += logRank(names, groups).ChiSquare
		}
		mean := total / replicates
		expected := float64(groupCount - 1)
		if math.Abs(mean-expected) > 0.15*expected {
			t.Errorf("%d groups: null mean chi-square %.3f, want near %v", groupCount, mean, expected)
		}
	}
}

// A three-group split of one homogeneous sample must not look significant, and
// the statistic must be finite on 2 degrees of freedom.
func TestLogRank_ThreeIdenticalGroupsAreNotSignificant(t *testing.T) {
	group := []observation{{4, true}, {7, true}, {9, false}, {12, true}, {20, false}}
	result := logRank([]string{"a", "b", "c"}, [][]observation{group, group, group})
	if result.DegreesOfFreedom != 2 {
		t.Fatalf("degrees of freedom %d, want 2", result.DegreesOfFreedom)
	}
	if result.ChiSquare > 1e-9 {
		t.Errorf("chi-square %.10f for three identical groups, want 0", result.ChiSquare)
	}
	if math.Abs(result.PValue-1) > 1e-9 {
		t.Errorf("p-value %v, want 1", result.PValue)
	}
}

func TestKaplanMeier_EveryObservationCensored(t *testing.T) {
	observations := []observation{{40, false}, {40, false}, {40, false}}
	curve := kaplanMeier(observations)
	if len(curve) != 1 {
		t.Fatalf("curve has %d points, want 1", len(curve))
	}
	if curve[0].Survival != 1 || curve[0].Events != 0 || curve[0].Censored != 3 {
		t.Errorf("point %+v, want survival 1 with 3 censored", curve[0])
	}
	if _, ok := medianSurvival(curve); ok {
		t.Error("median defined for an arm where nothing violated")
	}
}

func TestKaplanMeier_EveryObservationAnEvent(t *testing.T) {
	curve := kaplanMeier([]observation{{2, true}, {4, true}, {6, true}, {8, true}})
	last := curve[len(curve)-1]
	if last.Survival != 0 {
		t.Errorf("final survival %v, want 0", last.Survival)
	}
	median, ok := medianSurvival(curve)
	if !ok || median != 4 {
		t.Errorf("median %v ok=%v, want 4", median, ok)
	}
}

func TestKaplanMeier_TiedEventTimesDropOnce(t *testing.T) {
	curve := kaplanMeier([]observation{{5, true}, {5, true}, {5, true}, {9, true}})
	if len(curve) != 2 {
		t.Fatalf("curve has %d points, want 2", len(curve))
	}
	if curve[0].Events != 3 || math.Abs(curve[0].Survival-0.25) > 1e-12 {
		t.Errorf("first point %+v, want 3 events and survival 0.25", curve[0])
	}
	if curve[1].Survival != 0 {
		t.Errorf("second point %+v, want survival 0", curve[1])
	}
}

func TestKaplanMeier_SingleObservation(t *testing.T) {
	event := kaplanMeier([]observation{{11, true}})
	if len(event) != 1 || event[0].Survival != 0 {
		t.Fatalf("single event curve %+v", event)
	}
	median, ok := medianSurvival(event)
	if !ok || median != 11 {
		t.Errorf("median %v ok=%v, want 11", median, ok)
	}
	censored := kaplanMeier([]observation{{11, false}})
	if len(censored) != 1 || censored[0].Survival != 1 {
		t.Fatalf("single censored curve %+v", censored)
	}
	if _, ok := medianSurvival(censored); ok {
		t.Error("median defined for a single censored observation")
	}
}

func TestKaplanMeier_NoObservations(t *testing.T) {
	if curve := kaplanMeier(nil); curve != nil {
		t.Errorf("curve %v for no observations, want nil", curve)
	}
	if _, ok := medianSurvival(nil); ok {
		t.Error("median defined for an empty curve")
	}
}

func TestLogRank_SingleObservationPerGroup(t *testing.T) {
	result := logRank([]string{"a", "b"}, [][]observation{{{3, true}}, {{9, true}}})
	if math.IsNaN(result.ChiSquare) || result.ChiSquare < 0 {
		t.Errorf("chi-square %v", result.ChiSquare)
	}
	if math.IsNaN(result.PValue) || result.PValue > 1 || result.PValue < 0 {
		t.Errorf("p-value %v", result.PValue)
	}
}

// With no events anywhere the covariance matrix is singular and there is
// nothing to test, which must report no difference rather than a divide by zero.
func TestLogRank_NoEventsAnywhere(t *testing.T) {
	result := logRank([]string{"a", "b"}, [][]observation{
		{{40, false}, {40, false}},
		{{40, false}, {40, false}, {40, false}},
	})
	if result.ChiSquare != 0 || result.PValue != 1 {
		t.Errorf("chi-square %v p-value %v, want 0 and 1", result.ChiSquare, result.PValue)
	}
}
