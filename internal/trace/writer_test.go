package trace

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		SpecPath:     "examples/specs/merchant-ledger.ts",
		BundleSHA256: "deadbeef",
		Platform:     "android",
		BundleID:     "in.okcredit.merchant",
		StartedAt:    time.Date(2026, 4, 17, 22, 30, 0, 0, time.UTC),
		UatuVersion:  "0.0.1",
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
		Action:     &Action{Kind: "tap", X: 100, Y: 200},
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
	if got.Index != 1 || got.Screen != "customer_ledger" || got.Action.X != 100 || got.Violations[0] != "ledgerBalanceMatchesTxns" {
		t.Errorf("step round-trip wrong: %+v", got)
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
