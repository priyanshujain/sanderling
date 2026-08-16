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

// TestExtractorEncoding_NestedUndefinedIsNotOnTheWire pins the one reading
// shape the two hosts do NOT encode alike, rather than hiding it.
//
// JSON has no undefined, so the page loses the whole key (asserted in
// pkg/spec/test/web-runtime.test.ts) while goja writes null. goja cannot mirror
// the drop: Export reports an undefined member and a null member identically as
// nil, so dropping those keys here would drop the genuine nulls the page keeps.
// Mirroring the other way, by writing null on the page, would break the one
// thing that does agree. Carrying the member across takes a wire format that
// can express undefined, which is a change to every layer that parses a reading
// and to the replay UI that renders one.
//
// So the guarantee is narrower than "the same object": both hosts answer
// undefined when a property READS the member. Key presence (`in`, Object.keys)
// is not part of it, and this test says so out loud, so closing the gap has to
// be a deliberate change to both hosts at once.
func TestExtractorEncoding_NestedUndefinedIsNotOnTheWire(t *testing.T) {
	const reading = `({ absent: undefined, empty: null, present: 1 })`
	const fromGoja = `{"absent":null,"empty":null,"present":1}`
	// What the page sends for the same getter, with the key gone.
	const fromWeb = `{"empty":null,"present":1}`

	if got := encodeSpecValue(t, reading); got != fromGoja {
		t.Errorf("goja encoded the reading as %s, want %s", got, fromGoja)
	}

	native := newVerifier(t)
	mustLoad(t, native, "__sanderling__.extract(state => "+reading+", \"value\");\nglobalThis.properties = {};")
	if err := native.PushSnapshot(SnapshotInput{}); err != nil {
		t.Fatal(err)
	}

	web := newVerifier(t)
	mustLoad(t, web, "__sanderling__.extract(state => null, \"value\");\nglobalThis.properties = {};")
	if err := web.PushSnapshot(SnapshotInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := web.OverrideExtractorValues(map[int]json.RawMessage{0: json.RawMessage(fromWeb)}); err != nil {
		t.Fatal(err)
	}

	for _, probe := range []struct {
		expression string
		native     bool
		web        bool
	}{
		{"reading.absent === undefined", true, true},
		{"reading.empty === null", true, true},
		{"reading.present === 1", true, true},
		// The half that does not survive the wire.
		{`"absent" in reading`, true, false},
	} {
		if got := evaluateAgainstReading(t, native, probe.expression); got != probe.native {
			t.Errorf("goja host: %s is %v, want %v", probe.expression, got, probe.native)
		}
		if got := evaluateAgainstReading(t, web, probe.expression); got != probe.web {
			t.Errorf("web host: %s is %v, want %v", probe.expression, got, probe.web)
		}
	}
}

// evaluateAgainstReading answers a boolean expression over the value a property
// would read out of the first extractor, which is where the two hosts have to
// agree.
func evaluateAgainstReading(t *testing.T, verifier *Verifier, expression string) bool {
	t.Helper()
	if err := verifier.runtime.GlobalObject().Set("reading", verifier.extractors[0].currentValue); err != nil {
		t.Fatal(err)
	}
	value, err := verifier.runtime.RunString(expression)
	if err != nil {
		t.Fatalf("evaluate %s: %v", expression, err)
	}
	return value.ToBoolean()
}
