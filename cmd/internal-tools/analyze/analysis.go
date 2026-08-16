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
	Statistic  float64 `json:"mann_whitney_u"`
	A12        float64 `json:"a12"`
	PValue     float64 `json:"p_value"`
	HolmPValue float64 `json:"holm_p_value"`
	Exact      bool    `json:"exact"`
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

const outcomeDescription = "steps to first violation, right-censored at the step budget"

func analyse(arms []arm, now time.Time) analysis {
	result, testable := baseAnalysis(arms, now)
	if len(testable) >= 2 {
		result.Pairwise = comparePairs(testable)
		result.HolmFamilySize = countCorrected(result.Pairwise)
	}
	return result
}

// analysePaired is the seed-matched design of the actuation ablation: two arms
// running the same seeds, contrasted seed by seed rather than as two
// independent samples.
func analysePaired(arms []arm, now time.Time) (analysis, error) {
	result, testable := baseAnalysis(arms, now)
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
	if !math.IsNaN(comparison.PValue) {
		comparison.HolmPValue = holm([]float64{comparison.PValue})[0]
		result.HolmFamilySize = 1
	}
	result.Paired = &comparison
	return result, nil
}

func baseAnalysis(arms []arm, now time.Time) (analysis, []arm) {
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
	return result, testable
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
			test := rankSum(arms[first].stepTimes(), arms[second].stepTimes())
			pairs = append(pairs, pairwiseResult{
				First:      arms[first].Name,
				Second:     arms[second].Name,
				FirstSize:  test.FirstSize,
				SecondSize: test.SecondSize,
				Statistic:  test.Statistic,
				A12:        test.A12,
				PValue:     test.PValue,
				HolmPValue: math.NaN(),
				Exact:      test.Exact,
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
		Arm:          current.Name,
		Generator:    current.Generator,
		Platform:     current.Platform,
		StepBudget:   current.Budget,
		Directories:  current.Directories,
		Recorded:     len(current.Runs),
		MissingSeeds: current.MissingSeeds,
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
		// Steps and actions differ by the steps that chose no action and the
		// steps whose action was never dispatched. Only dispatched actions
		// exercised the app, so only they belong in a per-action rate.
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
