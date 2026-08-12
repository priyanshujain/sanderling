package main

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildFixtureCampaign writes a campaign directory shaped exactly like the one
// the campaign tool emits: campaign.json plus one runs.jsonl line per seed.
func buildFixtureCampaign(t *testing.T, directory, armName string, budget int, records []map[string]any) {
	t.Helper()
	seeds := make([]int, 0, len(records))
	for _, record := range records {
		seeds = append(seeds, record["seed"].(int))
	}
	writeCampaign(t, directory, map[string]any{
		"arm":           armName,
		"generator":     "seeded",
		"platform":      "web",
		"spec_path":     "/specs/folio.ts",
		"bundle_id":     "app.folio",
		"max_steps":     budget,
		"seeds":         seeds,
		"host":          "experiment-host",
		"started_at":    "2026-08-12T00:00:00Z",
		"argument_temp": nil,
	}, records)
}

func seededArmRecords() []map[string]any {
	// Ten runs: two violate early, one violates late, six run the budget clean,
	// one times out and is missing data rather than a censored observation.
	return []map[string]any{
		{"seed": 1, "exit_code": 0, "steps": 60, "duration_millis": 300000, "first_violation_origin_step": nil},
		{"seed": 2, "exit_code": 0, "steps": 18, "duration_millis": 120000,
			"first_violation_origin_step": 14, "violated_properties": []string{"cartTotalMatches"}},
		{"seed": 3, "exit_code": 0, "steps": 60, "duration_millis": 300000, "first_violation_origin_step": nil},
		{"seed": 4, "exit_code": 0, "steps": 60, "duration_millis": 300000, "first_violation_origin_step": nil},
		{"seed": 5, "exit_code": 0, "steps": 44, "duration_millis": 240000,
			"first_violation_origin_step": 41, "violated_properties": []string{"cartTotalMatches", "backLeavesApp"}},
		{"seed": 6, "exit_code": 0, "steps": 60, "duration_millis": 300000, "first_violation_origin_step": nil},
		{"seed": 7, "exit_code": -1, "timed_out": true, "duration_millis": 900000},
		{"seed": 8, "exit_code": 0, "steps": 60, "duration_millis": 300000, "first_violation_origin_step": nil},
		{"seed": 9, "exit_code": 0, "steps": 60, "duration_millis": 300000, "first_violation_origin_step": nil},
		{"seed": 10, "exit_code": 0, "steps": 21, "duration_millis": 130000,
			"first_violation_origin_step": 19, "violated_properties": []string{"cartTotalMatches"}},
	}
}

func llmArmRecords() []map[string]any {
	// Eight runs: six violate, one clean, one failed to launch.
	return []map[string]any{
		{"seed": 1, "exit_code": 0, "steps": 7, "duration_millis": 400000,
			"first_violation_origin_step": 5, "violated_properties": []string{"cartTotalMatches"}},
		{"seed": 2, "exit_code": 0, "steps": 9, "duration_millis": 420000,
			"first_violation_origin_step": 8, "violated_properties": []string{"backLeavesApp"}},
		{"seed": 3, "exit_code": 0, "steps": 60, "duration_millis": 1800000, "first_violation_origin_step": nil},
		{"seed": 4, "exit_code": 0, "steps": 5, "duration_millis": 380000,
			"first_violation_origin_step": 3, "violated_properties": []string{"cartTotalMatches"}},
		{"seed": 5, "exit_code": 0, "steps": 13, "duration_millis": 500000,
			"first_violation_origin_step": 11, "violated_properties": []string{"cartTotalMatches", "priceNeverNegative"}},
		{"seed": 6, "exit_code": -1, "launch_error": "fork/exec sanderling: no such file or directory"},
		{"seed": 7, "exit_code": 0, "steps": 6, "duration_millis": 390000,
			"first_violation_origin_step": 6, "violated_properties": []string{"cartTotalMatches"}},
		{"seed": 8, "exit_code": 0, "steps": 16, "duration_millis": 520000,
			"first_violation_origin_step": 15, "violated_properties": []string{"backLeavesApp"}},
	}
}

func TestRun_EndToEndOverFixtureCampaignDirectories(t *testing.T) {
	root := t.TempDir()
	seededDirectory := filepath.Join(root, "seeded-web")
	llmDirectory := filepath.Join(root, "llm-web")
	buildFixtureCampaign(t, seededDirectory, "seeded", 60, seededArmRecords())
	buildFixtureCampaign(t, llmDirectory, "llm", 60, llmArmRecords())
	summaryPath := filepath.Join(root, "analysis.json")

	var stdout bytes.Buffer
	if err := run([]string{"--json", summaryPath, seededDirectory, llmDirectory}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}

	text := stdout.String()
	for _, fragment := range []string{
		"steps to first violation, right-censored at the step budget",
		"log-rank across 2 arms",
		"pairwise wilcoxon rank-sum",
		"llm vs seeded",
		"excluded 1 run(s) as missing data",
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("stdout is missing %q\n%s", fragment, text)
		}
	}

	body, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatal(err)
	}
	var result analysis
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}

	if len(result.Arms) != 2 {
		t.Fatalf("%d arms, want 2", len(result.Arms))
	}
	byName := map[string]armSummary{}
	for _, summary := range result.Arms {
		byName[summary.Arm] = summary
	}

	seeded := byName["seeded"]
	if seeded.Usable != 9 || seeded.Violated != 3 || seeded.Censored != 6 || seeded.Excluded != 1 {
		t.Errorf("seeded arm %+v, want 9 usable, 3 violated, 6 censored, 1 excluded", seeded)
	}
	if seeded.ExcludedByReason[reasonTimedOut] != 1 {
		t.Errorf("seeded exclusions %v, want one timeout", seeded.ExcludedByReason)
	}
	if seeded.MedianStepsToFirstViolation != nil {
		t.Errorf("seeded median %v, want undefined with 3 of 9 violating",
			*seeded.MedianStepsToFirstViolation)
	}
	if math.Abs(*seeded.ViolationRate-3.0/9.0) > 1e-12 {
		t.Errorf("seeded violation rate %v, want 1/3", *seeded.ViolationRate)
	}
	// cartTotalMatches in 3 runs, backLeavesApp in 1 of 2 distinct defects.
	if seeded.DistinctDefects != 2 || seeded.SingletonDefects != 1 {
		t.Errorf("seeded defects %d singletons %d, want 2 and 1", seeded.DistinctDefects, seeded.SingletonDefects)
	}
	if seeded.TotalActions != 443 {
		t.Errorf("seeded actions %d, want 443", seeded.TotalActions)
	}

	llm := byName["llm"]
	if llm.Usable != 7 || llm.Violated != 6 || llm.Censored != 1 || llm.Excluded != 1 {
		t.Errorf("llm arm %+v, want 7 usable, 6 violated, 1 censored, 1 excluded", llm)
	}
	if llm.ExcludedByReason[reasonLaunchError] != 1 {
		t.Errorf("llm exclusions %v, want one launch error", llm.ExcludedByReason)
	}
	if llm.MedianStepsToFirstViolation == nil || *llm.MedianStepsToFirstViolation != 8 {
		t.Errorf("llm median %v, want 8", llm.MedianStepsToFirstViolation)
	}

	if result.LogRank == nil {
		t.Fatal("no log-rank result")
	}
	if result.LogRank.DegreesOfFreedom != 1 {
		t.Errorf("log-rank df %d, want 1", result.LogRank.DegreesOfFreedom)
	}
	if result.LogRank.PValue > 0.05 {
		t.Errorf("log-rank p %v, want the two clearly different arms to separate", result.LogRank.PValue)
	}
	if len(result.Pairwise) != 1 {
		t.Fatalf("%d comparisons, want 1", len(result.Pairwise))
	}
	pair := result.Pairwise[0]
	if pair.First != "llm" || pair.Second != "seeded" {
		t.Errorf("comparison %s vs %s, want arms in sorted order", pair.First, pair.Second)
	}
	if pair.A12 >= 0.5 {
		t.Errorf("a12 %v, want the arm that violates sooner below 0.5", pair.A12)
	}
	if pair.HolmPValue != pair.PValue {
		t.Errorf("holm p %v differs from raw p %v in a family of one", pair.HolmPValue, pair.PValue)
	}
	if pair.Exact {
		t.Error("used the exact null distribution despite the tie mass at the budget")
	}
}

func TestRun_ReportsBothArmsWhenOneHasNothingUsable(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "good")
	broken := filepath.Join(root, "broken")
	buildFixtureCampaign(t, good, "good", 30, []map[string]any{
		{"seed": 1, "exit_code": 0, "steps": 30},
		{"seed": 2, "exit_code": 0, "steps": 9, "duration_millis": 1000,
			"first_violation_origin_step": 9, "violated_properties": []string{"cartTotalMatches"}},
	})
	buildFixtureCampaign(t, broken, "broken", 30, []map[string]any{
		{"seed": 1, "exit_code": 3},
		{"seed": 2, "timed_out": true, "exit_code": -1},
	})

	var stdout bytes.Buffer
	if err := run([]string{good, broken}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	text := stdout.String()
	if !strings.Contains(text, "broken") {
		t.Errorf("the arm with nothing usable is not reported\n%s", text)
	}
	if strings.Contains(text, "log-rank across") {
		t.Errorf("ran a log-rank with only one testable arm\n%s", text)
	}
	if !strings.Contains(text, "arms with no usable runs are reported but left out") {
		t.Errorf("no note about the dropped arm\n%s", text)
	}
}

func TestRun_JsonToStdout(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "only")
	buildFixtureCampaign(t, directory, "only", 20, []map[string]any{
		{"seed": 1, "exit_code": 0, "steps": 20},
	})
	var stdout bytes.Buffer
	if err := run([]string{"--json", "-", "--campaign", directory}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	start := strings.Index(stdout.String(), "{")
	if start < 0 {
		t.Fatalf("no json in stdout\n%s", stdout.String())
	}
	var result analysis
	if err := json.Unmarshal([]byte(stdout.String()[start:]), &result); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(result.Arms) != 1 || result.Arms[0].Arm != "only" {
		t.Errorf("arms %+v", result.Arms)
	}
}

func TestRun_RejectsTheSameDirectoryTwice(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "one")
	buildFixtureCampaign(t, directory, "one", 20, []map[string]any{{"seed": 1, "exit_code": 0, "steps": 20}})
	err := run([]string{directory, directory}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("error %v, want a refusal to double count", err)
	}
}

func TestRun_RequiresACampaignDirectory(t *testing.T) {
	if err := run(nil, io.Discard, io.Discard); err == nil {
		t.Fatal("expected an error with no campaign directories")
	}
}
