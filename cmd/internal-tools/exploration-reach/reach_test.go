package main

import (
	"testing"

	"github.com/priyanshujain/sanderling/internal/driver/ioscompanion"
	"github.com/priyanshujain/sanderling/internal/hierarchy"
	"github.com/priyanshujain/sanderling/internal/trace"
	"github.com/priyanshujain/sanderling/internal/tracecorpus"
)

const (
	home = `{"attributes": {"text": "Home", "bounds": "[0,0,10,10]"}, "children": [
    {"attributes": {"text": "row", "bounds": "[0,0,5,5]"}, "children": []}
  ]}`
	homeScrolled = `{"attributes": {"text": "Home", "bounds": "[0,4,10,14]"}, "children": [
    {"attributes": {"text": "row", "bounds": "[0,4,5,9]"}, "children": []}
  ]}`
	ledger = `{"attributes": {"text": "Ledger", "bounds": "[0,0,10,10]"}, "children": [
    {"attributes": {"text": "row", "bounds": "[0,0,5,5]"}, "children": []}
  ]}`
	ledgerWithRow = `{"attributes": {"text": "Ledger", "bounds": "[0,0,10,10]"}, "children": [
    {"attributes": {"text": "row", "bounds": "[0,0,5,5]"}, "children": []},
    {"attributes": {"text": "row"}, "children": []}
  ]}`
)

// TestReachCountsStructuresNotObservations: five observations of three
// structures, one of them revisited and one differing only in where it sits on
// screen, so the answer is three by construction.
func TestReachCountsStructuresNotObservations(t *testing.T) {
	reach := measureRun(t, 7, home, homeScrolled, ledger, home, ledgerWithRow)

	if reach.Observations != 5 {
		t.Fatalf("observations = %d, want 5", reach.Observations)
	}
	if reach.Distinct != 3 {
		t.Fatalf("distinct states = %d, want 3", reach.Distinct)
	}
}

func TestFinalizeRecordIsNoObservation(t *testing.T) {
	directory := writeRun(t, 7, home, ledger)
	appendFinalize(t, directory, 3)

	run, err := tracecorpus.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	reach := measure(run)
	if reach.Observations != 2 || reach.Unobserved != 1 {
		t.Fatalf("observations = %d, unobserved = %d, want 2 and 1",
			reach.Observations, reach.Unobserved)
	}
}

// TestStoredTreeHashesAsTheLiveTreeDid is the equivalence the whole measure
// rests on: the hash the settle path computed from the live tree and the hash
// this tool computes from the stored one are the same string, so a reach
// number counts the same state boundaries the drivers wait on.
func TestStoredTreeHashesAsTheLiveTreeDid(t *testing.T) {
	live, err := hierarchy.Parse(home)
	if err != nil {
		t.Fatal(err)
	}
	liveHash := ioscompanion.StructuralHash(live)
	if liveHash == "" {
		t.Fatal("live hash is empty, so the test would pass on any stored tree")
	}

	run, err := tracecorpus.Load(writeRun(t, 7, home))
	if err != nil {
		t.Fatal(err)
	}
	stored := ioscompanion.StructuralHash(run.Steps[0].Hierarchy)
	if stored != liveHash {
		t.Fatalf("stored hash differs from the live one:\n live=%q\n stored=%q",
			liveHash, stored)
	}
}

func TestFirstDivergenceNamesTheObservationThatDiffers(t *testing.T) {
	reference := measureRun(t, 7, home, ledger, home, ledger)
	replay := measureRun(t, 7, home, ledger, ledgerWithRow, ledger)

	divergence := diverge(reference, replay)
	if !divergence.Diverged || divergence.Step != 3 {
		t.Fatalf("divergence = %+v, want the third observation", divergence)
	}
}

func TestAReplayThatMatchesIsCensoredAtTheSharedLength(t *testing.T) {
	reference := measureRun(t, 7, home, ledger, home, ledger)
	replay := measureRun(t, 7, home, ledger, home)

	divergence := diverge(reference, replay)
	if divergence.Diverged {
		t.Fatalf("identical observations must not report a divergence: %+v", divergence)
	}
	if divergence.Step != 3 || divergence.Compared != 3 {
		t.Fatalf("divergence = %+v, want censoring at the third observation", divergence)
	}
}

func TestMedianDivergenceHoldsCensoredReplaysApart(t *testing.T) {
	median, censored := medianDivergence([]Divergence{
		{Step: 9, Diverged: true},
		{Step: 3, Diverged: true},
		{Step: 40, Diverged: false},
	})
	if median != 9 {
		t.Fatalf("median = %v, want 9", median)
	}
	if censored != 1 {
		t.Fatalf("censored = %d, want 1", censored)
	}
}

func TestCorpusStatesAreTheUnionNotTheSum(t *testing.T) {
	first := measureRun(t, 7, home, ledger)
	second := measureRun(t, 11, ledger, ledgerWithRow)

	if got := corpusDistinct([]Reach{first, second}); got != 3 {
		t.Fatalf("corpus distinct = %d, want 3", got)
	}
}

func measureRun(t *testing.T, seed int64, dumps ...string) Reach {
	t.Helper()
	run, err := tracecorpus.Load(writeRun(t, seed, dumps...))
	if err != nil {
		t.Fatal(err)
	}
	return measure(run)
}

func writeRun(t *testing.T, seed int64, dumps ...string) string {
	t.Helper()
	directory := t.TempDir()
	writer, err := trace.NewWriter(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteMeta(trace.Meta{Seed: seed, Platform: "web"}); err != nil {
		t.Fatal(err)
	}
	for index, dump := range dumps {
		tree, err := hierarchy.Parse(dump)
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteStep(trace.Step{Index: index + 1, Hierarchy: tree}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return directory
}

func appendFinalize(t *testing.T, directory string, index int) {
	t.Helper()
	writer, err := trace.NewWriter(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteStep(trace.Step{
		Index:      index,
		Violations: []string{"someTransactionExists"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}
