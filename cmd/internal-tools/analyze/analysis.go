package main

import (
	"fmt"
	"math"
	"slices"
	"time"
)

type armSummary struct {
	Arm                         string          `json:"arm"`
	Generator                   string          `json:"generator,omitempty"`
	Platform                    string          `json:"platform,omitempty"`
	StepBudget                  int             `json:"step_budget"`
	Directories                 []string        `json:"directories"`
	Recorded                    int             `json:"recorded_runs"`
	Usable                      int             `json:"usable_runs"`
	Violated                    int             `json:"violated_runs"`
	Censored                    int             `json:"censored_runs"`
	Excluded                    int             `json:"excluded_runs"`
	ExcludedByReason            map[string]int  `json:"excluded_by_reason,omitempty"`
	MissingSeeds                []int64         `json:"missing_seeds,omitempty"`
	EventsHeldAtBudget          int             `json:"events_held_at_budget"`
	EventsDetectedAfterOrigin   int             `json:"events_detected_after_origin"`
	MedianStepsToFirstViolation *float64        `json:"median_steps_to_first_violation"`
	FirstQuartileSteps          *float64        `json:"first_quartile_steps_to_first_violation"`
	ThirdQuartileSteps          *float64        `json:"third_quartile_steps_to_first_violation"`
	SurvivalCurve               []survivalPoint `json:"survival_curve,omitempty"`
	ViolationRate               *float64        `json:"violation_rate"`
	TotalSteps                  int             `json:"total_steps"`
	TotalActions                int             `json:"total_actions"`
	UnattributedActions         int             `json:"unattributed_actions"`
	TotalRunHours               float64         `json:"total_run_hours"`
	Detections                  int             `json:"detections"`
	DefectsPerThousandActions   *float64        `json:"defects_per_thousand_actions"`
	DefectsPerHour              *float64        `json:"defects_per_hour"`
	DistinctDefects             int             `json:"distinct_defects"`
	SingletonDefects            int             `json:"singleton_defects"`
	SingletonFraction           *float64        `json:"singleton_fraction"`
	DefectRunCounts             map[string]int  `json:"defect_run_counts,omitempty"`
}

type pairwiseResult struct {
	First      string  `json:"first"`
	Second     string  `json:"second"`
	FirstSize  int     `json:"first_size"`
	SecondSize int     `json:"second_size"`
	Statistic  float64 `json:"u"`
	A12        float64 `json:"a12"`
	// Unordered is how many of the run pairs behind U and A12 have no order
	// between them, because they were tied or because censoring stopped one run
	// before the other violated. Each of them counts as half, so it is also how
	// much of the effect size is the null value rather than an observation.
	Unordered  int     `json:"unordered_pairs"`
	PValue     float64 `json:"p_value"`
	HolmPValue float64 `json:"holm_p_value"`
}

type analysis struct {
	GeneratedAt time.Time `json:"generated_at"`
	Outcome     string    `json:"outcome"`
	// Question names the family Holm corrects within. The correction is applied
	// across the comparisons of one research question and never across the
	// paper, so the family a p-value was adjusted in has to be recorded next to
	// it rather than left to the reader to reconstruct.
	Question       string            `json:"question,omitempty"`
	HolmFamilySize int               `json:"holm_family_size"`
	Arms           []armSummary      `json:"arms"`
	LogRank        *logRankResult    `json:"log_rank"`
	Pairwise       []pairwiseResult  `json:"pairwise"`
	Paired         *pairedComparison `json:"paired,omitempty"`
	Notes          []string          `json:"notes,omitempty"`
}

const outcomeDescription = "steps to first violation, right-censored at the last step a clean run reached"

func analyse(arms []arm, now time.Time) (analysis, error) {
	result, testable, err := baseAnalysis(arms, now)
	if err != nil {
		return analysis{}, err
	}
	if len(testable) >= 2 {
		result.Pairwise = comparePairs(testable)
		result.HolmFamilySize = countCorrected(result.Pairwise)
	}
	return result, nil
}

// analysePaired is the seed-matched design of the actuation ablation: two arms
// running the same seeds, contrasted seed by seed rather than as two
// independent samples.
func analysePaired(arms []arm, now time.Time) (analysis, error) {
	result, testable, err := baseAnalysis(arms, now)
	if err != nil {
		return analysis{}, err
	}
	if len(testable) != 2 {
		return analysis{}, fmt.Errorf("a paired comparison needs exactly two arms with usable runs, found %d", len(testable))
	}
	comparison, err := pairArms(testable[0], testable[1])
	if err != nil {
		return analysis{}, err
	}
	if comparison.Pairs == 0 {
		return analysis{}, fmt.Errorf("arms %q and %q share no seed with a usable run in both",
			testable[0].Name, testable[1].Name)
	}
	if comparison.PValue != nil {
		adjusted := holm([]float64{*comparison.PValue})[0]
		comparison.HolmPValue = &adjusted
		result.HolmFamilySize = 1
	}
	result.Paired = &comparison
	return result, nil
}

func baseAnalysis(arms []arm, now time.Time) (analysis, []arm, error) {
	result := analysis{GeneratedAt: now, Outcome: outcomeDescription}
	for _, current := range arms {
		result.Arms = append(result.Arms, summarize(current))
	}

	var testable []arm
	for _, current := range arms {
		if len(current.observations()) > 0 {
			testable = append(testable, current)
		}
	}
	if len(testable) < len(arms) {
		result.Notes = append(result.Notes,
			"arms with no usable runs are reported but left out of the log-rank test and the pairwise comparisons")
	}
	if err := sameBudget(testable); err != nil {
		return analysis{}, nil, err
	}
	if err := sameAttribution(testable); err != nil {
		return analysis{}, nil, err
	}
	if len(testable) >= 2 {
		names := make([]string, len(testable))
		groups := make([][]observation, len(testable))
		for index, current := range testable {
			names[index] = current.Name
			groups[index] = current.observations()
		}
		test := logRank(names, groups)
		result.LogRank = &test
	}
	return result, testable, nil
}

// sameBudget refuses arms that were given different exposure. A clean run is
// censored somewhere at or below its arm's budget, so the arm with the larger
// budget carries censored runs the smaller arm could not have produced, and
// every test that ranks the two against each other reads that as the arm
// surviving longer. It is the cross-arm form of what groupArms already refuses
// within one arm.
func sameBudget(arms []arm) error {
	for index := 1; index < len(arms); index++ {
		if arms[index].Budget != arms[0].Budget {
			return fmt.Errorf("arm %q has step budget %d and arm %q has %d: "+
				"runs censored at different budgets cannot be compared",
				arms[0].Name, arms[0].Budget, arms[index].Name, arms[index].Budget)
		}
	}
	return nil
}

// sameAttribution refuses arms whose actions were counted against different
// denominators. An arm recorded before an action named its producer counts
// whatever the spec's setup dispatched among its actions, and an arm recorded
// after leaves the login out, so the same rate over the two divides by
// different things and the tests rank a bookkeeping difference. Two arms of the
// same unknown provenance are diluted alike and compare; one of each does not.
func sameAttribution(arms []arm) error {
	for index := 1; index < len(arms); index++ {
		unknown, attributed := arms[0], arms[index]
		if (unknown.unattributedActions() == 0) == (attributed.unattributedActions() == 0) {
			continue
		}
		if unknown.unattributedActions() == 0 {
			unknown, attributed = attributed, unknown
		}
		return fmt.Errorf("arm %q counts %d action(s) of unknown provenance and arm %q counts none: "+
			"a denominator that may include the spec's setup cannot be compared against one that excludes it",
			unknown.Name, unknown.unattributedActions(), attributed.Name)
	}
	return nil
}

func countCorrected(pairs []pairwiseResult) int {
	corrected := 0
	for _, pair := range pairs {
		if !math.IsNaN(pair.PValue) {
			corrected++
		}
	}
	return corrected
}

func comparePairs(arms []arm) []pairwiseResult {
	var pairs []pairwiseResult
	for first := 0; first < len(arms); first++ {
		for second := first + 1; second < len(arms); second++ {
			test := gehanTest(arms[first].observations(), arms[second].observations())
			pairs = append(pairs, pairwiseResult{
				First:      arms[first].Name,
				Second:     arms[second].Name,
				FirstSize:  test.FirstSize,
				SecondSize: test.SecondSize,
				Statistic:  test.Statistic,
				A12:        test.A12,
				Unordered:  test.Unordered,
				PValue:     test.PValue,
				HolmPValue: math.NaN(),
			})
		}
	}
	// Holm runs over this one family of comparisons. A comparison whose p-value
	// could not be computed is not part of the family and does not shrink the
	// correction the others receive.
	var family []int
	var raw []float64
	for index, pair := range pairs {
		if math.IsNaN(pair.PValue) {
			continue
		}
		family = append(family, index)
		raw = append(raw, pair.PValue)
	}
	for position, adjusted := range holm(raw) {
		pairs[family[position]].HolmPValue = adjusted
	}
	return pairs
}

func summarize(current arm) armSummary {
	summary := armSummary{
		Arm:                 current.Name,
		Generator:           current.Generator,
		Platform:            current.Platform,
		StepBudget:          current.Budget,
		Directories:         current.Directories,
		Recorded:            len(current.Runs),
		MissingSeeds:        current.MissingSeeds,
		UnattributedActions: current.unattributedActions(),
	}
	runsPerDefect := map[string]int{}
	for _, item := range current.Runs {
		if item.ExcludedBecause != "" {
			summary.Excluded++
			if summary.ExcludedByReason == nil {
				summary.ExcludedByReason = map[string]int{}
			}
			summary.ExcludedByReason[item.ExcludedBecause]++
			continue
		}
		summary.Usable++
		summary.TotalSteps += item.Steps
		// Steps and actions differ by the steps that chose no action, the steps
		// whose action was never dispatched, and the steps the spec's setup
		// drove into position. Only what the action generator dispatched
		// explored the app, so only that belongs in a per-action rate.
		summary.TotalActions += item.Actions
		summary.TotalRunHours += float64(item.MonotonicMillis) / float64(time.Hour/time.Millisecond)
		if item.ClampedToBudget {
			summary.EventsHeldAtBudget++
		}
		if item.Violated && item.EventStep > item.OriginStep {
			summary.EventsDetectedAfterOrigin++
		}
		if item.Violated {
			summary.Violated++
		} else {
			summary.Censored++
		}
		distinct := slices.Compact(slices.Sorted(slices.Values(item.ViolatedProperties)))
		summary.Detections += len(distinct)
		for _, property := range distinct {
			runsPerDefect[property]++
		}
	}

	summary.SurvivalCurve = kaplanMeier(current.observations())
	if median, ok := medianSurvival(summary.SurvivalCurve); ok {
		summary.MedianStepsToFirstViolation = &median
	}
	if lower, ok := quantileSurvival(summary.SurvivalCurve, 0.25); ok {
		summary.FirstQuartileSteps = &lower
	}
	if upper, ok := quantileSurvival(summary.SurvivalCurve, 0.75); ok {
		summary.ThirdQuartileSteps = &upper
	}
	if summary.Usable > 0 {
		rate := float64(summary.Violated) / float64(summary.Usable)
		summary.ViolationRate = &rate
	}
	if summary.TotalActions > 0 {
		perThousand := 1000 * float64(summary.Detections) / float64(summary.TotalActions)
		summary.DefectsPerThousandActions = &perThousand
	}
	if summary.TotalRunHours > 0 {
		perHour := float64(summary.Detections) / summary.TotalRunHours
		summary.DefectsPerHour = &perHour
	}
	if len(runsPerDefect) > 0 {
		summary.DefectRunCounts = runsPerDefect
		summary.DistinctDefects = len(runsPerDefect)
		for _, count := range runsPerDefect {
			if count == 1 {
				summary.SingletonDefects++
			}
		}
		fraction := float64(summary.SingletonDefects) / float64(summary.DistinctDefects)
		summary.SingletonFraction = &fraction
	}
	return summary
}
