package main

import (
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
)

func writeReport(result analysis, out io.Writer) {
	fmt.Fprintf(out, "primary outcome: %s\n\n", result.Outcome)

	writeTable(out, []string{"arm", "runs", "violated", "censored", "excluded", "missing", "median steps", "iqr steps", "violation rate"},
		func(add func(...string)) {
			for _, summary := range result.Arms {
				add(
					summary.Arm,
					strconv.Itoa(summary.Usable),
					strconv.Itoa(summary.Violated),
					strconv.Itoa(summary.Censored),
					strconv.Itoa(summary.Excluded),
					strconv.Itoa(len(summary.MissingSeeds)),
					formatMedian(summary.MedianStepsToFirstViolation),
					formatMedian(summary.FirstQuartileSteps)+" to "+formatMedian(summary.ThirdQuartileSteps),
					formatRatio(summary.ViolationRate, 3),
				)
			}
		})

	fmt.Fprintln(out)
	fmt.Fprintln(out, "a detection is one distinct property violated in one run; run hours sum the time the runs worked,")
	fmt.Fprintln(out, "on the monotonic clock, so a host that slept mid-run is not charged for the sleep")
	fmt.Fprintln(out, "actions count the steps that dispatched one; the rest chose nothing or had the choice thrown away")
	writeTable(out, []string{"arm", "steps", "actions", "run hours", "detections", "defects/1k actions", "defects/hour", "distinct defects", "found in one run"},
		func(add func(...string)) {
			for _, summary := range result.Arms {
				add(
					summary.Arm,
					strconv.Itoa(summary.TotalSteps),
					strconv.Itoa(summary.TotalActions),
					fmt.Sprintf("%.2f", summary.TotalRunHours),
					strconv.Itoa(summary.Detections),
					formatRatio(summary.DefectsPerThousandActions, 2),
					formatRatio(summary.DefectsPerHour, 2),
					strconv.Itoa(summary.DistinctDefects),
					formatSingletons(summary),
				)
			}
		})

	for _, summary := range result.Arms {
		if len(summary.ExcludedByReason) == 0 {
			continue
		}
		var parts []string
		for _, reason := range sortedKeys(summary.ExcludedByReason) {
			parts = append(parts, fmt.Sprintf("%s=%d", reason, summary.ExcludedByReason[reason]))
		}
		fmt.Fprintf(out, "\n%s excluded %d run(s) as missing data, not as censored observations: %s",
			summary.Arm, summary.Excluded, strings.Join(parts, ", "))
	}
	for _, summary := range result.Arms {
		if summary.EventsHeldAtBudget > 0 {
			fmt.Fprintf(out, "\n%s held %d violation(s) reported past the budget at %d steps",
				summary.Arm, summary.EventsHeldAtBudget, summary.StepBudget)
		}
	}
	for _, summary := range result.Arms {
		if summary.EventsDetectedAfterOrigin > 0 {
			fmt.Fprintf(out, "\n%s timed %d violation(s) at the step they were detected rather than the step that armed them, "+
				"which is what an obligation reported only when the run ended looks like",
				summary.Arm, summary.EventsDetectedAfterOrigin)
		}
	}
	if len(result.Arms) > 0 {
		fmt.Fprintln(out)
	}

	if result.LogRank != nil {
		fmt.Fprintf(out, "\nlog-rank across %d arms: chi-square %.4f on %d df, p %s\n",
			len(result.LogRank.Groups), result.LogRank.ChiSquare, result.LogRank.DegreesOfFreedom,
			formatPValue(result.LogRank.PValue))
		writeTable(out, []string{"arm", "n", "observed", "expected"}, func(add func(...string)) {
			for index, name := range result.LogRank.Groups {
				add(name,
					strconv.Itoa(result.LogRank.Sizes[index]),
					fmt.Sprintf("%.0f", result.LogRank.Observed[index]),
					fmt.Sprintf("%.2f", result.LogRank.Expected[index]))
			}
		})
	}

	if len(result.Pairwise) > 0 {
		fmt.Fprintln(out, "\npairwise wilcoxon rank-sum, censored runs held at the budget")
		fmt.Fprintln(out, "a12 above 0.5 means the first arm takes more steps to its first violation")
		writeTable(out, []string{"comparison", "n1", "n2", "u", "a12", "p", "holm p"}, func(add func(...string)) {
			for _, pair := range result.Pairwise {
				add(
					pair.First+" vs "+pair.Second,
					strconv.Itoa(pair.FirstSize),
					strconv.Itoa(pair.SecondSize),
					fmt.Sprintf("%.1f", pair.Statistic),
					fmt.Sprintf("%.3f", pair.A12),
					formatPValue(pair.PValue),
					formatPValue(pair.HolmPValue),
				)
			}
		})
	}

	for _, note := range result.Notes {
		fmt.Fprintf(out, "\nnote: %s\n", note)
	}
}

func sortedKeys(counts map[string]int) []string {
	return slices.Sorted(maps.Keys(counts))
}

func writeTable(out io.Writer, header []string, rows func(add func(...string))) {
	writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, strings.Join(header, "\t"))
	rows(func(cells ...string) {
		fmt.Fprintln(writer, strings.Join(cells, "\t"))
	})
	writer.Flush()
}

// formatMedian says undefined rather than substituting a mean, because a curve
// that never reaches one half has no median to report.
func formatMedian(value *float64) string {
	if value == nil {
		return "undefined"
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}

func formatRatio(value *float64, digits int) string {
	if value == nil {
		return "n/a"
	}
	return strconv.FormatFloat(*value, 'f', digits, 64)
}

func formatSingletons(summary armSummary) string {
	if summary.SingletonFraction == nil {
		return "n/a"
	}
	return fmt.Sprintf("%d/%d (%.3f)", summary.SingletonDefects, summary.DistinctDefects, *summary.SingletonFraction)
}

func formatPValue(value float64) string {
	switch {
	case math.IsNaN(value):
		return "n/a"
	case value < 1e-4:
		return fmt.Sprintf("%.3e", value)
	default:
		return fmt.Sprintf("%.4f", value)
	}
}
