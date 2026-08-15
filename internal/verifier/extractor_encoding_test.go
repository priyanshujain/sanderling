package verifier

import (
	"bytes"
	"encoding/json"
	"testing"
)

const elementTreeJSON = `{
  "attributes": {"resource-id": "root", "bounds": "[0,0,400,800]"},
  "enabled": true,
  "children": [
    {"attributes": {"resource-id": "TxnAmountField", "text": "199", "bounds": "[0,100,400,160]"},
     "editable": true, "enabled": true, "children": []}
  ]
}`

const elementExtractorSpec = `
const field = __sanderling__.extract(state => state.ax.find({ "resource-id": "TxnAmountField" }), "field");
globalThis.properties = {};
`

// canonicalElement is the trace's record of one ax element, written out in the
// key order encoding/json emits. It is the contract both hosts owe the replay
// UI: an element the reader can read, with no host-function members and nothing
// dropped. Keys the two hosts disagree on (a DOM has no `checked`, a native
// tree has no `dataset`) are each host's own business; the ENCODING is not.
const canonicalElement = `{
	"__sanderlingSelector": "resource-id:TxnAmountField",
	"attrs": {
		"bounds": "[0,100,400,160]",
		"editable": "true",
		"enabled": "true",
		"resource-id": "TxnAmountField",
		"text": "199"
	},
	"bounds": {"bottom": 160, "left": 0, "right": 400, "top": 100},
	"checked": false,
	"class": "",
	"clickable": false,
	"desc": "",
	"editable": true,
	"enabled": true,
	"focused": false,
	"id": "TxnAmountField",
	"selected": false,
	"text": "199",
	"x": 200,
	"y": 130
}`

// TestExtractorEncoding_ElementIsIdenticalOnBothHosts holds the two extractor
// paths to one encoding of one element. The goja hosts (ios, android) run the
// getter in-process and encode the value it returned; the web host runs it in
// V8 and injects the page's reading through OverrideExtractorValues. A reader
// opening a trace does not know which host wrote it, so the same element has to
// land as the same bytes either way.
//
// The goja side used to write null here: an ax element carries find/findAll as
// host functions and json.Marshal refuses the whole object over them.
func TestExtractorEncoding_ElementIsIdenticalOnBothHosts(t *testing.T) {
	want := compactJSON(t, canonicalElement)

	native := newVerifier(t)
	mustLoad(t, native, elementExtractorSpec)
	pushTree(t, native, elementTreeJSON)
	fromGoja := string(native.extractors[0].curr)
	if fromGoja != want {
		t.Errorf("goja host encoded the element as\n %s\nwant\n %s", fromGoja, want)
	}

	web := newVerifier(t)
	mustLoad(t, web, elementExtractorSpec)
	if err := web.PushSnapshot(SnapshotInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := web.OverrideExtractorValues(map[int]json.RawMessage{0: json.RawMessage(want)}); err != nil {
		t.Fatal(err)
	}
	fromWeb := string(web.extractors[0].curr)
	if fromWeb != fromGoja {
		t.Errorf("the same element reaches the trace as\n %s\non the web host and\n %s\non goja",
			fromWeb, fromGoja)
	}
}

// TestExtractorEncoding_MirrorsTheWebSanitizeRule pins the goja host to the
// rule the web host applies before a reading leaves the page (sanitize in
// pkg/spec/src/web-runtime.ts, asserted there by the "sanitize ..." tests in
// pkg/spec/test/web-runtime.test.ts). Two hosts encoding one value two ways is
// the same defect as encoding it not at all: the reader cannot line the traces
// up.
func TestExtractorEncoding_MirrorsTheWebSanitizeRule(t *testing.T) {
	for _, test := range []struct {
		name       string
		expression string
		want       string
	}{
		{
			name:       "function-valued properties are dropped",
			expression: `({ keep: 1, fn: () => 7 })`,
			want:       `{"keep":1}`,
		},
		{
			name:       "a top-level function is not a value",
			expression: `(() => 7)`,
			want:       `null`,
		},
		{
			name:       "a self-referential cycle breaks instead of overflowing",
			expression: `(() => { const a = { name: "root" }; a.self = a; return a; })()`,
			want:       `{"name":"root","self":null}`,
		},
		{
			name:       "arrays and nested plain values are preserved",
			expression: `({ items: [1, "two", { ok: true }] })`,
			want:       `{"items":[1,"two",{"ok":true}]}`,
		},
		{
			name:       "a non-finite number is not a value",
			expression: `Number("nope")`,
			want:       `null`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := encodeSpecValue(t, test.expression); got != test.want {
				t.Errorf("encoded as %s, want %s", got, test.want)
			}
		})
	}
}

// TestExtractorEncoding_BoundsRecursionPastTheDepthLimit mirrors the web host's
// depth cap. state.ax hands out no cyclic element, but a spec returning a value
// it built itself can nest without end, and a walk with no bound takes the run
// down with a stack overflow.
func TestExtractorEncoding_BoundsRecursionPastTheDepthLimit(t *testing.T) {
	encoded := encodeSpecValue(t, `(() => {
		let deep = { leaf: true };
		for (let i = 0; i < 40; i++) deep = { next: deep };
		return deep;
	})()`)

	var node any
	if err := json.Unmarshal([]byte(encoded), &node); err != nil {
		t.Fatalf("decode %s: %v", encoded, err)
	}
	for depth := 0; depth < recordableMaxDepth; depth++ {
		object, ok := node.(map[string]any)
		if !ok {
			t.Fatalf("depth %d: recursion stopped early at %v", depth, node)
		}
		node = object["next"]
	}
	if node != nil {
		t.Errorf("depth %d is %v, want null", recordableMaxDepth, node)
	}
}

// encodeSpecValue returns what the trace records for an extractor whose getter
// returned the given expression.
func encodeSpecValue(t *testing.T, expression string) string {
	t.Helper()
	verifier := newVerifier(t)
	mustLoad(t, verifier, "__sanderling__.extract(state => "+expression+", \"value\");\nglobalThis.properties = {};")
	if err := verifier.PushSnapshot(SnapshotInput{}); err != nil {
		t.Fatal(err)
	}
	return string(verifier.extractors[0].curr)
}

func compactJSON(t *testing.T, source string) string {
	t.Helper()
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(source)); err != nil {
		t.Fatal(err)
	}
	return compact.String()
}
