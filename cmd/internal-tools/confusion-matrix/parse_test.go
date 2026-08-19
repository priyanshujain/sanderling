package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runExpectingError(t *testing.T, built fixture) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"--sweep", built.Sweep,
		"--reviews", built.Reviews,
		"--assignment", built.Assignment,
		"--property-clauses", built.Mapping,
	}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("the tool reported success on input it must refuse:\n%s", stdout.String())
	}
	return err.Error()
}

func scoredFixture(name, model string) fixtureImplementation {
	return fixtureImplementation{
		Name: name, Model: model,
		Runs:   []fixtureRun{cleanRun(1)},
		Review: &fixtureReview{Overall: overallNotDefective},
	}
}

// Three flags missing is one rerun, not three: the operator is told about all
// of them at once, in flag order, whatever order the check happened to walk.
func TestRunNamesEveryMissingRequiredFlagInFlagOrder(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"--sweep", "s"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("got no error, want every missing flag named")
	}
	message := err.Error()
	previous := -1
	for _, name := range []string{"--reviews", "--assignment", "--property-clauses"} {
		at := strings.Index(message, name)
		if at < 0 {
			t.Fatalf("got %q, want %s named", message, name)
		}
		if at < previous {
			t.Errorf("got %q, want the flags named in flag order", message)
		}
		previous = at
	}
	if strings.Contains(message, "--sweep") {
		t.Errorf("got %q, want the supplied --sweep left out", message)
	}
}

func TestMappingRefusesInputTheMatrixCannotBeScoredFrom(t *testing.T) {
	tests := []struct {
		name    string
		mapping string
		want    string
	}{
		{
			name: "a property reads a surface the surface table never declares",
			mapping: "| property | clauses | surfaces |\n| p | R1 | badgeRow |\n" +
				"| surface | never observed | note |\n| composer | unlocatable | |\n",
			want: "the surface table does not declare",
		},
		{
			name:    "no surface is declared at all",
			mapping: "| property | clauses | surfaces |\n| p | R1 | none |\n",
			want:    "declares no surfaces",
		},
		{
			name: "a property names a clause outside the requirement",
			mapping: "| property | clauses | surfaces |\n| p | R21 | none |\n" +
				"| surface | never observed | note |\n| composer | unlocatable | |\n",
			want: "which is not one of R1 to R20",
		},
		{
			name: "a surface says something other than what a miss means",
			mapping: "| property | clauses | surfaces |\n| p | R1 | none |\n" +
				"| surface | never observed | note |\n| composer | maybe | |\n",
			want: "want unlocatable or inconclusive",
		},
		{
			name: "one property is mapped twice",
			mapping: "| property | clauses | surfaces |\n| p | R1 | none |\n| p | R2 | none |\n" +
				"| surface | never observed | note |\n| composer | unlocatable | |\n",
			want: "is mapped twice",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			built := writeFixture(t, []fixtureImplementation{scoredFixture("impl-01", "Opus 5")}, test.mapping)
			if got := runExpectingError(t, built); !strings.Contains(got, test.want) {
				t.Fatalf("error %q does not name the problem %q", got, test.want)
			}
		})
	}
}

func TestAssignmentRefusesAModelTheSampleWasNotDrawnFrom(t *testing.T) {
	built := writeFixture(t, []fixtureImplementation{scoredFixture("impl-01", "Opus 5")}, defaultMapping)
	mustWrite(t, built.Assignment, "| implementation | model |\n| impl-01 | Opus 4 |\n")
	if got := runExpectingError(t, built); !strings.Contains(got, "which is none of Sonnet 5, Opus 5, Fable 5") {
		t.Fatalf("error %q does not refuse the unknown model", got)
	}
}

func TestSweepRecordingAnImplementationTwiceIsRefused(t *testing.T) {
	built := writeFixture(t, []fixtureImplementation{scoredFixture("impl-01", "Opus 5")}, defaultMapping)
	path := filepath.Join(built.Sweep, sweepRecordsFileName)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, string(body)+string(body))
	if got := runExpectingError(t, built); !strings.Contains(got, "a second time") {
		t.Fatalf("error %q does not refuse the repeated implementation", got)
	}
}

func TestMalformedVerdictFormsAreExcludedWithTheirReason(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "a clause carries a word that is not a verdict",
			body: reviewWithRow("| R7 | probably fine | |"),
			want: "want meets, violates or cannot tell",
		},
		{
			name: "the form closes with no overall verdict",
			body: strings.Replace(renderReview(fixtureReview{Overall: overallNotDefective}),
				"overall: not defective", "", 1),
			want: "no overall verdict",
		},
		{
			name: "the overall verdict is neither answer",
			body: strings.Replace(renderReview(fixtureReview{Overall: overallNotDefective}),
				"overall: not defective", "overall: mostly ok", 1),
			want: "is neither defective nor not defective",
		},
		{
			name: "a clause is filed twice with two labels",
			body: renderReview(fixtureReview{Overall: overallNotDefective}) +
				"\n| R3 | violates | filed again |\n",
			want: "is filed twice",
		},
		{
			name: "a clause row is missing",
			body: strings.Replace(renderReview(fixtureReview{Overall: overallNotDefective}),
				"| R12 | meets | seen by hand | offline, compose, online |\n", "", 1),
			want: "no row for clause R12",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			built := writeFixture(t, []fixtureImplementation{{
				Name: "impl-01", Model: "Opus 5",
				Runs:      []fixtureRun{cleanRun(1)},
				RawReview: test.body,
			}}, defaultMapping)
			emitted, stdout := runTool(t, built)
			entry := exclusionFor(t, emitted, "impl-01")
			if entry.Reason != missingMalformed {
				t.Fatalf("impl-01 excluded as %q, want %q", entry.Reason, missingMalformed)
			}
			if !strings.Contains(entry.Detail, test.want) {
				t.Errorf("exclusion detail %q does not name %q", entry.Detail, test.want)
			}
			if !strings.Contains(stdout, missingMalformed) {
				t.Errorf("the report never prints the malformed form:\n%s", stdout)
			}
			if emitted.Implementations.Scored != 0 {
				t.Errorf("a malformed form scored %d implementation(s), want none", emitted.Implementations.Scored)
			}
		})
	}
}

func TestAnUnfilledMappingStillReportsTheImplementationMatrix(t *testing.T) {
	unfilled := "| property | clauses | surfaces |\n| TODO | TODO | TODO |\n" +
		"| surface | never observed | note |\n| stateWords | unlocatable | R4 obliges one |\n"
	emitted, stdout := runTool(t, writeFixture(t, []fixtureImplementation{{
		Name: "impl-01", Model: "Opus 5",
		Runs:   []fixtureRun{cleanRun(1, "serverHoldsEachMessageOnce")},
		Review: &fixtureReview{Overall: overallDefective, Clauses: map[string]string{"R15": clauseViolates}},
	}}, unfilled))

	if emitted.Implementations.TruePositive != 1 {
		t.Fatalf("implementation matrix = %+v, want one true positive", emitted.Implementations)
	}
	if emitted.Clauses.TruePositive != 0 || emitted.Clauses.FalseNegative != 1 {
		t.Errorf("clause matrix = %+v, want every clause uncovered", emitted.Clauses)
	}
	if emitted.Coverage.MappingTodoRows != 1 {
		t.Errorf("coverage reports %d TODO row(s), want 1", emitted.Coverage.MappingTodoRows)
	}
	if !strings.Contains(stdout, "maps no property to a clause") {
		t.Errorf("the report never says the mapping is unfilled:\n%s", stdout)
	}
}

func TestAnImplementationOutsideTheAssignmentIsMissingData(t *testing.T) {
	emitted, _ := runTool(t, writeFixture(t, []fixtureImplementation{
		scoredFixture("impl-01", "Opus 5"),
		{
			Name: "impl-02",
			Runs: []fixtureRun{cleanRun(1)}, Review: &fixtureReview{Overall: overallNotDefective},
		},
	}, defaultMapping))

	if entry := exclusionFor(t, emitted, "impl-02"); entry.Reason != missingNoModel {
		t.Fatalf("impl-02 excluded as %q, want %q", entry.Reason, missingNoModel)
	}
}

func TestSurfacesArePooledAcrossTheSeedsOneImplementationWasSweptAt(t *testing.T) {
	emitted, _ := runTool(t, writeFixture(t, []fixtureImplementation{{
		Name: "impl-01", Model: "Fable 5",
		Runs: []fixtureRun{
			{Seed: 1, Surfaces: map[string]bool{"composer": true}},
			{Seed: 2, Surfaces: map[string]bool{"stateWords": true}},
		},
		Review: &fixtureReview{Overall: overallNotDefective},
	}}, defaultMapping))

	row := outcomeFor(t, emitted, "impl-01")
	if len(row.UnlocatableSurfaces) != 0 {
		t.Fatalf("impl-01 reports %v unlocatable, want none: each surface was located on one seed or the other",
			row.UnlocatableSurfaces)
	}
	if emitted.Clauses.UnevaluatedSurfaceMissed != 0 {
		t.Errorf("clause matrix reports %d unevaluated pair(s), want none", emitted.Clauses.UnevaluatedSurfaceMissed)
	}
}

func reviewWithRow(row string) string {
	body := renderReview(fixtureReview{Overall: overallNotDefective})
	return strings.Replace(body, "| R7 | meets | seen by hand | offline, compose, online |", row, 1)
}
