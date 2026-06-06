package trace

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

func TestWriteMeta_RoundTrip(t *testing.T) {
	directory := t.TempDir()
	writer, err := NewWriter(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	meta := Meta{
		Seed:         42,
		SpecPath:     "spec.ts",
		BundleSHA256: "deadbeef",
		Platform:     "android",
		BundleID:     "in.okcredit.merchant",
		StartedAt:    time.Date(2026, 4, 17, 22, 30, 0, 0, time.UTC),
		SanderlingVersion:  "0.0.1",
	}
	if err := writer.WriteMeta(meta); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(directory, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got Meta
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("meta.json is not valid JSON: %v\n%s", err, body)
	}
	if got != meta {
		t.Errorf("meta round-trip mismatch:\n got: %+v\nwant: %+v", got, meta)
	}
}

func TestWriteMeta_EndedAtRoundTrip(t *testing.T) {
	directory := t.TempDir()
	writer, err := NewWriter(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	endedAt := time.Date(2026, 4, 17, 22, 31, 0, 0, time.UTC)
	meta := Meta{
		Seed:        7,
		SpecPath:    "spec.ts",
		Platform:    "android",
		BundleID:    "in.test",
		StartedAt:   time.Date(2026, 4, 17, 22, 30, 0, 0, time.UTC),
		EndedAt:     &endedAt,
		SanderlingVersion: "0.0.1",
	}
	if err := writer.WriteMeta(meta); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(directory, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"ended_at": "2026-04-17T22:31:00Z"`) {
		t.Errorf("ended_at not in meta.json: %s", body)
	}
	var got Meta
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.EndedAt == nil || !got.EndedAt.Equal(endedAt) {
		t.Errorf("EndedAt round-trip wrong: %v", got.EndedAt)
	}
}

func TestWriteMeta_OmitsEndedAtWhenNil(t *testing.T) {
	directory := t.TempDir()
	writer, _ := NewWriter(directory)
	defer writer.Close()
	if err := writer.WriteMeta(Meta{StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(directory, "meta.json"))
	if strings.Contains(string(body), "ended_at") {
		t.Errorf("ended_at should be omitted when nil: %s", body)
	}
}

func TestWriteStep_HierarchyAndResidualsRoundTrip(t *testing.T) {
	directory := t.TempDir()
	writer, _ := NewWriter(directory)
	defer writer.Close()

	tree, err := hierarchy.Parse(`{"attributes":{"resource-id":"root"},"children":[
		{"attributes":{"resource-id":"child","text":"hi"},"children":[]}]}`)
	if err != nil {
		t.Fatal(err)
	}

	step := Step{
		Index:     1,
		Timestamp: time.Now().UTC(),
		NextAction: &Action{
			Kind:           "tap",
			Selector:       "id:next",
			ResolvedBounds: &BoundsRecord{X: 10, Y: 20, Width: 100, Height: 50},
			TapPoint:       &PointRecord{X: 60, Y: 45},
		},
		Hierarchy: tree,
		Residuals: map[string]json.RawMessage{
			"prop1": json.RawMessage(`{"op":"true"}`),
		},
	}
	if err := writer.WriteStep(step); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(directory, "trace.jsonl"))
	var got Step
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("bad jsonl: %v\n%s", err, body)
	}
	if got.NextAction.Selector != "id:next" {
		t.Errorf("selector = %q", got.NextAction.Selector)
	}
	if got.NextAction.ResolvedBounds == nil || got.NextAction.ResolvedBounds.Width != 100 {
		t.Errorf("resolved_bounds round-trip wrong: %+v", got.NextAction.ResolvedBounds)
	}
	if got.NextAction.TapPoint == nil || got.NextAction.TapPoint.X != 60 {
		t.Errorf("tap_point round-trip wrong: %+v", got.NextAction.TapPoint)
	}
	if string(got.Residuals["prop1"]) != `{"op":"true"}` {
		t.Errorf("residuals round-trip wrong: %s", got.Residuals["prop1"])
	}

	// Intentionally-lossy contract: Tree marshals only Elements (Root and
	// Node.Children are json:"-"). The flat element list survives; tree
	// structure does not. Lock both halves so a regression that drops the
	// element list, or one that silently starts persisting structure the
	// replay UI would then depend on, is caught.
	if got.Hierarchy == nil {
		t.Fatal("hierarchy dropped from trace")
	}
	if len(got.Hierarchy.Elements) != 2 {
		t.Fatalf("hierarchy elements not preserved: got %d", len(got.Hierarchy.Elements))
	}
	if got.Hierarchy.Elements[1].Text != "hi" {
		t.Errorf("element field lost: %+v", got.Hierarchy.Elements[1])
	}
	if got.Hierarchy.Root != nil {
		t.Errorf("Root is json:\"-\" and must decode nil, got %+v", got.Hierarchy.Root)
	}
}

func TestWriteStep_OmitsEmptyHierarchyAndResiduals(t *testing.T) {
	directory := t.TempDir()
	writer, _ := NewWriter(directory)
	defer writer.Close()
	if err := writer.WriteStep(Step{Index: 1}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(directory, "trace.jsonl"))
	if strings.Contains(string(body), "hierarchy") || strings.Contains(string(body), "residuals") {
		t.Errorf("empty hierarchy/residuals must omit: %s", body)
	}
}

func TestWriteStep_AppendsOneJsonLine(t *testing.T) {
	directory := t.TempDir()
	writer, err := NewWriter(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	step := Step{
		Index:     1,
		Timestamp: time.Now().UTC(),
		Screen:    "customer_ledger",
		Snapshots: map[string]json.RawMessage{
			"ledger.balance": json.RawMessage(`1500`),
		},
		NextAction: &Action{Kind: "tap", X: 100, Y: 200},
		Violations: []string{"ledgerBalanceMatchesTxns"},
	}
	if err := writer.WriteStep(step); err != nil {
		t.Fatal(err)
	}

	lines := readLines(t, filepath.Join(directory, "trace.jsonl"))
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	var got Step
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("invalid JSONL line: %v\n%s", err, lines[0])
	}
	if got.Index != 1 || got.Screen != "customer_ledger" || got.NextAction.X != 100 || got.Violations[0] != "ledgerBalanceMatchesTxns" {
		t.Errorf("step round-trip wrong: %+v", got)
	}
}

// Bug class: a property that first violates at step 0 carries Witness.Step==0,
// which omitempty drops from JSON. Decode must still yield Step 0 (the true
// origin) rather than confusing it with a later step. Also pins that the
// Witnesses/ExtractorChanges/Metrics/Exceptions sidecars survive the round-trip
// rather than silently vanishing on decode.
func TestWriteStep_DiagnosticsRoundTrip(t *testing.T) {
	directory := t.TempDir()
	writer, _ := NewWriter(directory)
	defer writer.Close()

	step := Step{
		Index: 4,
		Witnesses: map[string]Witness{
			"balanceNonNegative": {
				Reason:  "balance went negative",
				IsError: true,
				Step:    0,
				Extractors: map[string]json.RawMessage{
					"balance": json.RawMessage(`-5`),
				},
			},
		},
		ExtractorChanges: map[string]ExtractorChange{
			"balance": {Prev: json.RawMessage(`10`), Curr: json.RawMessage(`-5`)},
		},
		Metrics:    &Metrics{CPUPercent: 12.5, HeapBytes: 4096},
		Exceptions: []Exception{{Class: "NullPointerException", Message: "boom"}},
	}
	if err := writer.WriteStep(step); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(directory, "trace.jsonl"))
	if strings.Contains(string(body), `"step":0`) {
		t.Errorf("Witness.Step==0 should be omitted from JSON, got: %s", body)
	}
	var got Step
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("bad jsonl: %v\n%s", err, body)
	}
	w, ok := got.Witnesses["balanceNonNegative"]
	if !ok {
		t.Fatal("step-0 witness vanished on decode")
	}
	if w.Step != 0 || w.Reason != "balance went negative" || !w.IsError {
		t.Errorf("witness round-trip wrong: %+v", w)
	}
	if string(w.Extractors["balance"]) != `-5` {
		t.Errorf("witness extractors lost: %s", w.Extractors["balance"])
	}
	if c := got.ExtractorChanges["balance"]; string(c.Prev) != `10` || string(c.Curr) != `-5` {
		t.Errorf("extractor change round-trip wrong: %+v", c)
	}
	if got.Metrics == nil || got.Metrics.CPUPercent != 12.5 || got.Metrics.HeapBytes != 4096 {
		t.Errorf("metrics round-trip wrong: %+v", got.Metrics)
	}
	if len(got.Exceptions) != 1 || got.Exceptions[0].Class != "NullPointerException" {
		t.Errorf("exceptions round-trip wrong: %+v", got.Exceptions)
	}
}

func TestWriteStep_MultipleStepsAppend(t *testing.T) {
	directory := t.TempDir()
	writer, err := NewWriter(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	for index := 1; index <= 3; index++ {
		if err := writer.WriteStep(Step{Index: index, Screen: "s"}); err != nil {
			t.Fatal(err)
		}
	}
	lines := readLines(t, filepath.Join(directory, "trace.jsonl"))
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
}

// Bug class: dropping the writer mutex would let concurrent encoder.Encode
// calls interleave, producing torn JSONL lines. Run under -race: every line
// must be a complete, parseable Step and all N must arrive.
func TestWriteStep_ConcurrentWritesAreWellFormed(t *testing.T) {
	directory := t.TempDir()
	writer, err := NewWriter(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	const n = 50
	var wg sync.WaitGroup
	for index := 0; index < n; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			if err := writer.WriteStep(Step{Index: index, Screen: "s"}); err != nil {
				t.Errorf("WriteStep: %v", err)
			}
		}(index)
	}
	wg.Wait()

	lines := readLines(t, filepath.Join(directory, "trace.jsonl"))
	if len(lines) != n {
		t.Fatalf("expected %d lines, got %d", n, len(lines))
	}
	seen := make(map[int]bool, n)
	for _, line := range lines {
		var got Step
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("torn JSONL line: %v\n%s", err, line)
		}
		seen[got.Index] = true
	}
	if len(seen) != n {
		t.Errorf("expected %d distinct steps, got %d", n, len(seen))
	}
}

// Grep contract (intentional substring assertion, not a JSON round-trip):
// operators and CI scripts locate failing steps by grepping raw trace.jsonl for
// `"violations":["<prop>"]` without a JSON parser. Bug class: a serialization
// change (whitespace from indenting, renamed/reordered field, pointer slice)
// that keeps the Step parseable but breaks that exact on-disk byte shape would
// silently blind every grep-based tool.
func TestWriteStep_ViolationsAreGreppable(t *testing.T) {
	directory := t.TempDir()
	writer, _ := NewWriter(directory)
	defer writer.Close()

	_ = writer.WriteStep(Step{Index: 1})
	_ = writer.WriteStep(Step{Index: 2, Violations: []string{"prop1"}})
	_ = writer.WriteStep(Step{Index: 3})

	body, err := os.ReadFile(filepath.Join(directory, "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"violations":["prop1"]`) {
		t.Errorf("violations not in expected JSON shape: %s", body)
	}
}

func TestWriteScreenshot_CreatesPaddedFilenames(t *testing.T) {
	directory := t.TempDir()
	writer, _ := NewWriter(directory)
	defer writer.Close()

	pngBytes := []byte{0x89, 0x50, 0x4e, 0x47}
	if err := writer.WriteScreenshot(7, pngBytes); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteScreenshot(2024, pngBytes); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(directory, "screenshots", "step-00007.png"))
	if err != nil {
		t.Fatalf("step-00007 missing: %v", err)
	}
	if string(got) != string(pngBytes) {
		t.Errorf("screenshot bytes wrong")
	}
	if _, err := os.Stat(filepath.Join(directory, "screenshots", "step-02024.png")); err != nil {
		t.Errorf("step-02024 missing: %v", err)
	}
}

func TestWriteScreenshot_EmptyByteSliceIsNoop(t *testing.T) {
	directory := t.TempDir()
	writer, _ := NewWriter(directory)
	defer writer.Close()

	if err := writer.WriteScreenshot(1, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "screenshots")); !os.IsNotExist(err) {
		t.Errorf("screenshots dir should not exist after empty write")
	}
}

func TestWriteAfterClose_Errors(t *testing.T) {
	directory := t.TempDir()
	writer, _ := NewWriter(directory)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	err := writer.WriteStep(Step{Index: 1})
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("expected closed-writer error, got %v", err)
	}
}

func TestNewWriter_CreatesNestedDirectory(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "runs", "2026-04-17T22-30-00")
	writer, err := NewWriter(target)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := os.Stat(target); err != nil {
		t.Errorf("nested directory was not created: %v", err)
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}
