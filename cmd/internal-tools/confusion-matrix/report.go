package main

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
)

// labelledMatrix is one row of a printed matrix: the whole sample, or one model.
type labelledMatrix struct {
	Label    string
	Value    clauseMatrix
	Excluded int
}

func writeReport(outcome result, out io.Writer) {
	fmt.Fprintln(out, "unit: implementation whose own suite passed, scored on whether a property fired and on the reviewer's overall verdict")
	fmt.Fprintln(out, "an implementation with no cell is missing data and is listed separately, never counted as a clean run")
	fmt.Fprintln(out)
	writeTable(out, []string{"group", "scored", "true pos", "false pos", "false neg", "true neg", "precision", "recall", "no cell"},
		func(add func(...string)) {
			for _, row := range implementationRows(outcome) {
				add(
					row.Label,
					strconv.Itoa(row.Value.Scored),
					strconv.Itoa(row.Value.TruePositive),
					strconv.Itoa(row.Value.FalsePositive),
					strconv.Itoa(row.Value.FalseNegative),
					strconv.Itoa(row.Value.TrueNegative),
					formatRatio(row.Value.Precision),
					formatRatio(row.Value.Recall),
					strconv.Itoa(row.Excluded),
				)
			}
		})

	fmt.Fprintln(out)
	fmt.Fprintln(out, "clause pairs are an implementation against one of R1 to R20, scored through the declared property-to-clause mapping")
	fmt.Fprintln(out, "cannot tell is the reviewer's specification-error answer and is evidence neither way")
	fmt.Fprintln(out, "unevaluated is a clause whose every covering property a never-located surface left unrunnable: a portability miss, not a cell")
	writeTable(out, []string{"group", "scored", "true pos", "false pos", "false neg", "true neg",
		"precision", "recall", "cannot tell", "unevaluated", "uncovered", "uncovered false neg"},
		func(add func(...string)) {
			for _, row := range clauseRows(outcome) {
				add(
					row.Label,
					strconv.Itoa(row.Value.Scored),
					strconv.Itoa(row.Value.TruePositive),
					strconv.Itoa(row.Value.FalsePositive),
					strconv.Itoa(row.Value.FalseNegative),
					strconv.Itoa(row.Value.TrueNegative),
					formatRatio(row.Value.Precision),
					formatRatio(row.Value.Recall),
					strconv.Itoa(row.Value.CannotTell),
					strconv.Itoa(row.Value.UnevaluatedSurfaceMissed),
					strconv.Itoa(row.Value.UncoveredScored),
					strconv.Itoa(row.Value.UncoveredFalseNegative),
				)
			}
		})

	fmt.Fprintln(out)
	writeTable(out, []string{"implementation", "model", "cell", "usable runs", "fired", "violated clauses", "cannot tell"},
		func(add func(...string)) {
			for _, row := range outcome.Outcomes {
				add(
					row.Implementation,
					row.Model,
					row.Cell,
					strconv.Itoa(row.RunsUsable),
					joinOrDash(row.FiredProperties),
					joinOrDash(row.ViolatedClauses),
					strconv.Itoa(len(row.CannotTellClauses)),
				)
			}
		})

	if len(outcome.Excluded) > 0 {
		fmt.Fprintf(out, "\n%d implementation(s) carry no cell: missing data, never a clean run\n", len(outcome.Excluded))
		writeTable(out, []string{"implementation", "model", "reason", "detail"}, func(add func(...string)) {
			for _, entry := range outcome.Excluded {
				add(entry.Implementation, orDash(entry.Model), entry.Reason, orDash(entry.Detail))
			}
		})
	}

	fmt.Fprintf(out, "\nspecification portability over the %d scored implementation(s): %d needed a locating adaptation\n",
		outcome.Portability.Scored, outcome.Portability.WithUnlocatableSurface)
	if len(outcome.Portability.BySurface) > 0 {
		writeTable(out, []string{"surface never located", "implementations"}, func(add func(...string)) {
			for _, surface := range slices.Sorted(maps.Keys(outcome.Portability.BySurface)) {
				add(surface, strconv.Itoa(outcome.Portability.BySurface[surface]))
			}
		})
	}
	if len(outcome.Portability.SurfacesReadAsInconclusive) > 0 {
		fmt.Fprintf(out, "a miss on %s cannot be told from that surface being legitimately absent, so neither counts against portability\n",
			strings.Join(outcome.Portability.SurfacesReadAsInconclusive, ", "))
	}

	fmt.Fprintf(out, "\nclause coverage: %d mapped propert(ies) cover %d of %d clauses\n",
		outcome.Coverage.MappedProperties, len(outcome.Coverage.ClausesCovered), clauseCount)
	if len(outcome.Coverage.ClausesUncovered) > 0 {
		fmt.Fprintf(out, "no property covers %s\n", strings.Join(outcome.Coverage.ClausesUncovered, ", "))
	}
	fmt.Fprintf(out, "review cost over the scored implementations: %d minutes\n", outcome.ReviewMinutes)

	for _, note := range outcome.Notes {
		fmt.Fprintf(out, "\nnote: %s\n", note)
	}
}

func implementationRows(outcome result) []labelledMatrix {
	rows := []labelledMatrix{{
		Label:    "all",
		Value:    clauseMatrix{matrix: outcome.Implementations},
		Excluded: len(outcome.Excluded),
	}}
	for _, model := range outcome.ByModel {
		rows = append(rows, labelledMatrix{
			Label:    model.Model,
			Value:    clauseMatrix{matrix: model.Implementations},
			Excluded: model.Excluded,
		})
	}
	return rows
}

func clauseRows(outcome result) []labelledMatrix {
	rows := []labelledMatrix{{Label: "all", Value: outcome.Clauses}}
	for _, model := range outcome.ByModel {
		rows = append(rows, labelledMatrix{Label: model.Model, Value: model.Clauses})
	}
	return rows
}

func writeTable(out io.Writer, header []string, rows func(add func(...string))) {
	writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, strings.Join(header, "\t"))
	rows(func(cells ...string) {
		fmt.Fprintln(writer, strings.Join(cells, "\t"))
	})
	writer.Flush()
}

func formatRatio(value *float64) string {
	if value == nil {
		return "n/a"
	}
	return strconv.FormatFloat(*value, 'f', 3, 64)
}

func joinOrDash(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, " ")
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
