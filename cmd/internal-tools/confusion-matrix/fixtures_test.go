package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fixtureRun is one campaign of one implementation at one seed, in the shape
// implementation-sweep and campaign write together.
type fixtureRun struct {
	Seed     int64
	ExitCode int
	// CampaignExitCode is the campaign process's own exit status, which the
	// sweep records beside the campaign directory. It is not the exit status of
	// the run inside that campaign.
	CampaignExitCode int
	TimedOut         bool
	// PreconditionFailures is how many of the run's 400 steps never had the app
	// under test in front of them.
	PreconditionFailures int
	Violated             []string
	// Surfaces is the locatableSurfaces reading the trace records. A nil map
	// with NoTrace false still writes a reading of every surface false.
	Surfaces map[string]bool
	NoTrace  bool
}

type fixtureReview struct {
	Overall string
	Clauses map[string]string
	Minutes int
}

type fixtureImplementation struct {
	Name        string
	Model       string
	FailedStage string
	Runs        []fixtureRun
	Review      *fixtureReview
	RawReview   string
	Adjudicated map[string]string
}

type fixture struct {
	Sweep      string
	Reviews    string
	Assignment string
	Mapping    string
}

const defaultMapping = `
| property | clauses | surfaces |
| --- | --- | --- |
| sentOnlyAfterConfirmation | R5 R15 | stateWords |
| serverHoldsEachMessageOnce | R15 | none |
| unsentReachesZero | R14 | unsentCount |

| surface | never observed | note |
| --- | --- | --- |
| composer | unlocatable | R1 obliges one |
| stateWords | unlocatable | R4 obliges one on every composed message |
| unsentCount | inconclusive | R6 hides the count at zero |
`

func writeFixture(t *testing.T, implementations []fixtureImplementation, mappingBody string) fixture {
	t.Helper()
	root := t.TempDir()
	built := fixture{
		Sweep:      filepath.Join(root, "sweep"),
		Reviews:    filepath.Join(root, "reviews"),
		Assignment: filepath.Join(root, "assignment.md"),
		Mapping:    filepath.Join(root, "property-clauses.md"),
	}
	mustMkdir(t, built.Sweep)
	mustMkdir(t, built.Reviews)
	mustWrite(t, built.Mapping, mappingBody)

	var planned []map[string]any
	var assignmentRows []string
	assignmentRows = append(assignmentRows, "| implementation | model |", "| --- | --- |")
	var records []string
	for _, implementation := range implementations {
		planned = append(planned, map[string]any{"name": implementation.Name})
		if implementation.Model != "" {
			assignmentRows = append(assignmentRows,
				fmt.Sprintf("| %s | %s |", implementation.Name, implementation.Model))
		}
		records = append(records, writeImplementation(t, built, implementation))
		writeReviewFiles(t, built.Reviews, implementation)
	}
	mustWrite(t, built.Assignment, strings.Join(assignmentRows, "\n")+"\n")
	mustWriteJSON(t, filepath.Join(built.Sweep, sweepManifestFileName), map[string]any{
		"generator":       "seeded",
		"platform":        "web",
		"spec_path":       "paper/experiments/e4/spec/spec.ts",
		"max_steps":       400,
		"seeds":           []int{1, 2},
		"host":            "anton",
		"implementations": planned,
	})
	mustWrite(t, filepath.Join(built.Sweep, sweepRecordsFileName), strings.Join(records, "\n")+"\n")
	return built
}

func writeImplementation(t *testing.T, built fixture, implementation fixtureImplementation) string {
	t.Helper()
	record := map[string]any{"implementation": implementation.Name}
	if implementation.FailedStage != "" {
		record["failed_stage"] = implementation.FailedStage
		record["error"] = "bun run build exited 1"
		return encode(t, record)
	}
	var runs []map[string]any
	for _, run := range implementation.Runs {
		seedText := strconv.FormatInt(run.Seed, 10)
		campaignDirectory := filepath.Join(built.Sweep, implementation.Name, "seed-"+seedText)
		mustMkdir(t, campaignDirectory)
		mustWriteJSON(t, filepath.Join(campaignDirectory, campaignManifestFileName), map[string]any{
			"arm":       implementation.Name,
			"generator": "seeded",
			"platform":  "web",
			"max_steps": 400,
			"seeds":     []int64{run.Seed},
		})
		runDirectory := filepath.Join("seed-"+seedText, "20260820T110000Z")
		campaignRun := map[string]any{
			"seed":          run.Seed,
			"exit_code":     run.ExitCode,
			"steps":         400,
			"actions":       380,
			"run_directory": runDirectory,
		}
		if run.TimedOut {
			campaignRun["timed_out"] = true
		}
		if run.PreconditionFailures > 0 {
			campaignRun["precondition_failures"] = run.PreconditionFailures
		}
		if len(run.Violated) > 0 {
			campaignRun["violated_properties"] = run.Violated
		}
		mustWrite(t, filepath.Join(campaignDirectory, campaignRecordsFileName), encode(t, campaignRun)+"\n")
		if !run.NoTrace {
			writeTrace(t, filepath.Join(campaignDirectory, runDirectory), run.Surfaces)
		}
		runs = append(runs, map[string]any{
			"seed":               run.Seed,
			"exit_code":          run.CampaignExitCode,
			"campaign_directory": campaignDirectory,
		})
	}
	record["runs"] = runs
	return encode(t, record)
}

// writeTrace writes the one line the verifier emits on the first snapshot, when
// every extractor is reported as a change from null.
func writeTrace(t *testing.T, directory string, surfaces map[string]bool) {
	t.Helper()
	mustMkdir(t, directory)
	reading := map[string]bool{}
	for _, surface := range []string{"appRoot", "composer", "submit", "stateWords",
		"unsentCount", "pendingIndicator", "offlineIndicator", "retryControl"} {
		reading[surface] = surfaces[surface]
	}
	line := map[string]any{
		"step": 1,
		"extractor_changes": map[string]any{
			surfacesExtractor: map[string]any{"prev": nil, "curr": reading},
		},
	}
	mustWrite(t, filepath.Join(directory, traceFileName), encode(t, line)+"\n")
}

func writeReviewFiles(t *testing.T, directory string, implementation fixtureImplementation) {
	t.Helper()
	if implementation.RawReview != "" {
		mustWrite(t, filepath.Join(directory, implementation.Name+".md"), implementation.RawReview)
		return
	}
	if implementation.Review == nil {
		return
	}
	mustWrite(t, filepath.Join(directory, implementation.Name+".md"), renderReview(*implementation.Review))
	if len(implementation.Adjudicated) == 0 {
		return
	}
	rows := []string{"| clause | verdict | why |", "| --- | --- | --- |"}
	for _, clause := range allClauses() {
		label, resolved := implementation.Adjudicated[clause]
		if !resolved {
			continue
		}
		rows = append(rows, fmt.Sprintf("| %s | %s | joint reread |", clause, label))
	}
	mustWrite(t, filepath.Join(directory, implementation.Name+"-adjudication.md"),
		strings.Join(rows, "\n")+"\n")
}

func renderReview(review fixtureReview) string {
	minutes := review.Minutes
	if minutes == 0 {
		minutes = 45
	}
	lines := []string{
		"# review",
		"",
		"reviewer: Jane",
		"date: 2026-09-02",
		fmt.Sprintf("minutes: %d", minutes),
		"",
		"| clause | verdict | justification | steps |",
		"| --- | --- | --- | --- |",
	}
	for _, clause := range allClauses() {
		label := review.Clauses[clause]
		if label == "" {
			label = clauseMeets
		}
		lines = append(lines, fmt.Sprintf("| %s | %s | seen by hand | offline, compose, online |", clause, label))
	}
	lines = append(lines, "", "overall: "+review.Overall, "")
	return strings.Join(lines, "\n")
}

func runTool(t *testing.T, built fixture) (result, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	jsonPath := filepath.Join(t.TempDir(), "matrix.json")
	err := run([]string{
		"--sweep", built.Sweep,
		"--reviews", built.Reviews,
		"--assignment", built.Assignment,
		"--property-clauses", built.Mapping,
		"--json", jsonPath,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	body, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read emitted summary: %v", err)
	}
	var emitted result
	if err := json.Unmarshal(body, &emitted); err != nil {
		t.Fatalf("emitted summary is not valid JSON: %v", err)
	}
	return emitted, stdout.String()
}

func cleanRun(seed int64, violated ...string) fixtureRun {
	return fixtureRun{
		Seed:     seed,
		Violated: violated,
		Surfaces: map[string]bool{"appRoot": true, "composer": true, "submit": true, "stateWords": true},
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustWriteJSON(t *testing.T, path string, value any) {
	t.Helper()
	mustWrite(t, path, encode(t, value)+"\n")
}

func encode(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func outcomeFor(t *testing.T, emitted result, name string) implementationOutcome {
	t.Helper()
	for _, row := range emitted.Outcomes {
		if row.Implementation == name {
			return row
		}
	}
	t.Fatalf("%s carries no cell; excluded as %v", name, emitted.Excluded)
	return implementationOutcome{}
}

func exclusionFor(t *testing.T, emitted result, name string) exclusion {
	t.Helper()
	for _, entry := range emitted.Excluded {
		if entry.Implementation == name {
			return entry
		}
	}
	t.Fatalf("%s was not excluded; it scored %v", name, emitted.Outcomes)
	return exclusion{}
}
