package verifier

import (
	"strings"
	"testing"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// enumTreeJSON exercises every labeling path: a clickable wrapper whose own text
// is empty but whose child Text reads "Add credit" (descendant borrowing), an
// editable field labeled by its hint, a text-labeled button, a DISABLED button,
// and a scrollable list (the only valid gesture origin).
const enumTreeJSON = `{
  "attributes": {"bounds": "[0,0,1080,2400]"},
  "children": [
    {"attributes": {"resource-id": "AddCredit", "bounds": "[0,100,1080,200]"}, "clickable": true, "enabled": true, "children": [
      {"attributes": {"text": "Add credit", "bounds": "[0,100,540,200]"}, "children": []}
    ]},
    {"attributes": {"resource-id": "Amount", "class": "EditText", "hintText": "Amount", "bounds": "[0,300,1080,400]"}, "enabled": true, "children": []},
    {"attributes": {"resource-id": "SignIn", "text": "Sign in", "bounds": "[0,450,1080,550]"}, "clickable": true, "enabled": true, "children": []},
    {"attributes": {"resource-id": "Off", "text": "Off", "bounds": "[0,600,1080,700]"}, "clickable": true, "enabled": false, "children": []},
    {"attributes": {"resource-id": "List", "scrollable": "true", "bounds": "[0,800,1080,2000]"}, "children": []}
  ]
}`

// enumVerifier loads a spec whose actions root is the given plain-object graph
// and stages the given tree, so Candidates walks a controlled action tree.
func enumVerifier(t *testing.T, actionsJS, treeJSON string) *Verifier {
	t.Helper()
	v := newLoadedVerifier(t, "globalThis.actions = "+actionsJS+";")
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatalf("parse tree: %v", err)
	}
	v.lastTree = tree
	return v
}

func findCandidate(candidates []ActionCandidate, description string) (ActionCandidate, bool) {
	for _, candidate := range candidates {
		if candidate.Description == description {
			return candidate, true
		}
	}
	return ActionCandidate{}, false
}

func hasCandidate(candidates []ActionCandidate, description string) bool {
	_, ok := findCandidate(candidates, description)
	return ok
}

func TestCandidatesLabelsControlsByVisibleText(t *testing.T) {
	v := enumVerifier(t, "{kind:'builtin', verb:'taps'}", enumTreeJSON)
	candidates := v.Candidates()

	// The empty-text clickable wrapper is labeled by its child Text, NOT its
	// resource-id.
	if !hasCandidate(candidates, `Tap "Add credit"`) {
		t.Errorf("want Tap \"Add credit\" (descendant text), got %v", descriptions(candidates))
	}
	// The plain text button is labeled by its own text.
	if !hasCandidate(candidates, `Tap "Sign in"`) {
		t.Errorf("want Tap \"Sign in\", got %v", descriptions(candidates))
	}
	// Descriptions are never the opaque resource-id.
	if hasCandidate(candidates, `Tap "AddCredit"`) {
		t.Error("labeled a control by its resource-id instead of visible text")
	}
	// Indices are dense and 1-based.
	for i, candidate := range candidates {
		if candidate.Index != i+1 {
			t.Errorf("candidate %d has Index %d, want %d", i, candidate.Index, i+1)
		}
	}
}

func TestCandidatesDropsDisabledControls(t *testing.T) {
	v := enumVerifier(t, "{kind:'builtin', verb:'taps'}", enumTreeJSON)
	for _, candidate := range v.Candidates() {
		if strings.Contains(candidate.Description, "Off") {
			t.Errorf("disabled control surfaced as %q", candidate.Description)
		}
	}
}

func TestCandidatesTypingExposesInputType(t *testing.T) {
	v := enumVerifier(t, "{kind:'builtin', verb:'typing'}", enumTreeJSON)
	candidates := v.Candidates()
	candidate, ok := findCandidate(candidates, `Type into "Amount" (number)`)
	if !ok {
		t.Fatalf("want typing candidate with input type, got %v", descriptions(candidates))
	}
	if !candidate.LLMText {
		t.Error("builtin typing must flag LLMText so the model supplies the value")
	}
	if candidate.InputType != "number" {
		t.Errorf("InputType = %q, want number", candidate.InputType)
	}
}

func TestCandidatesLabelsEditableFieldByHintNotTypedValue(t *testing.T) {
	// A field already showing "99" must still be labeled by its purpose (the
	// hint), not by its transient content, so the description stays stable.
	tree := `{
      "attributes": {"bounds": "[0,0,400,800]"},
      "children": [
        {"attributes": {"resource-id": "Amt", "class": "EditText", "hintText": "Amount", "text": "99", "bounds": "[0,0,400,100]"}, "enabled": true, "children": []}
      ]
    }`
	v := enumVerifier(t, "{kind:'builtin', verb:'typing'}", tree)
	candidates := v.Candidates()
	if hasCandidate(candidates, `Type into "99" (number)`) || hasCandidate(candidates, `Type into "99"`) {
		t.Errorf("editable field labeled by its typed value: %v", descriptions(candidates))
	}
	if !hasCandidate(candidates, `Type into "Amount" (number)`) {
		t.Errorf("want the field labeled by its hint, got %v", descriptions(candidates))
	}
}

func TestCandidatesFoldsGesturesIntoDirectionalScrolls(t *testing.T) {
	v := enumVerifier(t,
		"{kind:'weighted', branches:[[1,{kind:'builtin',verb:'scrolls'}],[1,{kind:'builtin',verb:'swipes'}]]}",
		enumTreeJSON)
	candidates := v.Candidates()

	// Gestures are directional and scoped to the one scrollable container: no
	// per-element, element-labeled Swipe entries.
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate.Description, "Swipe") {
			t.Errorf("gesture kept as element-labeled swipe: %q", candidate.Description)
		}
	}
	if !hasCandidate(candidates, "Scroll down") || !hasCandidate(candidates, "Scroll up") {
		t.Errorf("want directional scrolls, got %v", descriptions(candidates))
	}
	// scrolls and swipes fold into the SAME directional entries: one each.
	if got := count(candidates, "Scroll down"); got != 1 {
		t.Errorf("Scroll down appears %d times, want 1 (folded)", got)
	}
}

func TestCandidatesWeightsCombineAcrossPaths(t *testing.T) {
	// A single clickable reached through two equal branches: its weight sums to
	// the full distribution.
	oneClickable := `{
      "attributes": {"bounds": "[0,0,400,800]"},
      "children": [
        {"attributes": {"resource-id": "SignIn", "text": "Sign in", "bounds": "[0,0,400,100]"}, "clickable": true, "enabled": true, "children": []}
      ]
    }`
	v := enumVerifier(t,
		"{kind:'weighted', branches:[[1,{kind:'builtin',verb:'taps'}],[1,{kind:'builtin',verb:'taps'}]]}",
		oneClickable)
	candidates := v.Candidates()
	if len(candidates) != 1 {
		t.Fatalf("want one deduped candidate, got %v", descriptions(candidates))
	}
	candidate := candidates[0]
	if !candidate.Weighted {
		t.Fatal("candidate under a weighted tree must be Weighted")
	}
	if candidate.Weight != 100 {
		t.Errorf("summed weight = %d, want 100", candidate.Weight)
	}
}

func TestCandidatesWeightReflectsBranchShare(t *testing.T) {
	v := enumVerifier(t,
		"{kind:'weighted', branches:[[1,{kind:'builtin',verb:'taps'}],[3,{kind:'builtin',verb:'typing'}]]}",
		enumTreeJSON)
	candidates := v.Candidates()
	tap, ok := findCandidate(candidates, `Tap "Sign in"`)
	if !ok {
		t.Fatalf("missing tap candidate: %v", descriptions(candidates))
	}
	if tap.Weight != 25 {
		t.Errorf("tap weight = %d, want 25 (1/4 share)", tap.Weight)
	}
	typing, ok := findCandidate(candidates, `Type into "Amount" (number)`)
	if !ok {
		t.Fatalf("missing typing candidate: %v", descriptions(candidates))
	}
	if typing.Weight != 75 {
		t.Errorf("typing weight = %d, want 75 (3/4 share)", typing.Weight)
	}
}

func TestCandidatesUnweightedTreeShowsNoWeight(t *testing.T) {
	v := enumVerifier(t, "{kind:'builtin', verb:'taps'}", enumTreeJSON)
	for _, candidate := range v.Candidates() {
		if candidate.Weighted || candidate.Weight != 0 {
			t.Errorf("%q carries a weight despite no weighted node", candidate.Description)
		}
	}
}

func TestCandidatesCallsAuthoredLeafOnce(t *testing.T) {
	actions := `{kind:'actions', generate: () => [
      {kind:'Tap', on:'id:SignIn'},
      {kind:'Tap', on:'id:Off'},
      {kind:'InputText', into:'id:Amount', text:'42'}
    ]}`
	v := enumVerifier(t, actions, enumTreeJSON)
	candidates := v.Candidates()

	// Authored Tap resolves its selector to the visible-text label.
	if !hasCandidate(candidates, `Tap "Sign in"`) {
		t.Errorf("authored tap missing: %v", descriptions(candidates))
	}
	// A disabled authored target is dropped.
	for _, candidate := range candidates {
		if strings.Contains(candidate.Description, "Off") {
			t.Errorf("authored action on disabled control surfaced: %q", candidate.Description)
		}
	}
	// Authored InputText replays its own sampled value (LLM does not supply it).
	authored, ok := findCandidate(candidates, `Type "42" into "Amount"`)
	if !ok {
		t.Fatalf("authored typing missing: %v", descriptions(candidates))
	}
	if authored.LLMText {
		t.Error("authored InputText must not request an LLM-supplied value")
	}
	if authored.Action.Text != "42" {
		t.Errorf("authored text = %q, want 42", authored.Action.Text)
	}
}

func TestCandidatesOffRouteLeafYieldsNothing(t *testing.T) {
	v := enumVerifier(t, "{kind:'actions', generate: () => []}", enumTreeJSON)
	if got := v.Candidates(); len(got) != 0 {
		t.Errorf("off-route leaf should yield no candidates, got %v", descriptions(got))
	}
}

func TestCandidatesSkipsCrossFadeFrames(t *testing.T) {
	// Two route *Screen tags alive at once is a NavHost cross-fade: its layout is
	// mid-animation (collapsed coordinate space), so the LLM must NOT act on it.
	crossFade := `{
      "attributes": {"bounds": "[0,0,320,640]"},
      "children": [
        {"attributes": {"resource-id": "LedgerScreen", "bounds": "[0,0,320,640]"}, "children": [
          {"attributes": {"resource-id": "TxnSubmit", "text": "Add credit", "bounds": "[20,332,300,380]"}, "clickable": true, "enabled": true, "children": []}
        ]},
        {"attributes": {"resource-id": "AddTransactionScreen", "bounds": "[0,0,320,640]"}, "children": []}
      ]
    }`
	v := enumVerifier(t, "{kind:'builtin', verb:'taps'}", crossFade)
	if got := v.Candidates(); got != nil {
		t.Errorf("cross-fade frame should yield no candidates, got %v", descriptions(got))
	}
}

func TestCandidatesNilWithoutTreeOrActions(t *testing.T) {
	withActions := newLoadedVerifier(t, "globalThis.actions = {kind:'builtin', verb:'taps'};")
	if got := withActions.Candidates(); got != nil {
		t.Errorf("Candidates with no tree = %v, want nil", got)
	}
	noActions := newLoadedVerifier(t, "globalThis.properties = {};")
	tree, _ := hierarchy.Parse(enumTreeJSON)
	noActions.lastTree = tree
	if got := noActions.Candidates(); got != nil {
		t.Errorf("Candidates with no actions root = %v, want nil", got)
	}
}

func descriptions(candidates []ActionCandidate) []string {
	out := make([]string, len(candidates))
	for i, candidate := range candidates {
		out[i] = candidate.Description
	}
	return out
}

func count(candidates []ActionCandidate, description string) int {
	n := 0
	for _, candidate := range candidates {
		if candidate.Description == description {
			n++
		}
	}
	return n
}

func TestLLMConfigDetectsMarker(t *testing.T) {
	v := newLoadedVerifier(t, `globalThis.generator = { kind: "llm", config: { model: "vendor/model" } };`)
	config, ok := v.LLMConfig()
	if !ok {
		t.Fatal("LLMConfig not detected for llm marker")
	}
	if config.Model != "vendor/model" {
		t.Errorf("model = %q, want vendor/model", config.Model)
	}
	if config.Instructions != "" {
		t.Errorf("instructions = %q, want empty when unset", config.Instructions)
	}
}

func TestLLMConfigReadsInstructions(t *testing.T) {
	v := newLoadedVerifier(t, `globalThis.generator = { kind: "llm", config: { model: "m", instructions: "find bugs" } };`)
	config, ok := v.LLMConfig()
	if !ok {
		t.Fatal("LLMConfig not detected")
	}
	if config.Instructions != "find bugs" {
		t.Errorf("instructions = %q, want %q", config.Instructions, "find bugs")
	}
}

func TestLLMConfigAbsentForSeededSpec(t *testing.T) {
	v := newLoadedVerifier(t, `globalThis.actions = { kind: "builtin", verb: "taps" };`)
	if _, ok := v.LLMConfig(); ok {
		t.Error("LLMConfig should be false when no generator is declared")
	}
}

func TestSampleInputErrorsWithoutBundle(t *testing.T) {
	v := newLoadedVerifier(t, `globalThis.actions = { kind: "llm", config: { model: "m" } };`)
	if _, err := v.SampleInput(); err == nil {
		t.Error("expected SampleInput to error when the sampler is not installed")
	}
}

func TestSampleInputDrawsFromCorpus(t *testing.T) {
	v := newLoadedVerifier(t, `globalThis.__sanderlingSampleInput__ = () => "sampled";`)
	got, err := v.SampleInput()
	if err != nil {
		t.Fatalf("SampleInput: %v", err)
	}
	if got != "sampled" {
		t.Errorf("SampleInput = %q, want sampled", got)
	}
}

func newLoadedVerifier(t *testing.T, source string) *Verifier {
	t.Helper()
	v, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := v.Load(source); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return v
}


