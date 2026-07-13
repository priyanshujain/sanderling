package verifier

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/dop251/goja"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// LLMConfig is the spec-declared configuration for the LLM action generator,
// read off globalThis.generator when the spec assigned `generator = llm({...})`.
// It is orthogonal to globalThis.actions (the weighted tree the LLM picks from);
// only the picker differs.
type LLMConfig struct {
	Model string
	// Instructions is optional spec-level guidance appended to the prompt to
	// steer the model toward bug-hunting (empty when unset).
	Instructions string
}

// LLMConfig reports the LLM action-generator config when the spec declared one
// (globalThis.generator.kind === "llm"). The second return is false for every
// other spec, so the runner falls back to the seeded picker.
func (v *Verifier) LLMConfig() (LLMConfig, bool) {
	generator := v.runtime.GlobalObject().Get("generator")
	if generator == nil || goja.IsUndefined(generator) || goja.IsNull(generator) {
		return LLMConfig{}, false
	}
	object := generator.ToObject(v.runtime)
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

// ActionCandidate is one selectable action the LLM generator may choose from,
// enumerated by collect-walking the spec's weighted actionsRoot (the same tree
// the seeded picker draws). Each candidate is a concrete, ready-to-execute
// action carrying a plainly-worded Description (numbered and echoed for
// strict-skip) plus its effective Weight so the model sees the spec's testing
// priorities.
type ActionCandidate struct {
	// Index is the candidate's 1-based position in the numbered list the model
	// picks a number from.
	Index int
	// Kind is the resulting action kind.
	Kind ActionKind
	// Description is the rendered action shown to the model and echoed back as
	// chosen_action, e.g. `Tap "Add credit"`. Dedup keys on it, so it is unique.
	Description string
	// Label is the visible-text target label (empty for gestures).
	Label string
	// Weight is the effective selection weight as a percentage (1..100),
	// meaningful only when Weighted is true (the tree used `weighted`).
	Weight   int
	Weighted bool
	// InputType hints a typing field's expected input (e.g. "number", or the
	// field's hint); empty when unknown or not a typing candidate.
	InputType string
	// Direction is up/down/left/right for gesture (Scroll) candidates, else "".
	Direction string
	// LLMText is true for builtin typing, where the model supplies the value;
	// false for authored InputText, whose sampled Action.Text is replayed as-is.
	LLMText bool
	// Action is the concrete action executed when this candidate is chosen. For
	// builtin typing it carries no text until the model's value is filled in.
	Action Action

	// prob is the internal accumulated selection probability, summed across
	// dedup, then rounded into Weight. Not exposed in the prompt directly.
	prob float64

	// The following are retained by the legacy AllCandidates enumeration.
	Verb          string
	X, Y          int
	Width, Height int
	Selector      string
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

// maxLabelRunes caps a visible-text label so joined descendant text stays short
// enough to render on one numbered line.
const maxLabelRunes = 40

// gestureDirections are the directional scrolls emitted per scrollable
// container. Vertical only: most mobile lists scroll up/down, and keeping the
// set tiny is the whole point of folding per-element swipes away.
var gestureDirections = []string{"down", "up"}

// Candidates enumerates every action the spec's weighted actionsRoot yields at
// the current step, each tagged with a plainly-worded description and its
// effective weight, for the LLM generator to pick one number from. It walks the
// SAME tree the seeded picker draws: weighted branches recurse (accumulating the
// selection probability), authored actions()/whenRoute leaves are called once
// for their concrete actions, and builtin verbs enumerate per applicable
// element. Disabled controls are dropped, per-element gestures fold into a few
// directional scrolls over scrollable containers, and identical descriptions
// dedup (summing weight).
func (v *Verifier) Candidates() []ActionCandidate {
	if v.lastTree == nil {
		return nil
	}
	root := v.runtime.GlobalObject().Get("actions")
	if root == nil || goja.IsUndefined(root) || goja.IsNull(root) {
		return nil
	}
	nodeIndex := buildNodeIndex(v.lastTree)
	var raw []ActionCandidate
	v.collectNode(root, 1.0, false, nodeIndex, &raw)
	return finalizeCandidates(raw)
}

// collectNode dispatches one GeneratorNode of the action tree. prob is the
// accumulated probability the seeded picker reaches this node; weighted records
// whether any weighted node lies on the path (so weights are shown only when the
// spec actually declared them).
func (v *Verifier) collectNode(node goja.Value, prob float64, weighted bool, nodeIndex map[*hierarchy.Element]*hierarchy.Node, out *[]ActionCandidate) {
	object := node.ToObject(v.runtime)
	if object == nil {
		return
	}
	kind := object.Get("kind")
	if kind == nil || goja.IsUndefined(kind) {
		return
	}
	switch kind.String() {
	case "weighted":
		v.collectWeighted(object, prob, nodeIndex, out)
	case "actions":
		v.collectActions(object, prob, weighted, nodeIndex, out)
	case "builtin":
		verb := object.Get("verb")
		if verb != nil && !goja.IsUndefined(verb) {
			v.collectBuiltin(verb.String(), prob, weighted, nodeIndex, out)
		}
	case "llm":
		// The llm marker is the generator, not part of the candidate tree.
	}
}

// collectWeighted recurses each branch, splitting the incoming probability by
// the branch weight over the sibling total (matching the seeded picker's single
// weighted draw).
func (v *Verifier) collectWeighted(object *goja.Object, prob float64, nodeIndex map[*hierarchy.Element]*hierarchy.Node, out *[]ActionCandidate) {
	branches := object.Get("branches")
	if branches == nil {
		return
	}
	array := branches.ToObject(v.runtime)
	if array == nil {
		return
	}
	length := int(array.Get("length").ToInteger())
	weights := make([]float64, length)
	children := make([]goja.Value, length)
	total := 0.0
	for i := range length {
		entry := array.Get(strconv.Itoa(i))
		pair := entry.ToObject(v.runtime)
		if pair == nil {
			continue
		}
		weight := pair.Get("0").ToFloat()
		if weight < 0 || math.IsNaN(weight) {
			weight = 0
		}
		weights[i] = weight
		children[i] = pair.Get("1")
		total += weight
	}
	if total <= 0 {
		return
	}
	for i := range length {
		if children[i] == nil {
			continue
		}
		v.collectNode(children[i], prob*weights[i]/total, true, nodeIndex, out)
	}
}

// collectActions calls an authored leaf's generator once (safe: it reads state
// and, off-route, returns []), turning each concrete descriptor into a
// candidate. It runs OUTSIDE the picker's rng scope, so from(...).generate()
// draws nothing and no seed advances.
func (v *Verifier) collectActions(object *goja.Object, prob float64, weighted bool, nodeIndex map[*hierarchy.Element]*hierarchy.Node, out *[]ActionCandidate) {
	generate, ok := goja.AssertFunction(object.Get("generate"))
	if !ok {
		return
	}
	result, err := generate(goja.Undefined())
	if err != nil {
		return
	}
	array := result.ToObject(v.runtime)
	if array == nil {
		return
	}
	length := int(array.Get("length").ToInteger())
	for i := range length {
		candidate, ok := v.candidateFromDescriptor(array.Get(strconv.Itoa(i)), nodeIndex)
		if !ok {
			continue
		}
		candidate.prob = prob
		candidate.Weighted = weighted
		*out = append(*out, candidate)
	}
}

// candidateFromDescriptor lowers one authored ActionDescriptor (as a goja
// object) into a ready-to-run candidate, resolving the target's coordinates,
// selector, and visible-text label. Actions on a disabled control are dropped.
func (v *Verifier) candidateFromDescriptor(value goja.Value, nodeIndex map[*hierarchy.Element]*hierarchy.Node) (ActionCandidate, bool) {
	object := value.ToObject(v.runtime)
	if object == nil {
		return ActionCandidate{}, false
	}
	kindValue := object.Get("kind")
	if kindValue == nil || goja.IsUndefined(kindValue) {
		return ActionCandidate{}, false
	}
	kind := ActionKind(kindValue.String())
	switch kind {
	case ActionKindTap, ActionKindDoubleTap, ActionKindLongPress:
		target := v.resolveTarget(object.Get("on"), nodeIndex)
		if target.disabled {
			return ActionCandidate{}, false
		}
		return ActionCandidate{
			Kind:   kind,
			Label:  target.label,
			Action: Action{Kind: kind, On: target.selector, X: target.x, Y: target.y},
		}, true
	case ActionKindInputText:
		target := v.resolveTarget(object.Get("into"), nodeIndex)
		if target.disabled {
			return ActionCandidate{}, false
		}
		text := stringField(object, "text")
		return ActionCandidate{
			Kind:      kind,
			Label:     target.label,
			InputType: target.inputType,
			Action:    Action{Kind: kind, On: target.selector, X: target.x, Y: target.y, Text: text},
		}, true
	case ActionKindScroll:
		target := v.resolveTarget(object.Get("in"), nodeIndex)
		direction := stringField(object, "direction")
		if direction == "" {
			direction = "down"
		}
		return ActionCandidate{
			Kind:      kind,
			Direction: direction,
			Action:    Action{Kind: kind, On: target.selector, Direction: direction},
		}, true
	case ActionKindPressKey:
		return ActionCandidate{
			Kind:   kind,
			Action: Action{Kind: kind, Key: stringField(object, "key")},
		}, true
	case ActionKindWait:
		return ActionCandidate{Kind: kind, Action: Action{Kind: kind}}, true
	default:
		return ActionCandidate{}, false
	}
}

// resolvedTarget is the geometry, selector, label, and input hint a target
// (ax element, selector string, or bare point) resolves to.
type resolvedTarget struct {
	x, y      int
	selector  string
	label     string
	inputType string
	disabled  bool
}

// resolveTarget reads an authored action's target. Ax element handles carry
// x/y/__sanderlingSelector plus their own text; a bare selector string resolves
// against the current tree; a point carries geometry only.
func (v *Verifier) resolveTarget(value goja.Value, nodeIndex map[*hierarchy.Element]*hierarchy.Node) resolvedTarget {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return resolvedTarget{}
	}
	if selector, ok := value.Export().(string); ok {
		return v.targetFromSelector(selector, nodeIndex)
	}
	object := value.ToObject(v.runtime)
	if object == nil {
		return resolvedTarget{}
	}
	selector := stringField(object, tagSelector)
	if selector == "" {
		selector = stringField(object, "selector")
	}
	target := resolvedTarget{
		x:        int(object.Get("x").ToInteger()),
		y:        int(object.Get("y").ToInteger()),
		selector: selector,
	}
	if element := v.findBySelector(selector); element != nil {
		target.label = visibleLabel(element, nodeIndex)
		target.inputType = inputTypeHint(element)
		target.disabled = !element.Enabled && hasEnabled(element)
	}
	if target.label == "" {
		target.label = truncateLabel(stringField(object, "text"))
	}
	return target
}

// targetFromSelector resolves a bare selector-string target against the tree.
func (v *Verifier) targetFromSelector(selector string, nodeIndex map[*hierarchy.Element]*hierarchy.Node) resolvedTarget {
	target := resolvedTarget{selector: selector}
	element := v.findBySelector(selector)
	if element == nil {
		return target
	}
	target.x, target.y = element.Bounds.Center()
	target.label = visibleLabel(element, nodeIndex)
	target.inputType = inputTypeHint(element)
	target.disabled = !element.Enabled && hasEnabled(element)
	return target
}

func (v *Verifier) findBySelector(selector string) *hierarchy.Element {
	if selector == "" || v.lastTree == nil {
		return nil
	}
	return v.lastTree.Find(selector)
}

// collectBuiltin enumerates a builtin verb over the current tree: tap-family and
// typing emit one candidate per applicable element; scrolls/swipes fold into
// directional gestures over scrollable containers.
func (v *Verifier) collectBuiltin(verb string, prob float64, weighted bool, nodeIndex map[*hierarchy.Element]*hierarchy.Node, out *[]ActionCandidate) {
	switch verb {
	case "taps", "doubleTaps", "longPresses":
		kind := verbActionKind(verb)
		for _, element := range v.elementsForVerb(verb) {
			x, y := element.Bounds.Center()
			*out = append(*out, ActionCandidate{
				Kind:     kind,
				Label:    visibleLabel(element, nodeIndex),
				Action:   Action{Kind: kind, On: selectorForElement(v.lastTree, element), X: x, Y: y},
				prob:     prob,
				Weighted: weighted,
			})
		}
	case "typing":
		for _, element := range v.elementsForVerb(verb) {
			x, y := element.Bounds.Center()
			*out = append(*out, ActionCandidate{
				Kind:      ActionKindInputText,
				Label:     visibleLabel(element, nodeIndex),
				InputType: inputTypeHint(element),
				LLMText:   true,
				Action:    Action{Kind: ActionKindInputText, On: selectorForElement(v.lastTree, element), X: x, Y: y},
				prob:      prob,
				Weighted:  weighted,
			})
		}
	case "scrolls", "swipes":
		v.collectGestures(prob, weighted, out)
	}
}

// collectGestures emits directional scrolls scoped to each scrollable container,
// never per element and never element-labeled. Folding both scrolls and swipes
// here is what removes the flood of mislabeled `Swipe "X"` gestures.
func (v *Verifier) collectGestures(prob float64, weighted bool, out *[]ActionCandidate) {
	scope := v.scopedElements()
	for _, element := range v.lastTree.Elements {
		if !scope[element] {
			continue
		}
		if element.Attributes["scrollable"] != "true" {
			continue
		}
		if element.Bounds.Width() <= 0 || element.Bounds.Height() <= 0 {
			continue
		}
		selector := selectorForElement(v.lastTree, element)
		for _, direction := range gestureDirections {
			action := Action{Kind: ActionKindScroll, On: selector, Direction: direction}
			if selector == "" {
				action.FromX, action.FromY, action.ToX, action.ToY = scrollGeometry(element.Bounds, direction)
			}
			*out = append(*out, ActionCandidate{
				Kind:      ActionKindScroll,
				Direction: direction,
				Action:    action,
				prob:      prob,
				Weighted:  weighted,
			})
		}
	}
}

// scrollGeometry lowers a directional scroll to swipe endpoints over the given
// container bounds, matching the runner's own derivation, used only when the
// container has no resolving selector.
func scrollGeometry(bounds hierarchy.Bounds, direction string) (fromX, fromY, toX, toY int) {
	cx, cy := bounds.Center()
	fromX, fromY, toX, toY = cx, cy, cx, cy
	switch direction {
	case "down":
		toY = cy - 4*bounds.Height()/10
	case "up":
		toY = cy + 4*bounds.Height()/10
	case "left":
		toX = cx + 4*bounds.Width()/10
	case "right":
		toX = cx - 4*bounds.Width()/10
	}
	return fromX, fromY, max(0, toX), max(0, toY)
}

// elementsForVerb returns the in-scope elements a builtin verb applies to, in
// tree order (the seeded picker's enumeration order), reusing verbAccepts.
func (v *Verifier) elementsForVerb(verb string) []*hierarchy.Element {
	scope := v.scopedElements()
	var elements []*hierarchy.Element
	for _, element := range v.lastTree.Elements {
		if scope[element] && verbAccepts(verb, element) {
			elements = append(elements, element)
		}
	}
	return elements
}

// finalizeCandidates renders each candidate's description, dedups identical
// descriptions (summing weight), numbers the survivors 1..N, and rounds the
// accumulated probability into a percentage Weight.
func finalizeCandidates(raw []ActionCandidate) []ActionCandidate {
	seen := make(map[string]int, len(raw))
	result := make([]ActionCandidate, 0, len(raw))
	for _, candidate := range raw {
		candidate.Description = describeCandidate(candidate)
		if index, ok := seen[candidate.Description]; ok {
			result[index].prob += candidate.prob
			result[index].Weighted = result[index].Weighted || candidate.Weighted
			continue
		}
		seen[candidate.Description] = len(result)
		result = append(result, candidate)
	}
	for i := range result {
		result[i].Index = i + 1
		if result[i].Weighted {
			result[i].Weight = max(1, int(math.Round(result[i].prob*100)))
		}
	}
	return result
}

// describeCandidate renders the plain, echo-friendly description shown in the
// numbered list. It is the dedup key, so it must be stable and unique per
// distinct action.
func describeCandidate(candidate ActionCandidate) string {
	switch candidate.Kind {
	case ActionKindTap:
		return fmt.Sprintf("Tap %q", candidate.Label)
	case ActionKindDoubleTap:
		return fmt.Sprintf("Double-tap %q", candidate.Label)
	case ActionKindLongPress:
		return fmt.Sprintf("Long-press %q", candidate.Label)
	case ActionKindInputText:
		if candidate.LLMText {
			if candidate.InputType != "" {
				return fmt.Sprintf("Type into %q (%s)", candidate.Label, candidate.InputType)
			}
			return fmt.Sprintf("Type into %q", candidate.Label)
		}
		return fmt.Sprintf("Type %q into %q", candidate.Action.Text, candidate.Label)
	case ActionKindScroll:
		return "Scroll " + candidate.Direction
	case ActionKindPressKey:
		return "Press " + candidate.Action.Key
	case ActionKindWait:
		return "Wait"
	default:
		return string(candidate.Kind)
	}
}

// buildNodeIndex maps each Element pointer to its Node so descendant text can be
// borrowed for a control whose own text is empty.
func buildNodeIndex(tree *hierarchy.Tree) map[*hierarchy.Element]*hierarchy.Node {
	index := make(map[*hierarchy.Element]*hierarchy.Node)
	if tree == nil || tree.Root == nil {
		return index
	}
	var walk func(node *hierarchy.Node)
	walk = func(node *hierarchy.Node) {
		index[&node.Element] = node
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(tree.Root)
	return index
}

// visibleLabel names a control by what a user would read: its own text, then
// description, then a field hint, then text borrowed from its descendants (the
// case that fixes empty-text Compose buttons whose word lives on a child), then
// its class as a last resort.
func visibleLabel(element *hierarchy.Element, nodeIndex map[*hierarchy.Element]*hierarchy.Node) string {
	if element.Text != "" {
		return truncateLabel(element.Text)
	}
	if element.Description != "" {
		return truncateLabel(element.Description)
	}
	if hint := element.Attributes["hintText"]; hint != "" {
		return truncateLabel(hint)
	}
	if node := nodeIndex[element]; node != nil {
		if text := descendantText(node); text != "" {
			return truncateLabel(text)
		}
	}
	if element.Class != "" {
		return element.Class
	}
	if element.ResourceID != "" {
		return element.ResourceID
	}
	return "control"
}

// descendantText joins the visible text of a node's descendants in tree order,
// so a clickable wrapper borrows the label of the Text child it contains.
func descendantText(node *hierarchy.Node) string {
	var parts []string
	var walk func(node *hierarchy.Node)
	walk = func(node *hierarchy.Node) {
		for _, child := range node.Children {
			switch {
			case child.Element.Text != "":
				parts = append(parts, child.Element.Text)
			case child.Element.Description != "":
				parts = append(parts, child.Element.Description)
			}
			walk(child)
		}
	}
	walk(node)
	return strings.Join(parts, " ")
}

// truncateLabel trims and shortens a label to one line's worth of runes.
func truncateLabel(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	runes := []rune(text)
	if len(runes) <= maxLabelRunes {
		return text
	}
	return strings.TrimSpace(string(runes[:maxLabelRunes])) + "…"
}

// inputTypeHint reports a typing field's expected input as a short word the
// model can use to synthesize a value, or "" when nothing distinguishes it.
func inputTypeHint(element *hierarchy.Element) string {
	haystack := strings.ToLower(element.Class + " " +
		element.Attributes["inputType"] + " " + element.Attributes["hintText"])
	switch {
	case strings.Contains(haystack, "number") || strings.Contains(haystack, "amount") || strings.Contains(haystack, "numeric"):
		return "number"
	case strings.Contains(haystack, "email"):
		return "email"
	case strings.Contains(haystack, "password"):
		return "password"
	case strings.Contains(haystack, "phone"):
		return "phone"
	default:
		return ""
	}
}

// hasEnabled reports whether the source tree carried an explicit enabled flag
// for the element, so a missing flag is not mistaken for "disabled".
func hasEnabled(element *hierarchy.Element) bool {
	_, ok := element.Attributes["enabled"]
	return ok
}

// stringField reads a string property off a goja object, returning "" when
// absent, null, or undefined.
func stringField(object *goja.Object, key string) string {
	value := object.Get(key)
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return ""
	}
	return value.String()
}
