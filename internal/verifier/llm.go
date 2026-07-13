package verifier

import (
	"errors"

	"github.com/dop251/goja"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// LLMConfig is the spec-declared configuration for the LLM action backend,
// read off globalThis.actions when the spec assigned `actions = llm({...})`.
type LLMConfig struct {
	Model string
	// Instructions is optional spec-level guidance appended to the prompt to
	// steer the model toward bug-hunting (empty when unset).
	Instructions string
}

// LLMConfig reports the LLM action backend config when the spec selected it
// (globalThis.actions.kind === "llm"). The second return is false for every
// other spec, so the runner falls back to the seeded picker.
func (v *Verifier) LLMConfig() (LLMConfig, bool) {
	actions := v.runtime.GlobalObject().Get("actions")
	if actions == nil || goja.IsUndefined(actions) || goja.IsNull(actions) {
		return LLMConfig{}, false
	}
	object := actions.ToObject(v.runtime)
	if object == nil {
		return LLMConfig{}, false
	}
	kind := object.Get("kind")
	if kind == nil || kind.String() != "llm" {
		return LLMConfig{}, false
	}
	config := object.Get("config")
	if config == nil || goja.IsUndefined(config) || goja.IsNull(config) {
		return LLMConfig{}, false
	}
	configObject := config.ToObject(v.runtime)
	if configObject == nil {
		return LLMConfig{}, false
	}
	model := ""
	if value := configObject.Get("model"); value != nil && !goja.IsUndefined(value) {
		model = value.String()
	}
	instructions := ""
	if value := configObject.Get("instructions"); value != nil && !goja.IsUndefined(value) && !goja.IsNull(value) {
		instructions = value.String()
	}
	return LLMConfig{Model: model, Instructions: instructions}, true
}

// Screenshot returns the most recent step's screenshot PNG (set by
// PushSnapshot), or nil if none was captured.
func (v *Verifier) Screenshot() []byte {
	return v.lastScreenshot
}

// CurrentScreen returns the screen id of the most recent snapshot's first
// element, matching the runner's own screen labeling. Empty when no tree is
// loaded.
func (v *Verifier) CurrentScreen() string {
	if v.lastTree == nil || len(v.lastTree.Elements) == 0 {
		return ""
	}
	return v.lastTree.Elements[0].Screen
}

// SampleInput draws one InputText value from the shared corpus via the bundled
// __sanderlingSampleInput__. It errors when the bundle did not install the
// callable (a raw-JS fixture) so the caller can skip typing rather than send an
// empty string.
func (v *Verifier) SampleInput() (string, error) {
	if v.sampleInputFn == nil {
		return "", errors.New("verifier: input sampler not available")
	}
	value, err := v.sampleInputFn(goja.Undefined())
	if err != nil {
		return "", err
	}
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return "", nil
	}
	return value.String(), nil
}

// ActionCandidate is one selectable action the LLM backend may choose from: an
// applicable verb on an in-scope element, resolved to coordinates and a
// selector. It is the existing per-verb enumeration (candidatesForVerb),
// unioned across verbs and flattened into one indexed list.
type ActionCandidate struct {
	// Index is the candidate's position in the AllCandidates list; the LLM
	// returns these indices.
	Index int
	// Verb is the picker verb that produced this candidate
	// (taps/doubleTaps/longPresses/typing/scrolls/swipes).
	Verb string
	// Kind is the resulting action kind, shown to the model as the candidate's
	// kind label.
	Kind ActionKind
	// Label is a short human-readable target description (text, then
	// description, then resource-id, then class).
	Label string
	// X, Y are the target element center.
	X, Y int
	// Width, Height are the target bounds, used to size swipe/scroll gestures.
	Width, Height int
	// Selector resolves the target back to an element by id/text, or "" when no
	// selector uniquely resolves.
	Selector string
}

// llmVerbs lists the verbs AllCandidates enumerates, in the order they are
// emitted per element. Mirrors verbAccepts; no new filtering logic.
var llmVerbs = []string{"taps", "doubleTaps", "longPresses", "typing", "scrolls", "swipes"}

// AllCandidates flattens the per-verb candidate enumeration into one indexed
// list the LLM backend chooses from. It reuses scopedElements/verbAccepts/
// selectorForElement exactly as the seeded picker does, walking the tree once
// and emitting an entry for every (in-scope element, applicable verb) pair.
func (v *Verifier) AllCandidates() []ActionCandidate {
	if v.lastTree == nil {
		return nil
	}
	scope := v.scopedElements()
	var result []ActionCandidate
	for _, element := range v.lastTree.Elements {
		if !scope[element] {
			continue
		}
		for _, verb := range llmVerbs {
			if !verbAccepts(verb, element) {
				continue
			}
			x, y := element.Bounds.Center()
			result = append(result, ActionCandidate{
				Index:    len(result),
				Verb:     verb,
				Kind:     verbActionKind(verb),
				Label:    candidateLabel(element),
				X:        x,
				Y:        y,
				Width:    element.Bounds.Width(),
				Height:   element.Bounds.Height(),
				Selector: selectorForElement(v.lastTree, element),
			})
		}
	}
	return result
}

// verbActionKind maps a picker verb to the action kind it dispatches.
func verbActionKind(verb string) ActionKind {
	switch verb {
	case "taps":
		return ActionKindTap
	case "doubleTaps":
		return ActionKindDoubleTap
	case "longPresses":
		return ActionKindLongPress
	case "typing":
		return ActionKindInputText
	case "scrolls":
		return ActionKindScroll
	case "swipes":
		return ActionKindSwipe
	default:
		return ""
	}
}

// candidateLabel builds a short target description, preferring the most
// human-meaningful field available.
func candidateLabel(element *hierarchy.Element) string {
	switch {
	case element.Text != "":
		return element.Text
	case element.Description != "":
		return element.Description
	case element.ResourceID != "":
		return element.ResourceID
	default:
		return element.Class
	}
}
