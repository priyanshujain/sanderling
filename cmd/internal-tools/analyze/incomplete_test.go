package main

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// A campaign that produced nothing still has a manifest, and the manifest is
// what makes the difference between an arm that ran nothing and an arm that was
// never scheduled. Reading one must report every intended seed as missing rather
// than an arm with a small clean sample.
func TestRun_EmptyCampaignIsReportedAsEverySeedMissing(t *testing.T) {
	root := t.TempDir()
	empty := filepath.Join(root, "empty")
	writeCampaign(t, empty, map[string]any{
		"arm": "seeded", "max_steps": 400, "seeds": []int{1, 2, 3, 4, 5},
	}, nil)

	var stdout bytes.Buffer
	if err := run([]string{empty}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	result := analyseCampaigns(t, empty)
	summary := armByName(t, result, "seeded")
	if summary.Recorded != 0 || summary.Usable != 0 {
		t.Errorf("recorded %d usable %d, want none", summary.Recorded, summary.Usable)
	}
	if len(summary.MissingSeeds) != 5 {
		t.Errorf("missing seeds %v, want all five", summary.MissingSeeds)
	}
	if summary.MedianStepsToFirstViolation != nil || summary.ViolationRate != nil {
		t.Errorf("median %v violation rate %v, want neither from no runs",
			summary.MedianStepsToFirstViolation, summary.ViolationRate)
	}
	if result.LogRank != nil || len(result.Pairwise) != 0 {
		t.Errorf("log-rank %+v pairwise %v, want no tests", result.LogRank, result.Pairwise)
	}
	if !strings.Contains(stdout.String(), "seeded") {
		t.Errorf("the empty arm is not in the report\n%s", stdout.String())
	}
}

// A host that stopped part way through leaves a directory that looks complete.
// The seeds it never reached are the difference between a partial campaign and
// a smaller one, and the report has to carry that count.
func TestRun_PartialCampaignCountsTheSeedsTheHostNeverReached(t *testing.T) {
	root := t.TempDir()
	partial := filepath.Join(root, "partial")
	var records []map[string]any
	for seed := 1; seed <= 6; seed++ {
		records = append(records, map[string]any{
			"seed": seed, "exit_code": 0, "steps": 400, "actions": 380, "monotonic_millis": 360000,
		})
	}
	writeCampaign(t, partial, map[string]any{
		"arm": "seeded", "max_steps": 400,
		"seeds": []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	}, records)

	summary := armByName(t, analyseCampaigns(t, partial), "seeded")
	if summary.Usable != 6 {
		t.Errorf("%d usable runs, want the 6 that landed", summary.Usable)
	}
	if len(summary.MissingSeeds) != 4 {
		t.Errorf("missing seeds %v, want the 4 the host never reached", summary.MissingSeeds)
	}

	var stdout bytes.Buffer
	if err := run([]string{partial}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "missing") {
		t.Errorf("no missing-seed column in the report\n%s", stdout.String())
	}
}

// The ablation is seed-matched, so a run lost on one arm removes its partner
// from the comparison too. Those seeds are named because a paired sample that
// silently shrinks is how a campaign reports a difference between two arms that
// were not in fact matched.
func TestRun_PairedComparisonNamesSeedsLostOnOneArm(t *testing.T) {
	root := t.TempDir()
	declared := []int{1, 2, 3, 4, 5, 6}
	pre := filepath.Join(root, "pre")
	post := filepath.Join(root, "post")
	writeCampaign(t, pre, map[string]any{"arm": "pre", "max_steps": 400, "seeds": declared}, []map[string]any{
		{"seed": 1, "exit_code": 0, "steps": 300, "actions": 290, "monotonic_millis": 1000, "first_violation_origin_step": 300, "violated_properties": []string{"p"}},
		{"seed": 2, "exit_code": 0, "steps": 400, "actions": 390, "monotonic_millis": 1000},
		{"seed": 3, "exit_code": 0, "steps": 250, "actions": 240, "monotonic_millis": 1000, "first_violation_origin_step": 250, "violated_properties": []string{"p"}},
		{"seed": 4, "exit_code": -1, "timed_out": true, "actions": 0},
	})
	writeCampaign(t, post, map[string]any{"arm": "post", "max_steps": 400, "seeds": declared}, []map[string]any{
		{"seed": 1, "exit_code": 0, "steps": 40, "actions": 38, "monotonic_millis": 1000, "first_violation_origin_step": 40, "violated_properties": []string{"p"}},
		{"seed": 2, "exit_code": 0, "steps": 60, "actions": 55, "monotonic_millis": 1000, "first_violation_origin_step": 60, "violated_properties": []string{"p"}},
		{"seed": 3, "exit_code": 0, "steps": 50, "actions": 47, "monotonic_millis": 1000, "first_violation_origin_step": 50, "violated_properties": []string{"p"}},
		{"seed": 4, "exit_code": 0, "steps": 70, "actions": 66, "monotonic_millis": 1000, "first_violation_origin_step": 70, "violated_properties": []string{"p"}},
	})

	result := analyseCampaigns(t, "--paired", pre, post)
	if result.Paired == nil {
		t.Fatal("no paired comparison")
	}
	if result.Paired.Pairs != 3 {
		t.Errorf("%d pairs, want the 3 seeds usable on both arms", result.Paired.Pairs)
	}
	if len(result.Paired.UnpairedSeeds) != 1 || result.Paired.UnpairedSeeds[0] != 4 {
		t.Errorf("unpaired seeds %v, want [4]", result.Paired.UnpairedSeeds)
	}
	for _, summary := range result.Arms {
		if len(summary.MissingSeeds) != 2 {
			t.Errorf("arm %s missing seeds %v, want seeds 5 and 6", summary.Arm, summary.MissingSeeds)
		}
	}

	var stdout bytes.Buffer
	if err := run([]string{"--paired", pre, post}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "usable in one arm only") {
		t.Errorf("the report does not name the lost seed\n%s", stdout.String())
	}
}

func TestRun_PairedRefusesAnythingOtherThanTwoArms(t *testing.T) {
	root := t.TempDir()
	var directories []string
	for _, name := range []string{"a", "b", "c"} {
		directory := filepath.Join(root, name)
		writeCampaign(t, directory, map[string]any{"arm": name, "max_steps": 40, "seeds": []int{1}},
			[]map[string]any{{"seed": 1, "exit_code": 0, "steps": 40, "actions": 40}})
		directories = append(directories, directory)
	}
	err := run(append([]string{"--paired"}, directories...), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "exactly two arms") {
		t.Fatalf("error %v, want a refusal to pair three arms", err)
	}
}

func TestRun_PairedRefusesArmsThatShareNoSeed(t *testing.T) {
	root := t.TempDir()
	north := filepath.Join(root, "north")
	south := filepath.Join(root, "south")
	writeCampaign(t, north, map[string]any{"arm": "north", "max_steps": 40, "seeds": []int{1, 2}},
		[]map[string]any{{"seed": 1, "exit_code": 0, "steps": 40, "actions": 40}})
	writeCampaign(t, south, map[string]any{"arm": "south", "max_steps": 40, "seeds": []int{3, 4}},
		[]map[string]any{{"seed": 3, "exit_code": 0, "steps": 40, "actions": 40}})

	err := run([]string{"--paired", north, south}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "share no seed") {
		t.Fatalf("error %v, want a refusal to pair arms that ran different seeds", err)
	}
}
