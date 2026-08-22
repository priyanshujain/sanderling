package main

import (
	"strings"
	"testing"
)

func TestCrossTabulateScoresEachImplementationIntoOneCell(t *testing.T) {
	tests := []struct {
		name           string
		implementation fixtureImplementation
		wantCell       string
		wantExcluded   string
	}{
		{
			name: "checker and reviewer agree",
			implementation: fixtureImplementation{
				Name: "impl-01", Model: "Opus 5",
				Runs:   []fixtureRun{cleanRun(1, "serverHoldsEachMessageOnce"), cleanRun(2)},
				Review: &fixtureReview{Overall: overallDefective, Clauses: map[string]string{"R15": clauseViolates}},
			},
			wantCell: cellTruePositive,
		},
		{
			name: "checker fired and the reviewer found nothing",
			implementation: fixtureImplementation{
				Name: "impl-02", Model: "Sonnet 5",
				Runs:   []fixtureRun{cleanRun(1, "serverHoldsEachMessageOnce")},
				Review: &fixtureReview{Overall: overallNotDefective},
			},
			wantCell: cellFalsePositive,
		},
		{
			name: "reviewer found a defect no property fired on",
			implementation: fixtureImplementation{
				Name: "impl-03", Model: "Fable 5",
				Runs:   []fixtureRun{cleanRun(1), cleanRun(2)},
				Review: &fixtureReview{Overall: overallDefective, Clauses: map[string]string{"R17": clauseViolates}},
			},
			wantCell: cellFalseNegative,
		},
		{
			name: "both clean",
			implementation: fixtureImplementation{
				Name: "impl-04", Model: "Opus 5",
				Runs:   []fixtureRun{cleanRun(1), cleanRun(2)},
				Review: &fixtureReview{Overall: overallNotDefective},
			},
			wantCell: cellTrueNegative,
		},
		{
			name: "the build never finished",
			implementation: fixtureImplementation{
				Name: "impl-05", Model: "Sonnet 5", FailedStage: "build",
				Review: &fixtureReview{Overall: overallDefective, Clauses: map[string]string{"R11": clauseViolates}},
			},
			wantExcluded: missingSweepStage,
		},
		{
			name: "every run timed out",
			implementation: fixtureImplementation{
				Name: "impl-06", Model: "Fable 5",
				Runs:   []fixtureRun{{Seed: 1, ExitCode: -1, TimedOut: true}},
				Review: &fixtureReview{Overall: overallNotDefective},
			},
			wantExcluded: missingNoUsableRun,
		},
		{
			name: "every run spent its budget outside the app under test",
			implementation: fixtureImplementation{
				Name: "impl-10", Model: "Opus 5",
				Runs:   []fixtureRun{{Seed: 1, PreconditionFailures: 380}},
				Review: &fixtureReview{Overall: overallNotDefective},
			},
			wantExcluded: missingNoUsableRun,
		},
		{
			name: "no verdict was filed",
			implementation: fixtureImplementation{
				Name: "impl-07", Model: "Opus 5",
				Runs: []fixtureRun{cleanRun(1, "serverHoldsEachMessageOnce")},
			},
			wantExcluded: missingNoReview,
		},
		{
			name: "the verdict form is missing a clause",
			implementation: fixtureImplementation{
				Name: "impl-08", Model: "Sonnet 5",
				Runs: []fixtureRun{cleanRun(1)},
				RawReview: "reviewer: Jane\n\n| clause | verdict |\n| --- | --- |\n" +
					"| R1 | meets |\n| R2 | meets |\n\noverall: not defective\n",
			},
			wantExcluded: missingMalformed,
		},
		{
			name: "no trace says whether a surface was located",
			implementation: fixtureImplementation{
				Name: "impl-09", Model: "Fable 5",
				Runs:   []fixtureRun{{Seed: 1, NoTrace: true}},
				Review: &fixtureReview{Overall: overallNotDefective},
			},
			wantExcluded: missingSurfaces,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			emitted, stdout := runTool(t, writeFixture(t, []fixtureImplementation{test.implementation}, defaultMapping))
			if test.wantExcluded != "" {
				entry := exclusionFor(t, emitted, test.implementation.Name)
				if entry.Reason != test.wantExcluded {
					t.Fatalf("%s excluded as %q, want %q", test.implementation.Name, entry.Reason, test.wantExcluded)
				}
				if emitted.Implementations.Scored != 0 {
					t.Errorf("missing data scored %d implementation(s), want none: absent evidence is not a clean run",
						emitted.Implementations.Scored)
				}
				if !strings.Contains(stdout, "carry no cell") {
					t.Errorf("the report never separates the missing data from the matrix:\n%s", stdout)
				}
				return
			}
			row := outcomeFor(t, emitted, test.implementation.Name)
			if row.Cell != test.wantCell {
				t.Fatalf("%s landed in %q, want %q", test.implementation.Name, row.Cell, test.wantCell)
			}
			if emitted.Implementations.Scored != 1 {
				t.Errorf("scored %d implementation(s), want 1", emitted.Implementations.Scored)
			}
		})
	}
}

// TestACampaignThatDiedIsMissingDataNotATrueNegative covers the sweep it was
// interrupted on: the one seed the campaign got through wrote a clean run
// before the process died, and scoring the implementation on it reads the nine
// seeds that never ran as agreement between the checker and the reviewer.
func TestACampaignThatDiedIsMissingDataNotATrueNegative(t *testing.T) {
	interrupted := cleanRun(1)
	interrupted.CampaignExitCode = -1

	emitted, stdout := runTool(t, writeFixture(t, []fixtureImplementation{{
		Name: "impl-07", Model: "Opus 5",
		Runs:   []fixtureRun{interrupted},
		Review: &fixtureReview{Overall: overallNotDefective},
	}}, defaultMapping))

	entry := exclusionFor(t, emitted, "impl-07")
	if entry.Reason != missingNoUsableRun {
		t.Fatalf("impl-07 excluded as %q, want %q", entry.Reason, missingNoUsableRun)
	}
	if !strings.Contains(entry.Detail, reasonNonzeroExit) {
		t.Errorf("exclusion detail %q does not name %q", entry.Detail, reasonNonzeroExit)
	}
	if emitted.Implementations.TrueNegative != 0 || emitted.Implementations.Scored != 0 {
		t.Errorf("implementation matrix = %+v, want no cell: a dead campaign is absent evidence",
			emitted.Implementations)
	}
	if !strings.Contains(stdout, "carry no cell") {
		t.Errorf("the report never separates the dead campaign from the matrix:\n%s", stdout)
	}
}

// TestUnlocatableSurfaceIsNeitherAPositiveNorANegative pins the rule the
// pre-registration turns on: a clause whose every covering property a
// never-located surface left unrunnable is a portability miss. Counting it as a
// true positive credits the oracle for a property that never ran, and counting
// it as a false negative charges the oracle for a defect it was never in a
// position to see.
func TestUnlocatableSurfaceIsNeitherAPositiveNorANegative(t *testing.T) {
	built := writeFixture(t, []fixtureImplementation{{
		Name:  "impl-01",
		Model: "Opus 5",
		Runs: []fixtureRun{{
			Seed:     1,
			Violated: []string{"serverHoldsEachMessageOnce"},
			Surfaces: map[string]bool{"appRoot": true, "composer": true, "submit": true, "stateWords": false},
		}},
		Review: &fixtureReview{Overall: overallDefective, Clauses: map[string]string{
			"R5":  clauseViolates,
			"R15": clauseViolates,
		}},
	}}, defaultMapping)

	emitted, stdout := runTool(t, built)

	if got := emitted.Clauses.UnevaluatedSurfaceMissed; got != 1 {
		t.Errorf("clause matrix reports %d unevaluated pair(s), want 1 for R5 behind an unlocated stateWords", got)
	}
	if got := emitted.Clauses.TruePositive; got != 1 {
		t.Errorf("clause matrix reports %d true positive(s), want 1: only R15 had a property that ran and fired", got)
	}
	if got := emitted.Clauses.FalsePositive; got != 0 {
		t.Errorf("clause matrix reports %d false positive(s), want 0", got)
	}
	if got := emitted.Clauses.FalseNegative; got != 0 {
		t.Errorf("clause matrix reports %d false negative(s), want 0: R5 was never evaluated, so it was not missed", got)
	}
	if got := emitted.Clauses.Scored; got != clauseCount-1 {
		t.Errorf("clause matrix scored %d pair(s), want %d: the unevaluated pair is excluded, not scored", got, clauseCount-1)
	}
	if total := emitted.Clauses.TruePositive + emitted.Clauses.FalsePositive +
		emitted.Clauses.FalseNegative + emitted.Clauses.TrueNegative; total != emitted.Clauses.Scored {
		t.Errorf("the four cells sum to %d against %d scored: an excluded pair leaked into a cell", total, emitted.Clauses.Scored)
	}
	row := outcomeFor(t, emitted, "impl-01")
	if want := []string{"stateWords"}; !equalStrings(row.UnlocatableSurfaces, want) {
		t.Errorf("impl-01 reports unlocatable surfaces %v, want %v", row.UnlocatableSurfaces, want)
	}
	if want := []string{"sentOnlyAfterConfirmation"}; !equalStrings(row.UnevaluatedProperties, want) {
		t.Errorf("impl-01 reports unevaluated properties %v, want %v", row.UnevaluatedProperties, want)
	}
	if emitted.Portability.WithUnlocatableSurface != 1 {
		t.Errorf("portability counts %d implementation(s) needing a locating adaptation, want 1",
			emitted.Portability.WithUnlocatableSurface)
	}
	if !strings.Contains(stdout, "needed a locating adaptation") {
		t.Errorf("the report never names the portability count:\n%s", stdout)
	}
}

// TestUnlocatableSurfaceOnAMetClauseIsNotATrueNegative is the other half: a
// property that never ran is not evidence the implementation met the clause.
func TestUnlocatableSurfaceOnAMetClauseIsNotATrueNegative(t *testing.T) {
	emitted, _ := runTool(t, writeFixture(t, []fixtureImplementation{{
		Name:  "impl-01",
		Model: "Sonnet 5",
		Runs: []fixtureRun{{
			Seed:     1,
			Surfaces: map[string]bool{"appRoot": true, "composer": true, "submit": true, "stateWords": false},
		}},
		Review: &fixtureReview{Overall: overallNotDefective},
	}}, defaultMapping))

	if got := emitted.Clauses.UnevaluatedSurfaceMissed; got != 1 {
		t.Errorf("clause matrix reports %d unevaluated pair(s), want 1: R5 is covered only by a property "+
			"stateWords left unrunnable", got)
	}
	if got := emitted.Clauses.TrueNegative; got != clauseCount-1 {
		t.Errorf("clause matrix reports %d true negative(s), want %d: R5 is excluded rather than credited", got, clauseCount-1)
	}
	if got := emitted.Clauses.FalsePositive; got != 0 {
		t.Errorf("clause matrix reports %d false positive(s), want 0: a property that never ran cannot have fired", got)
	}
}

// TestFiredPropertyOutranksAnUnevaluableSibling keeps the exclusion narrow: a
// clause is unevaluated only when every property covering it was unrunnable.
func TestFiredPropertyOutranksAnUnevaluableSibling(t *testing.T) {
	emitted, _ := runTool(t, writeFixture(t, []fixtureImplementation{{
		Name:  "impl-01",
		Model: "Fable 5",
		Runs: []fixtureRun{{
			Seed:     1,
			Violated: []string{"serverHoldsEachMessageOnce"},
			Surfaces: map[string]bool{"appRoot": true, "composer": true, "submit": true, "stateWords": false},
		}},
		Review: &fixtureReview{Overall: overallDefective, Clauses: map[string]string{"R15": clauseViolates}},
	}}, defaultMapping))

	row := outcomeFor(t, emitted, "impl-01")
	if row.Cell != cellTruePositive {
		t.Fatalf("impl-01 landed in %q, want %q", row.Cell, cellTruePositive)
	}
	if got := emitted.Clauses.TruePositive; got != 1 {
		t.Errorf("R15 produced %d true positive(s), want 1: one covering property ran and fired", got)
	}
	if got := emitted.Clauses.UnevaluatedSurfaceMissed; got != 1 {
		t.Errorf("clause matrix reports %d unevaluated pair(s), want 1 for R5 alone", got)
	}
}

// TestFalseNegativeBehindAnUnlocatedSurfaceIsReportedApart keeps a portability
// miss out of the blind-spot story it would otherwise be read as.
func TestFalseNegativeBehindAnUnlocatedSurfaceIsReportedApart(t *testing.T) {
	emitted, stdout := runTool(t, writeFixture(t, []fixtureImplementation{{
		Name:  "impl-01",
		Model: "Opus 5",
		Runs: []fixtureRun{{
			Seed:     1,
			Surfaces: map[string]bool{"appRoot": true, "composer": true, "submit": true, "stateWords": false},
		}},
		Review: &fixtureReview{Overall: overallDefective, Clauses: map[string]string{"R5": clauseViolates}},
	}}, defaultMapping))

	row := outcomeFor(t, emitted, "impl-01")
	if row.Cell != cellFalseNegative {
		t.Fatalf("impl-01 landed in %q, want %q at the implementation unit", row.Cell, cellFalseNegative)
	}
	if !row.DefectOnlyOnUnevaluatedClauses {
		t.Error("impl-01 faults only R5, whose one property never ran, and is not marked as a portability miss")
	}
	if got := emitted.Clauses.FalseNegative; got != 0 {
		t.Errorf("clause matrix reports %d false negative(s), want 0", got)
	}
	if !strings.Contains(stdout, "portability misses, not blind spots") {
		t.Errorf("the report never separates the portability miss from the blind spot:\n%s", stdout)
	}
}

func TestPrecisionRecallAndPerModelBreakdown(t *testing.T) {
	emitted, stdout := runTool(t, writeFixture(t, []fixtureImplementation{
		{
			Name: "impl-01", Model: "Opus 5",
			Runs:   []fixtureRun{cleanRun(1, "serverHoldsEachMessageOnce")},
			Review: &fixtureReview{Overall: overallDefective, Clauses: map[string]string{"R15": clauseViolates}},
		},
		{
			Name: "impl-02", Model: "Opus 5",
			Runs:   []fixtureRun{cleanRun(1, "serverHoldsEachMessageOnce")},
			Review: &fixtureReview{Overall: overallNotDefective},
		},
		{
			Name: "impl-03", Model: "Sonnet 5",
			Runs:   []fixtureRun{cleanRun(1)},
			Review: &fixtureReview{Overall: overallDefective, Clauses: map[string]string{"R17": clauseViolates}},
		},
		{
			Name: "impl-04", Model: "Fable 5",
			Runs:   []fixtureRun{cleanRun(1)},
			Review: &fixtureReview{Overall: overallNotDefective},
		},
	}, defaultMapping))

	if emitted.Implementations.Scored != 4 {
		t.Fatalf("scored %d implementation(s), want 4", emitted.Implementations.Scored)
	}
	wantCells := map[string]int{"tp": 1, "fp": 1, "fn": 1, "tn": 1}
	got := map[string]int{
		"tp": emitted.Implementations.TruePositive,
		"fp": emitted.Implementations.FalsePositive,
		"fn": emitted.Implementations.FalseNegative,
		"tn": emitted.Implementations.TrueNegative,
	}
	for cell, want := range wantCells {
		if got[cell] != want {
			t.Errorf("implementation matrix %s = %d, want %d", cell, got[cell], want)
		}
	}
	if emitted.Implementations.Precision == nil || *emitted.Implementations.Precision != 0.5 {
		t.Errorf("precision = %v, want 0.5", emitted.Implementations.Precision)
	}
	if emitted.Implementations.Recall == nil || *emitted.Implementations.Recall != 0.5 {
		t.Errorf("recall = %v, want 0.5", emitted.Implementations.Recall)
	}

	if len(emitted.ByModel) != 3 {
		t.Fatalf("broke down %d model(s), want 3", len(emitted.ByModel))
	}
	if order := []string{emitted.ByModel[0].Model, emitted.ByModel[1].Model, emitted.ByModel[2].Model}; !equalStrings(order, capabilityOrder) {
		t.Errorf("models reported in %v, want the pre-registered capability order %v", order, capabilityOrder)
	}
	for _, model := range emitted.ByModel {
		switch model.Model {
		case "Opus 5":
			if model.Implementations.TruePositive != 1 || model.Implementations.FalsePositive != 1 {
				t.Errorf("Opus 5 = %+v, want one true positive and one false positive", model.Implementations)
			}
		case "Sonnet 5":
			if model.Implementations.FalseNegative != 1 {
				t.Errorf("Sonnet 5 = %+v, want one false negative", model.Implementations)
			}
		case "Fable 5":
			if model.Implementations.TrueNegative != 1 {
				t.Errorf("Fable 5 = %+v, want one true negative", model.Implementations)
			}
		}
	}
	if emitted.ReviewMinutes != 180 {
		t.Errorf("review cost = %d minutes, want 180: four forms recording 45 each", emitted.ReviewMinutes)
	}
	for _, want := range []string{"precision", "recall", "Sonnet 5", "Opus 5", "Fable 5",
		"review cost over the scored implementations: 180 minutes"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the report never prints %q:\n%s", want, stdout)
		}
	}
}

func TestCannotTellIsEvidenceNeitherWay(t *testing.T) {
	emitted, _ := runTool(t, writeFixture(t, []fixtureImplementation{{
		Name: "impl-01", Model: "Opus 5",
		Runs: []fixtureRun{cleanRun(1)},
		Review: &fixtureReview{Overall: overallNotDefective, Clauses: map[string]string{
			"R17": clauseCannotTell,
			"R18": clauseCannotTell,
		}},
	}}, defaultMapping))

	if got := emitted.Clauses.CannotTell; got != 2 {
		t.Fatalf("clause matrix reports %d cannot-tell pair(s), want 2", got)
	}
	if got := emitted.Clauses.Scored; got != clauseCount-2 {
		t.Errorf("clause matrix scored %d pair(s), want %d: a specification error is not a true negative", got, clauseCount-2)
	}
	row := outcomeFor(t, emitted, "impl-01")
	if want := []string{"R17", "R18"}; !equalStrings(row.CannotTellClauses, want) {
		t.Errorf("impl-01 reports cannot-tell clauses %v, want %v", row.CannotTellClauses, want)
	}
}

func TestUncoveredClauseMissIsSeparatedFromACoveredOne(t *testing.T) {
	emitted, stdout := runTool(t, writeFixture(t, []fixtureImplementation{{
		Name: "impl-01", Model: "Fable 5",
		Runs:   []fixtureRun{cleanRun(1)},
		Review: &fixtureReview{Overall: overallDefective, Clauses: map[string]string{"R17": clauseViolates}},
	}}, defaultMapping))

	if got := emitted.Clauses.FalseNegative; got != 1 {
		t.Fatalf("clause matrix reports %d false negative(s), want 1 for R17", got)
	}
	if got := emitted.Clauses.UncoveredFalseNegative; got != 1 {
		t.Errorf("clause matrix reports %d false negative(s) on a clause no property covers, want 1", got)
	}
	if want := []string{"R5", "R14", "R15"}; !equalStrings(emitted.Coverage.ClausesCovered, want) {
		t.Errorf("coverage reports %v covered, want %v", emitted.Coverage.ClausesCovered, want)
	}
	if !strings.Contains(stdout, "no property covers R1, R2") {
		t.Errorf("the report never names the clauses no property covers:\n%s", stdout)
	}
}

func TestAdjudicatedLabelReplacesTheFirstRaters(t *testing.T) {
	emitted, stdout := runTool(t, writeFixture(t, []fixtureImplementation{{
		Name: "impl-01", Model: "Opus 5",
		Runs:        []fixtureRun{cleanRun(1, "serverHoldsEachMessageOnce")},
		Review:      &fixtureReview{Overall: overallDefective, Clauses: map[string]string{"R15": clauseCannotTell}},
		Adjudicated: map[string]string{"R15": clauseViolates},
	}}, defaultMapping))

	if got := emitted.Clauses.TruePositive; got != 1 {
		t.Fatalf("clause matrix reports %d true positive(s), want 1: R15 resolved to %s", got, clauseViolates)
	}
	if got := emitted.Clauses.CannotTell; got != 0 {
		t.Errorf("clause matrix reports %d cannot-tell pair(s), want 0: the adjudicated label replaces it", got)
	}
	if !strings.Contains(stdout, "uses the adjudicated label for R15") {
		t.Errorf("the report never says an adjudicated label was used:\n%s", stdout)
	}
}

func TestFiredPropertyOutsideTheMappingIsReported(t *testing.T) {
	emitted, stdout := runTool(t, writeFixture(t, []fixtureImplementation{{
		Name: "impl-01", Model: "Sonnet 5",
		Runs:   []fixtureRun{cleanRun(1, "orderingHoldsAtTheServer")},
		Review: &fixtureReview{Overall: overallDefective, Clauses: map[string]string{"R17": clauseViolates}},
	}}, defaultMapping))

	if want := []string{"orderingHoldsAtTheServer"}; !equalStrings(emitted.Coverage.FiredPropertiesNotMapped, want) {
		t.Fatalf("coverage reports unmapped fired properties %v, want %v", emitted.Coverage.FiredPropertiesNotMapped, want)
	}
	if row := outcomeFor(t, emitted, "impl-01"); row.Cell != cellTruePositive {
		t.Errorf("impl-01 landed in %q, want %q: an unmapped property still fired", row.Cell, cellTruePositive)
	}
	if !strings.Contains(stdout, "could not be attributed to a clause") {
		t.Errorf("the report never flags the unmapped property:\n%s", stdout)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
