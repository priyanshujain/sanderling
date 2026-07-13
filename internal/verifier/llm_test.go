package verifier

import (
	"slices"
	"testing"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// candidateTreeJSON is a small screen with one clickable button, one editable
// field, and one scrollable list. Every node has positive bounds, so each is
// additionally a swipe origin.
const candidateTreeJSON = `{
  "attributes": {"bounds": "[0,0,1080,2400]"},
  "children": [
    {"attributes": {"resource-id": "LoginSubmit", "text": "Sign in", "bounds": "[0,100,1080,200]"}, "clickable": true, "enabled": true, "children": []},
    {"attributes": {"resource-id": "EmailField", "class": "EditText", "bounds": "[0,300,1080,400]"}, "enabled": true, "children": []},
    {"attributes": {"resource-id": "List", "scrollable": "true", "bounds": "[0,500,1080,2000]"}, "children": []}
  ]
}`

func TestAllCandidatesUnionsVerbsWithIndicesAndLabels(t *testing.T) {
	tree, err := hierarchy.Parse(candidateTreeJSON)
	if err != nil {
		t.Fatal(err)
	}
	v := &Verifier{lastTree: tree}
	candidates := v.AllCandidates()

	// Indices are dense and ordered.
	for i, candidate := range candidates {
		if candidate.Index != i {
			t.Errorf("candidate %d has Index %d", i, candidate.Index)
		}
	}

	// Collect verbs per label to assert the union without pinning swipe count.
	byLabel := map[string][]string{}
	for _, candidate := range candidates {
		byLabel[candidate.Label] = append(byLabel[candidate.Label], candidate.Verb)
	}

	// The clickable button is tap/doubleTap/longPress + swipe; its label is the
	// visible text, not the resource-id.
	submit := byLabel["Sign in"]
	if !contains(submit, "taps") || !contains(submit, "doubleTaps") || !contains(submit, "longPresses") {
		t.Errorf("Sign in verbs = %v, want tap family", submit)
	}
	if !contains(submit, "swipes") {
		t.Errorf("Sign in verbs = %v, want swipes (positive bounds)", submit)
	}
	if contains(submit, "typing") {
		t.Errorf("Sign in should not be typeable, got %v", submit)
	}

	// The EditText is typeable (and a swipe origin); its label falls back to the
	// resource-id since it has no text.
	email := byLabel["EmailField"]
	if !contains(email, "typing") {
		t.Errorf("EmailField verbs = %v, want typing", email)
	}
	if contains(email, "taps") {
		t.Errorf("EmailField is not clickable, got %v", email)
	}

	// The scrollable list yields a scroll candidate.
	list := byLabel["List"]
	if !contains(list, "scrolls") {
		t.Errorf("List verbs = %v, want scrolls", list)
	}

	// Kinds map verbs to action kinds.
	for _, candidate := range candidates {
		if candidate.Verb == "typing" && candidate.Kind != ActionKindInputText {
			t.Errorf("typing candidate kind = %q, want InputText", candidate.Kind)
		}
		if candidate.Verb == "taps" && candidate.Kind != ActionKindTap {
			t.Errorf("taps candidate kind = %q, want Tap", candidate.Kind)
		}
	}
}

func TestAllCandidatesNilTree(t *testing.T) {
	v := &Verifier{}
	if got := v.AllCandidates(); got != nil {
		t.Errorf("AllCandidates with no tree = %v, want nil", got)
	}
}

func TestLLMConfigDetectsMarker(t *testing.T) {
	v := newLoadedVerifier(t, `globalThis.actions = { kind: "llm", config: { model: "vendor/model" } };`)
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
	v := newLoadedVerifier(t, `globalThis.actions = { kind: "llm", config: { model: "m", instructions: "find bugs" } };`)
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
		t.Error("LLMConfig should be false for a non-llm actions root")
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

func contains(items []string, want string) bool {
	return slices.Contains(items, want)
}
