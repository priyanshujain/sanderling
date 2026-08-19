package main

import (
	"math"
	"testing"
)

func tiedPairs(first, second []float64) int {
	tied := 0
	for _, left := range first {
		for _, right := range second {
			if left == right {
				tied++
			}
		}
	}
	return tied
}

func events(values []float64) []observation {
	items := make([]observation, 0, len(values))
	for _, value := range values {
		items = append(items, observation{Steps: value, Event: true})
	}
	return items
}

// Every ordering censoring supports and every ordering it does not.
func TestOutlives_OrdersOnlyWhatTheCensoringSupports(t *testing.T) {
	cases := []struct {
		name        string
		left, right observation
		want        int
	}{
		{"two violations", observation{30, true}, observation{10, true}, 1},
		{"two violations the other way", observation{10, true}, observation{30, true}, -1},
		{"two violations at the same step", observation{10, true}, observation{10, true}, 0},
		{"censored after the violation", observation{30, false}, observation{10, true}, 1},
		{"censored on the violation's own step", observation{10, false}, observation{10, true}, 1},
		{"censored before the violation", observation{10, false}, observation{30, true}, 0},
		{"violation before the censoring", observation{10, true}, observation{30, false}, -1},
		{"violation after the censoring", observation{30, true}, observation{10, false}, 0},
		{"both censored", observation{10, false}, observation{30, false}, 0},
	}
	for _, test := range cases {
		if got := outlives(test.left, test.right); got != test.want {
			t.Errorf("%s: %v against %v ordered %+d, want %+d", test.name, test.left, test.right, got, test.want)
		}
	}
}

// With nothing censored the test is the rank-sum, so its statistic and effect
// size have to be the ones the rank-sum reports on the same numbers, ties
// included.
func TestGehanTest_ReducesToTheRankSumWhenNothingIsCensored(t *testing.T) {
	cases := [][2][]float64{
		{chorioamnionTerm, chorioamnionEarly},
		{{1, 2, 3, 4}, {3, 4, 5, 6}},
		{{40, 40, 40}, {40, 40, 40, 40}},
		{{5, 6, 7}, {1, 2}},
		{{3}, {9}},
	}
	for _, test := range cases {
		result := gehanTest(events(test[0]), events(test[1]))
		reference := rankSum(test[0], test[1])
		if result.Statistic != reference.Statistic {
			t.Errorf("u %v over %v and %v, want the rank-sum's %v",
				result.Statistic, test[0], test[1], reference.Statistic)
		}
		if result.A12 != reference.A12 {
			t.Errorf("a12 %v over %v and %v, want the rank-sum's %v",
				result.A12, test[0], test[1], reference.A12)
		}
		if want := tiedPairs(test[0], test[1]); result.Unordered != want {
			t.Errorf("%d unordered pair(s) over %v and %v, want the %d tied ones and no others",
				result.Unordered, test[0], test[1], want)
		}
	}
}

// The failure the flattening produced: an arm the wall clock stopped at step 12
// says nothing about step 100, so there is no difference to find and no
// direction to report.
func TestGehanTest_RunsStoppedBeforeEveryViolationOrderNothing(t *testing.T) {
	stopped := []observation{{12, false}, {12, false}, {12, false}, {12, false}}
	violated := []observation{{100, true}, {100, true}, {100, true}}
	result := gehanTest(stopped, violated)

	if result.Unordered != 12 || result.A12 != 0.5 {
		t.Errorf("%d of 12 pairs unordered, a12 %v, want all of them and 0.5", result.Unordered, result.A12)
	}
	if result.PValue < 0.05 {
		t.Errorf("p %v, want no difference between arms never observed over the same steps", result.PValue)
	}
}

// A censored run outliving a violation is evidence, and it is the only kind the
// wall-clock case leaves: four runs still clean at step 12 against three
// violations by step 5.
func TestGehanTest_CensoringLeavesTheEvidenceItDoesSupport(t *testing.T) {
	stopped := []observation{{12, false}, {12, false}, {12, false}, {12, false}}
	violated := []observation{{5, true}, {5, true}, {5, true}}
	result := gehanTest(stopped, violated)

	if result.Unordered != 0 || result.Statistic != 12 || result.A12 != 1 {
		t.Errorf("result %+v, want every pair ordered for the arm that had not violated", result)
	}
	if result.PValue > 0.05 {
		t.Errorf("p %v, want the arms to separate", result.PValue)
	}
}

func riskAndDeaths(group []observation, steps float64) (float64, float64) {
	atRisk, deaths := 0.0, 0.0
	for _, item := range group {
		if item.Steps >= steps {
			atRisk++
		}
		if item.Steps == steps && item.Event {
			deaths++
		}
	}
	return atRisk, deaths
}

// gehanReference is Gehan's statistic and its conditional variance written
// straight from the definitions,
//
//	S = sum over event times of (Y2*d1 - Y1*d2)
//	V = sum over event times of d(Y-d)/(Y-1) * Y1*Y2
//
// which is an independent calculation rather than a second call into the code
// under test.
func gehanReference(first, second []observation) (float64, float64) {
	pooled := append(append([]observation{}, first...), second...)
	statistic, variance := 0.0, 0.0
	for _, steps := range distinctSteps(pooled) {
		firstAtRisk, firstDeaths := riskAndDeaths(first, steps)
		secondAtRisk, secondDeaths := riskAndDeaths(second, steps)
		deaths := firstDeaths + secondDeaths
		if deaths == 0 {
			continue
		}
		atRisk := firstAtRisk + secondAtRisk
		statistic += secondAtRisk*firstDeaths - firstAtRisk*secondDeaths
		if atRisk > 1 {
			variance += deaths * (atRisk - deaths) / (atRisk - 1) * firstAtRisk * secondAtRisk
		}
	}
	return statistic, variance
}

// The effect size and the p-value have to be the same statistic seen twice, or
// the report can carry a direction its p-value does not support. Counting run
// pairs and accumulating over risk sets are two routes to Gehan's statistic, and
// they are tied by S = mn - 2U.
func TestGehanTest_PairCountAndRiskSetAgreeOnOneStatistic(t *testing.T) {
	cases := []struct {
		name          string
		first, second []observation
	}{
		{"6-mp against placebo", gehanSixMercaptopurine, gehanPlacebo},
		{"maintained against nonmaintained", amlMaintained, amlNonmaintained},
		{"stopped short against violating late", []observation{{12, false}, {12, false}, {14, false}},
			[]observation{{5, true}, {100, true}, {100, true}}},
		{"censoring tied with an event", []observation{{20, false}, {20, true}, {35, true}},
			[]observation{{20, true}, {20, false}, {9, true}}},
	}
	for _, test := range cases {
		result := gehanTest(test.first, test.second)
		statistic, variance := gehanReference(test.first, test.second)
		pairs := float64(len(test.first) * len(test.second))

		if got := pairs - 2*result.Statistic; math.Abs(got-statistic) > 1e-9 {
			t.Errorf("%s: pair count gives a statistic of %v, the risk sets give %v", test.name, got, statistic)
		}
		expected := chiSquareUpperTail(statistic*statistic/variance, 1)
		if math.Abs(result.PValue-expected) > 1e-12 {
			t.Errorf("%s: p %v, want %v from statistic %v over variance %v",
				test.name, result.PValue, expected, statistic, variance)
		}
	}
}

// The 6-MP trial is the dataset the test is named for. Its log-rank result is
// checked elsewhere against the published one; here the generalized Wilcoxon
// has to reach the same conclusion, with the maintained arm outliving the
// placebo arm on both routes.
func TestGehanTest_SeparatesThePublishedLeukaemiaTrial(t *testing.T) {
	result := gehanTest(gehanSixMercaptopurine, gehanPlacebo)
	if result.A12 <= 0.5 {
		t.Errorf("a12 %v, want the 6-mp arm to outlive the placebo arm", result.A12)
	}
	if result.PValue > 0.001 {
		t.Errorf("p %v, want the arms to separate as the log-rank has them separate", result.PValue)
	}
	logRankResult := logRank([]string{"6-mp", "placebo"},
		[][]observation{gehanSixMercaptopurine, gehanPlacebo})
	if logRankResult.PValue > 0.001 {
		t.Fatalf("log-rank p %v: the comparison being made is not the one this test assumes", logRankResult.PValue)
	}
}

func censoredAt(count int, steps float64) []observation {
	items := make([]observation, 0, count)
	for index := 0; index < count; index++ {
		items = append(items, observation{Steps: steps})
	}
	return items
}

// A specification whose violations are all obligations reported when the run
// ends puts every event on one step, and the comparison collapses to a single
// two-by-two table of violated against clean. The statistic there is the
// Mantel-Haenszel chi-square of that table,
//
//	(N-1)(ad-bc)^2 / ((a+b)(c+d)(a+c)(b+d))
//
// and the (Y-d)/(Y-1) term in the variance is what carries the (N-1)/N that
// separates it from the Pearson chi-square. Nothing else in the pipeline
// exercises that term hard, because tied events are otherwise rare.
func TestGehanTest_EveryViolationOnOneStepIsTheMantelHaenszelTable(t *testing.T) {
	cases := [][4]int{{30, 20, 10, 40}, {25, 25, 15, 35}, {20, 20, 12, 28}}
	for _, test := range cases {
		firstEvents, firstCensored, secondEvents, secondCensored := test[0], test[1], test[2], test[3]
		first := append(events(repeated(firstEvents, 400)), censoredAt(firstCensored, 400)...)
		second := append(events(repeated(secondEvents, 400)), censoredAt(secondCensored, 400)...)

		total := float64(firstEvents + firstCensored + secondEvents + secondCensored)
		crossProduct := float64(firstEvents*secondCensored - firstCensored*secondEvents)
		chiSquare := (total - 1) * crossProduct * crossProduct /
			(float64(firstEvents+firstCensored) * float64(secondEvents+secondCensored) *
				float64(firstEvents+secondEvents) * float64(firstCensored+secondCensored))

		got := gehanTest(first, second).PValue
		want := chiSquareUpperTail(chiSquare, 1)
		if math.Abs(got-want) > 1e-12 {
			t.Errorf("%v: p %v, want the table's %v from chi-square %v", test, got, want, chiSquare)
		}
	}
}

func repeated(count int, value float64) []float64 {
	values := make([]float64, 0, count)
	for index := 0; index < count; index++ {
		values = append(values, value)
	}
	return values
}

func TestGehanTest_EmptyArmHasNoComparison(t *testing.T) {
	result := gehanTest(nil, []observation{{5, true}})
	if !math.IsNaN(result.PValue) || !math.IsNaN(result.A12) || !math.IsNaN(result.Statistic) {
		t.Errorf("result %+v, want everything undefined", result)
	}
}
