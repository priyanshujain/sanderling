package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// The four cells of the pre-registered measure, named as
// model-implementations.md describes them.
const (
	cellTruePositive  = "checker fired, review confirmed"
	cellFalsePositive = "checker fired, review found nothing"
	cellFalseNegative = "checker silent, review found a defect"
	cellTrueNegative  = "checker silent, review found nothing"
)

// Reasons an implementation is missing data rather than a cell of the matrix.
const (
	missingSweepStage    = "sweep stopped before any run"
	missingNoUsableRun   = "no usable run"
	missingNoSweepRecord = "no sweep record"
	missingNoReview      = "no verdict filed"
	missingMalformed     = "malformed verdict form"
	missingSurfaces      = "surface locatability unknown"
	missingNoModel       = "not in the assignment mapping"
)

type matrix struct {
	Unit          string   `json:"unit"`
	Scored        int      `json:"scored"`
	TruePositive  int      `json:"true_positive"`
	FalsePositive int      `json:"false_positive"`
	FalseNegative int      `json:"false_negative"`
	TrueNegative  int      `json:"true_negative"`
	Precision     *float64 `json:"precision"`
	Recall        *float64 `json:"recall"`
}

func (m *matrix) add(checkerPositive, humanPositive bool) {
	m.Scored++
	switch {
	case checkerPositive && humanPositive:
		m.TruePositive++
	case checkerPositive:
		m.FalsePositive++
	case humanPositive:
		m.FalseNegative++
	default:
		m.TrueNegative++
	}
}

func (m *matrix) finish() {
	m.Precision = ratio(m.TruePositive, m.TruePositive+m.FalsePositive)
	m.Recall = ratio(m.TruePositive, m.TruePositive+m.FalseNegative)
}

func ratio(numerator, denominator int) *float64 {
	if denominator == 0 {
		return nil
	}
	value := float64(numerator) / float64(denominator)
	return &value
}

// clauseMatrix scores one implementation-clause pair. Its three side buckets
// hold the pairs that are not evidence either way: a clause the reviewer could
// not judge, a clause whose every covering property was unevaluable because a
// surface was never located, and, tracked but still scored, a clause no
// property covers at all.
type clauseMatrix struct {
	matrix
	CannotTell               int `json:"cannot_tell"`
	UnevaluatedSurfaceMissed int `json:"unevaluated_surface_missed"`
	UncoveredScored          int `json:"uncovered_clause_pairs_scored"`
	UncoveredFalseNegative   int `json:"uncovered_clause_false_negatives"`
}

type implementationOutcome struct {
	Implementation        string   `json:"implementation"`
	Model                 string   `json:"model"`
	Cell                  string   `json:"cell"`
	FiredProperties       []string `json:"fired_properties,omitempty"`
	ViolatedClauses       []string `json:"violated_clauses,omitempty"`
	CannotTellClauses     []string `json:"cannot_tell_clauses,omitempty"`
	UnlocatableSurfaces   []string `json:"unlocatable_surfaces,omitempty"`
	UnevaluatedProperties []string `json:"unevaluated_properties,omitempty"`
	// DefectOnlyOnUnevaluatedClauses marks a false negative the checker was
	// never in a position to catch: every clause the reviewer faulted is
	// covered only by properties a missing surface left unevaluable. It is a
	// portability miss reported beside the matrix, never inside it.
	DefectOnlyOnUnevaluatedClauses bool `json:"defect_only_on_unevaluated_clauses,omitempty"`
	RunsUsable                     int  `json:"runs_usable"`
	ReviewMinutes                  int  `json:"review_minutes,omitempty"`
}

type exclusion struct {
	Implementation string `json:"implementation"`
	Model          string `json:"model,omitempty"`
	Reason         string `json:"reason"`
	Detail         string `json:"detail,omitempty"`
}

type modelBreakdown struct {
	Model                          string       `json:"model"`
	Implementations                matrix       `json:"implementation_matrix"`
	Clauses                        clauseMatrix `json:"clause_matrix"`
	Excluded                       int          `json:"excluded"`
	DefectOnlyOnUnevaluatedClauses int          `json:"defect_only_on_unevaluated_clauses"`
}

type portability struct {
	Scored                     int            `json:"implementations_scored"`
	WithUnlocatableSurface     int            `json:"implementations_with_an_unlocatable_surface"`
	BySurface                  map[string]int `json:"implementations_by_unlocatable_surface,omitempty"`
	SurfacesReadAsInconclusive []string       `json:"surfaces_a_miss_cannot_be_read_from,omitempty"`
}

type coverage struct {
	MappedProperties         int      `json:"mapped_properties"`
	MappingTodoRows          int      `json:"mapping_todo_rows"`
	ClausesCovered           []string `json:"clauses_covered,omitempty"`
	ClausesUncovered         []string `json:"clauses_no_property_covers,omitempty"`
	FiredPropertiesNotMapped []string `json:"fired_properties_not_in_the_mapping,omitempty"`
}

type result struct {
	GeneratedAt      time.Time               `json:"generated_at"`
	SweepDirectory   string                  `json:"sweep_directory"`
	ReviewsDirectory string                  `json:"reviews_directory"`
	MappingPath      string                  `json:"property_clause_mapping"`
	SpecPath         string                  `json:"spec_path,omitempty"`
	Implementations  matrix                  `json:"implementation_matrix"`
	Clauses          clauseMatrix            `json:"clause_matrix"`
	ByModel          []modelBreakdown        `json:"by_model"`
	Outcomes         []implementationOutcome `json:"outcomes"`
	Excluded         []exclusion             `json:"excluded,omitempty"`
	Portability      portability             `json:"portability"`
	ReviewMinutes    int                     `json:"review_minutes_over_scored_implementations"`
	Coverage         coverage                `json:"clause_coverage"`
	Notes            []string                `json:"notes,omitempty"`
}

func crossTabulate(
	checker checkerSide,
	reviews reviewSide,
	assignments map[string]string,
	declared mapping,
	now time.Time,
) result {
	outcome := result{
		GeneratedAt:      now,
		SweepDirectory:   checker.Directory,
		ReviewsDirectory: reviews.Directory,
		MappingPath:      declared.Path,
		SpecPath:         checker.SpecPath,
		Implementations:  matrix{Unit: "implementation"},
		Clauses:          clauseMatrix{matrix: matrix{Unit: "implementation-clause pair"}},
		Portability:      portability{BySurface: map[string]int{}},
	}

	reviewByName := map[string]reviewVerdict{}
	for _, verdict := range reviews.Verdicts {
		reviewByName[verdict.Implementation] = verdict
	}
	malformedByName := map[string]malformedReview{}
	for _, entry := range reviews.Malformed {
		malformedByName[entry.Implementation] = entry
	}
	checkerByName := map[string]checkerVerdict{}
	for _, verdict := range checker.Verdicts {
		checkerByName[verdict.Implementation] = verdict
	}

	byModel := map[string]*modelBreakdown{}
	modelOf := func(name string) string { return assignments[name] }
	breakdown := func(model string) *modelBreakdown {
		current, seen := byModel[model]
		if !seen {
			current = &modelBreakdown{
				Model:           model,
				Implementations: matrix{Unit: "implementation"},
				Clauses:         clauseMatrix{matrix: matrix{Unit: "implementation-clause pair"}},
			}
			byModel[model] = current
		}
		return current
	}

	unmappedFired := map[string]bool{}
	for _, name := range implementationNames(checker, reviews, assignments) {
		model := modelOf(name)
		verdict, swept := checkerByName[name]
		review, reviewed := reviewByName[name]

		if reason, detail := missingData(name, model, verdict, swept, reviewed, malformedByName); reason != "" {
			outcome.Excluded = append(outcome.Excluded, exclusion{
				Implementation: name, Model: model, Reason: reason, Detail: detail,
			})
			if model != "" {
				breakdown(model).Excluded++
			}
			continue
		}

		for _, property := range verdict.FiredProperties {
			if !declared.knows(property) {
				unmappedFired[property] = true
			}
		}
		row := scoreImplementation(verdict, review, declared)
		row.Model = model
		outcome.Outcomes = append(outcome.Outcomes, row)

		modelRow := breakdown(model)
		outcome.Implementations.add(verdict.fired(), review.defective())
		modelRow.Implementations.add(verdict.fired(), review.defective())
		scoreClauses(&outcome.Clauses, &modelRow.Clauses, verdict, review, declared)

		if row.DefectOnlyOnUnevaluatedClauses {
			modelRow.DefectOnlyOnUnevaluatedClauses++
		}
		outcome.Portability.Scored++
		outcome.ReviewMinutes += review.Minutes
		if len(row.UnlocatableSurfaces) > 0 {
			outcome.Portability.WithUnlocatableSurface++
			for _, surface := range row.UnlocatableSurfaces {
				outcome.Portability.BySurface[surface]++
			}
		}
	}

	outcome.Implementations.finish()
	outcome.Clauses.finish()
	for _, model := range capabilityOrder {
		current, seen := byModel[model]
		if !seen {
			continue
		}
		current.Implementations.finish()
		current.Clauses.finish()
		outcome.ByModel = append(outcome.ByModel, *current)
	}

	outcome.Coverage = describeCoverage(declared, unmappedFired)
	outcome.Portability.SurfacesReadAsInconclusive = inconclusiveSurfaces(declared)
	outcome.Notes = buildNotes(outcome, declared, reviews)
	return outcome
}

func implementationNames(checker checkerSide, reviews reviewSide, assignments map[string]string) []string {
	names := map[string]bool{}
	for _, planned := range checker.Planned {
		names[planned] = true
	}
	for _, verdict := range checker.Verdicts {
		names[verdict.Implementation] = true
	}
	for _, verdict := range reviews.Verdicts {
		names[verdict.Implementation] = true
	}
	for _, entry := range reviews.Malformed {
		names[entry.Implementation] = true
	}
	for name := range assignments {
		names[name] = true
	}
	return slices.Sorted(maps.Keys(names))
}

// missingData names why an implementation carries no cell. A build that never
// finished, a run that never produced a usable campaign and a verdict that was
// never filed are all absent evidence: scoring any of them as a clean run would
// read the gap as agreement.
func missingData(
	name string,
	model string,
	verdict checkerVerdict,
	swept bool,
	reviewed bool,
	malformed map[string]malformedReview,
) (string, string) {
	if model == "" {
		return missingNoModel, ""
	}
	if !swept {
		return missingNoSweepRecord, ""
	}
	if verdict.FailedStage != "" {
		return missingSweepStage, fmt.Sprintf("%s: %s", verdict.FailedStage, verdict.FailedError)
	}
	if verdict.RunsUsable == 0 {
		return missingNoUsableRun, excludedSummary(verdict)
	}
	if entry, broken := malformed[name]; broken {
		return missingMalformed, entry.Reason
	}
	if !reviewed {
		return missingNoReview, ""
	}
	if !verdict.SurfacesKnown {
		return missingSurfaces, strings.Join(verdict.TraceErrors, "; ")
	}
	return "", ""
}

func excludedSummary(verdict checkerVerdict) string {
	if len(verdict.ExcludedByReason) == 0 {
		return ""
	}
	var parts []string
	for _, reason := range slices.Sorted(maps.Keys(verdict.ExcludedByReason)) {
		parts = append(parts, fmt.Sprintf("%s=%d", reason, verdict.ExcludedByReason[reason]))
	}
	return strings.Join(parts, ", ")
}

func scoreImplementation(verdict checkerVerdict, review reviewVerdict, declared mapping) implementationOutcome {
	row := implementationOutcome{
		Implementation:  verdict.Implementation,
		Cell:            cellOf(verdict.fired(), review.defective()),
		FiredProperties: verdict.FiredProperties,
		ViolatedClauses: review.violatedClauses(),
		RunsUsable:      verdict.RunsUsable,
		ReviewMinutes:   review.Minutes,
	}
	for _, clause := range allClauses() {
		if review.Clauses[clause] == clauseCannotTell {
			row.CannotTellClauses = append(row.CannotTellClauses, clause)
		}
	}
	for _, surface := range declared.unlocatableSurfaces() {
		if !verdict.SurfacesObserved[surface] {
			row.UnlocatableSurfaces = append(row.UnlocatableSurfaces, surface)
		}
	}
	for _, property := range declared.Properties {
		if declared.unevaluable(property.Property, verdict.SurfacesObserved) {
			row.UnevaluatedProperties = append(row.UnevaluatedProperties, property.Property)
		}
	}
	row.DefectOnlyOnUnevaluatedClauses = attributableToUnevaluated(verdict, review, declared)
	return row
}

// attributableToUnevaluated reports a false negative the checker could not have
// caught: it fired nothing, the reviewer faulted at least one clause, and every
// clause the reviewer faulted is covered only by properties a missing surface
// left unevaluable.
func attributableToUnevaluated(verdict checkerVerdict, review reviewVerdict, declared mapping) bool {
	if verdict.fired() || !review.defective() {
		return false
	}
	violated := review.violatedClauses()
	if len(violated) == 0 {
		return false
	}
	for _, clause := range violated {
		covering := declared.covering(clause)
		if len(covering) == 0 {
			return false
		}
		for _, property := range covering {
			if !declared.unevaluable(property, verdict.SurfacesObserved) {
				return false
			}
		}
	}
	return true
}

func cellOf(checkerPositive, humanPositive bool) string {
	switch {
	case checkerPositive && humanPositive:
		return cellTruePositive
	case checkerPositive:
		return cellFalsePositive
	case humanPositive:
		return cellFalseNegative
	default:
		return cellTrueNegative
	}
}

func scoreClauses(overall, model *clauseMatrix, verdict checkerVerdict, review reviewVerdict, declared mapping) {
	fired := map[string]bool{}
	for _, property := range verdict.FiredProperties {
		fired[property] = true
	}
	for _, clause := range allClauses() {
		label := review.Clauses[clause]
		if label == clauseCannotTell {
			overall.CannotTell++
			model.CannotTell++
			continue
		}
		covering := declared.covering(clause)
		checkerPositive := false
		for _, property := range covering {
			if fired[property] {
				checkerPositive = true
				break
			}
		}
		if !checkerPositive && len(covering) > 0 && allUnevaluable(covering, verdict, declared) {
			overall.UnevaluatedSurfaceMissed++
			model.UnevaluatedSurfaceMissed++
			continue
		}
		humanPositive := label == clauseViolates
		overall.add(checkerPositive, humanPositive)
		model.add(checkerPositive, humanPositive)
		if len(covering) == 0 {
			overall.UncoveredScored++
			model.UncoveredScored++
			if humanPositive {
				overall.UncoveredFalseNegative++
				model.UncoveredFalseNegative++
			}
		}
	}
}

func allUnevaluable(covering []string, verdict checkerVerdict, declared mapping) bool {
	for _, property := range covering {
		if !declared.unevaluable(property, verdict.SurfacesObserved) {
			return false
		}
	}
	return true
}

func describeCoverage(declared mapping, unmappedFired map[string]bool) coverage {
	result := coverage{
		MappedProperties: len(declared.Properties),
		MappingTodoRows:  declared.PropertyTodoRows,
	}
	for _, clause := range allClauses() {
		if len(declared.covering(clause)) > 0 {
			result.ClausesCovered = append(result.ClausesCovered, clause)
			continue
		}
		result.ClausesUncovered = append(result.ClausesUncovered, clause)
	}
	if len(unmappedFired) > 0 {
		result.FiredPropertiesNotMapped = slices.Sorted(maps.Keys(unmappedFired))
	}
	return result
}

func inconclusiveSurfaces(declared mapping) []string {
	var names []string
	for _, surface := range declared.Surfaces {
		if surface.NeverObserved == surfaceInconclusive {
			names = append(names, surface.Surface)
		}
	}
	return names
}

func buildNotes(outcome result, declared mapping, reviews reviewSide) []string {
	var notes []string
	if len(declared.Properties) == 0 {
		notes = append(notes, fmt.Sprintf(
			"%s maps no property to a clause, so the clause matrix is empty and no portability miss can be detected; "+
				"the implementation matrix below stands on its own", declared.Path))
	}
	if declared.PropertyTodoRows > 0 {
		notes = append(notes, fmt.Sprintf("%s still carries %d TODO row(s) in its property table",
			declared.Path, declared.PropertyTodoRows))
	}
	if len(outcome.Coverage.FiredPropertiesNotMapped) > 0 {
		notes = append(notes, fmt.Sprintf(
			"%d fired propert(ies) are absent from the mapping and could not be attributed to a clause: %s",
			len(outcome.Coverage.FiredPropertiesNotMapped),
			strings.Join(outcome.Coverage.FiredPropertiesNotMapped, ", ")))
	}
	for _, verdict := range reviews.Verdicts {
		if !verdict.defective() && len(verdict.violatedClauses()) > 0 {
			notes = append(notes, fmt.Sprintf(
				"%s files %d violating clause(s) under an overall verdict of %s; the overall verdict is what the matrix scores",
				verdict.Implementation, len(verdict.violatedClauses()), overallNotDefective))
		}
		if len(verdict.Adjudicated) > 0 {
			notes = append(notes, fmt.Sprintf("%s uses the adjudicated label for %s",
				verdict.Implementation, strings.Join(verdict.Adjudicated, ", ")))
		}
	}
	if count := attributedFalseNegatives(outcome); count > 0 {
		notes = append(notes, fmt.Sprintf(
			"%d false negative(s) fault only clauses whose every property a missing surface left unevaluable: "+
				"those are portability misses, not blind spots", count))
	}
	notes = append(notes, "second-rater agreement and Cohen's kappa are not computed here; "+
		"an impl-NN-adjudication.md is read and its resolved labels replace the first rater's")
	return notes
}

func attributedFalseNegatives(outcome result) int {
	count := 0
	for _, row := range outcome.Outcomes {
		if row.DefectOnlyOnUnevaluatedClauses {
			count++
		}
	}
	return count
}
