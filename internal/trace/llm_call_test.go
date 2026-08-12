package trace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWriteLLMCall_RoundTrip(t *testing.T) {
	directory := t.TempDir()
	writer, err := NewWriter(directory)
	if err != nil {
		t.Fatal(err)
	}

	call := LLMCall{
		Step:        4,
		Timestamp:   time.Date(2026, 8, 12, 9, 30, 0, 0, time.UTC),
		Outcome:     LLMOutcomeSelected,
		Model:       "vendor/model",
		ServedModel: "vendor/model-2026-05",
		SystemPrompt: "You are exercising a UI to find bugs.\n\n" +
			"hunt for double submits",
		UserPrompt: "Actions available on the current screen:\n1. Tap \"Submit\"  (w60)\n",
		Candidates: []LLMCandidate{
			{Index: 1, Kind: "tap", Description: `Tap "Submit"`, Label: "Submit", Weight: 60},
			{Index: 2, Kind: "inputText", Description: `Type into "Name"`, Label: "Name", Weight: 40},
		},
		Screenshot:       ScreenshotReference(4),
		RawResponse:      `{"reasoning":"submit twice","choice":1,"chosen_action":"Tap \"Submit\"","text":""}`,
		PromptTokens:     1200,
		CompletionTokens: 34,
		TotalTokens:      1234,
		LatencyMillis:    812,
		Choice:           1,
		EchoedAction:     `Tap "Submit"`,
		Reasoning:        "submit twice",
	}
	if err := writer.WriteLLMCall(call); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteLLMCall(LLMCall{
		Step:      5,
		Timestamp: call.Timestamp.Add(time.Second),
		Outcome:   LLMOutcomeNoCandidates,
	}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(directory, LLMCallFileName))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 2 {
		t.Fatalf("wrote %d lines, want 2", len(lines))
	}
	var got LLMCall
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("%s line 1 is not valid JSON: %v", LLMCallFileName, err)
	}
	if !reflect.DeepEqual(got, call) {
		t.Errorf("round-trip mismatch:\n got: %+v\nwant: %+v", got, call)
	}

	var declined LLMCall
	if err := json.Unmarshal([]byte(lines[1]), &declined); err != nil {
		t.Fatal(err)
	}
	if declined.Step != 5 || declined.Outcome != LLMOutcomeNoCandidates {
		t.Errorf("second record = %+v, want step 5 with outcome %q", declined, LLMOutcomeNoCandidates)
	}
}

// TestWriteLLMCall_FileAbsentWithoutCalls keeps a seeded run's directory free of
// model-call output, so its presence alone identifies a model-driven run.
func TestWriteLLMCall_FileAbsentWithoutCalls(t *testing.T) {
	directory := t.TempDir()
	writer, err := NewWriter(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteStep(Step{Index: 1, Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, LLMCallFileName)); !os.IsNotExist(err) {
		t.Errorf("stat %s = %v, want the file never to be created", LLMCallFileName, err)
	}
}

// TestScreenshotReferenceMatchesWrittenFile pins the reference records point at
// to the path WriteScreenshot actually writes.
func TestScreenshotReferenceMatchesWrittenFile(t *testing.T) {
	directory := t.TempDir()
	writer, err := NewWriter(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	if err := writer.WriteScreenshot(12, []byte("not really a png")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, ScreenshotReference(12))); err != nil {
		t.Errorf("stat %s: %v", ScreenshotReference(12), err)
	}
}

func TestWriteLLMCall_AfterCloseErrors(t *testing.T) {
	directory := t.TempDir()
	writer, err := NewWriter(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteLLMCall(LLMCall{Step: 1}); err == nil {
		t.Error("expected an error writing to a closed writer")
	}
}
