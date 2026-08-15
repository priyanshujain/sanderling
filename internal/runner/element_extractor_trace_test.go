package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/trace"
)

// elementExtractorSpec records accessibility elements directly, the shape an
// author reaches for when the question is "what was on screen at this step":
// one element and the list a generator would have been offered.
const elementExtractorSpec = `
import { actions, extract } from "@sanderling/spec";
extract("field", state => state.ax.find({ testTag: "LoginEmail" }));
extract("rows", state => state.ax.findAll({ testTag: "Row" }));
globalThis.properties = {};
globalThis.actions = actions(() => []);
`

const elementExtractorHierarchy = `{
  "attributes": {"class": "android.widget.LinearLayout", "bounds": "[0,0,1080,2340]"},
  "children": [
    {"attributes": {"resource-id": "LoginEmail", "class": "android.widget.EditText",
      "text": "you@example.com", "bounds": "[10,20,200,60]"}, "children": []},
    {"attributes": {"resource-id": "Row", "class": "android.widget.TextView",
      "text": "first", "bounds": "[0,100,1080,200]"}, "children": []},
    {"attributes": {"resource-id": "Row", "class": "android.widget.TextView",
      "text": "second", "bounds": "[0,200,1080,300]"}, "children": []}
  ]
}`

// TestRunner_TraceRecordsElementValuedExtractors pins that an extractor holding
// an accessibility element reaches the trace. Elements carry host functions
// (find/findAll), which json.Marshal refuses; the encoder used to answer nil
// and the diff then emitted no entry, so the run finished clean with the
// extractor missing from every step and no error anywhere.
func TestRunner_TraceRecordsElementValuedExtractors(t *testing.T) {
	state := newHarnessWithSpec(t, elementExtractorSpec)
	state.mock.HierarchyJSON = elementExtractorHierarchy

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Run(ctx, Options{
		Duration:    100 * time.Millisecond,
		IdleTimeout: 20 * time.Millisecond,
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	field, rows := elementChangesFromTrace(t, state.directory)
	if field == nil {
		t.Fatal("no extractor change for the element-valued extractor reached the trace")
	}
	if rows == nil {
		t.Fatal("no extractor change for the element-list extractor reached the trace")
	}

	var element map[string]any
	if err := json.Unmarshal(field, &element); err != nil {
		t.Fatalf("field value is not a JSON object: %v (%s)", err, field)
	}
	if got := element["id"]; got != "LoginEmail" {
		t.Errorf("field.id = %v, want LoginEmail", got)
	}
	if got := element["text"]; got != "you@example.com" {
		t.Errorf("field.text = %v, want you@example.com", got)
	}
	if got := element["class"]; got != "android.widget.EditText" {
		t.Errorf("field.class = %v, want android.widget.EditText", got)
	}
	bounds, ok := element["bounds"].(map[string]any)
	if !ok {
		t.Fatalf("field.bounds missing or not an object: %v", element["bounds"])
	}
	if bounds["left"] != float64(10) || bounds["top"] != float64(20) ||
		bounds["right"] != float64(200) || bounds["bottom"] != float64(60) {
		t.Errorf("field.bounds = %v, want left/top/right/bottom 10/20/200/60", bounds)
	}
	for _, key := range []string{"find", "findAll"} {
		if _, present := element[key]; present {
			t.Errorf("field carries the host function %q into the trace", key)
		}
	}

	var list []map[string]any
	if err := json.Unmarshal(rows, &list); err != nil {
		t.Fatalf("rows value is not a JSON array: %v (%s)", err, rows)
	}
	if len(list) != 2 {
		t.Fatalf("rows recorded %d elements, want 2", len(list))
	}
	if list[0]["text"] != "first" || list[1]["text"] != "second" {
		t.Errorf("rows recorded %v, want the two Row elements in tree order", list)
	}
}

// elementChangesFromTrace returns the first recorded value of each extractor.
func elementChangesFromTrace(t *testing.T, directory string) (field, rows json.RawMessage) {
	t.Helper()
	file, err := os.Open(filepath.Join(directory, "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var line struct {
			ExtractorChanges map[string]trace.ExtractorChange `json:"extractor_changes"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("trace line decode: %v", err)
		}
		if change, ok := line.ExtractorChanges["field"]; ok && field == nil {
			field = change.Curr
		}
		if change, ok := line.ExtractorChanges["rows"]; ok && rows == nil {
			rows = change.Curr
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	return field, rows
}
