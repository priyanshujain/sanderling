package runner

import (
	"encoding/json"
	"fmt"
	"testing"
)

// elementExtractorSpec reads a live ax element, the shape every field and
// button in examples/folio/sanderling/spec.ts is extracted with. The property
// is false the moment the field is on screen, so the run records a witness
// whose only interesting content is that element.
const elementExtractorSpec = `
import { actions, always, extract } from "@sanderling/spec";
const amountField = extract("amountField", s => s.ax.find({ "resource-id": "TxnAmountField" }));
globalThis.properties = {
  noAmountField: always(() => amountField.current === undefined),
};
globalThis.actions = actions(() => []);
`

const amountFieldTreeJSON = `{
  "attributes": {"resource-id": "root", "bounds": "[0,0,400,800]"},
  "enabled": true,
  "children": [
    {"attributes": {"resource-id": "TxnAmountField", "text": "199", "bounds": "[0,100,400,160]"},
     "editable": true, "enabled": true, "children": []}
  ]
}`

// TestRunner_TraceRecordsElementValuedExtractors is the guard on the artifact a
// person opens to decide whether a conviction is real. An element-valued
// extractor used to reach the trace as null on the goja hosts (ios, android):
// its exported value carries the element's find/findAll host functions, which
// json.Marshal refuses, so the encoding failed and both the per-step diff and
// the witness recorded nothing. A witness that reads null for the field the
// property fired on describes a state the property could not have fired in,
// which is worse than a blank.
func TestRunner_TraceRecordsElementValuedExtractors(t *testing.T) {
	state := newHarnessWithSpec(t, elementExtractorSpec)
	state.mock.HierarchyJSON = amountFieldTreeJSON

	summary := state.run(t, Options{MaxSteps: 2})
	if !containsProperty(summary.Violations, "noAmountField") {
		t.Fatalf("noAmountField did not violate, so the element never reached a predicate: %v",
			summary.Violations)
	}

	changes, witnesses := 0, 0
	for _, line := range readTraceLines(t, state.writer.Directory()) {
		if change, ok := line.ExtractorChanges["amountField"]; ok {
			changes++
			assertAmountField(t, fmt.Sprintf("step %d extractor_changes", line.Step), change.Curr)
		}
		for name, witness := range line.Witnesses {
			witnesses++
			assertAmountField(t, fmt.Sprintf("step %d %s witness", line.Step, name),
				witness.Extractors["amountField"])
		}
	}
	if changes == 0 {
		t.Error("amountField never appears in extractor_changes; the element the run read is not in the trace")
	}
	if witnesses == 0 {
		t.Error("no witness reached the trace; nothing was compared")
	}
}

// assertAmountField reads the recorded element the way a person opening the
// trace would: the field's text is the number the property was judged on.
func assertAmountField(t *testing.T, where string, recorded json.RawMessage) {
	t.Helper()
	var element struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(recorded, &element); err != nil {
		t.Fatalf("%s: decode %s: %v", where, recorded, err)
	}
	if element.Text != "199" {
		t.Errorf("%s: recorded element is %s, want its text to read 199", where, recorded)
	}
}
