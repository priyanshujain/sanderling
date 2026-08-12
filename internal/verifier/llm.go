package verifier

import (
	"encoding/json"
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

// SnapshotStep returns the step index of the most recent PushSnapshot. It lags
// the runner's current step whenever an observation was skipped (a transitional
// tree), which is exactly when Screenshot returns an older step's image.
func (v *Verifier) SnapshotStep() int {
	return v.stepIndex
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
	// chosen_action, e.g. `Tap "Add credit"`. Two entries may render the same
	// when the screen holds two controls a user reads alike; Index is what tells
	// them apart, and it is what the model picks by.
	Description string
	// Label is the target label the selected LabelSource named (empty for
	// gestures).
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
}

// maxLabelRunes caps a visible-text label so joined descendant text stays short
// enough to render on one numbered line.
const maxLabelRunes = 40

// gestureDurationMillis is how long a drag takes when the descriptor did not say.
// It mirrors DEFAULT_SWIPE_DURATION in runtime-entry.ts, which is what the
// seeded policy's action carries by the time it reaches the runner: the two
// policies must hand the driver the same gesture, not two speeds of it.
const gestureDurationMillis = 250

// The label sources a candidate's target can be named by. This is the
// observation channel the model reads, and nothing else: the seeded picker
// selects by index and never asks for a label, so the two seeded cells of a
// labelling factorial draw the identical stream.
const (
	// LabelSourceVisibleText names a control by what a user would read. It is
	// the default, and the channel every run so far was produced with.
	LabelSourceVisibleText = "visible-text"
	// LabelSourceResourceID names a control by the identifier the app assigned
	// it, which no user ever sees.
	LabelSourceResourceID = "resource-id"
)

// Candidates enumerates every action the spec's weighted actionsRoot yields at
// the current step, each tagged with a plainly-worded description and its
// effective weight, for the LLM generator to pick one number from. It walks the
// SAME tree the seeded picker draws: weighted branches recurse (accumulating the
// selection probability), authored actions()/whenRoute leaves are called once
// for their concrete actions, and builtin verbs come straight from the picker's
// own enumeration. Candidates that would execute the same action dedup, summing
// weight; two controls that merely read alike stay two entries.
//
// labelSource selects the channel each target is named by. An unrecognized
// value (including the zero value) names targets by visible text; the CLI
// rejects an unknown mode before a run starts, so only a test reaches that.
//
// The error is the spec refusing to be run by this policy at all: an authored
// leaf that samples one of several items reaches the seeded picker's rng but
// never this walk, so the model would be offered a fixed first item forever. It
// names the leaf and it is fatal, because degrading to that fixed item silently
// is what makes a policy comparison meaningless.
func (v *Verifier) Candidates(labelSource string) ([]ActionCandidate, error) {
	if v.lastTree == nil {
		return nil, nil
	}
	root := v.runtime.GlobalObject().Get("actions")
	if root == nil || goja.IsUndefined(root) || goja.IsNull(root) {
		return nil, nil
	}
	labels := labelContext{nodeIndex: buildNodeIndex(v.lastTree), source: labelSource}
	var raw []ActionCandidate
	v.setEnumeratingCandidates(true)
	defer v.setEnumeratingCandidates(false)
	if err := v.collectNode(root, 1.0, false, labels, &raw); err != nil {
		return nil, err
	}
	return finalizeCandidates(raw), nil
}

// setEnumeratingCandidates tells the spec bundle that the authored leaves are
// being called by this policy rather than by the picker. A spec loaded without
// the runtime entry (a raw-JS unit fixture) has no such callable, and no
// sampler to refuse either.
func (v *Verifier) setEnumeratingCandidates(enumerating bool) {
	if v.setEnumeratingCandidatesFn == nil {
		return
	}
	_, _ = v.setEnumeratingCandidatesFn(goja.Undefined(), v.runtime.ToValue(enumerating))
}

// collectNode dispatches one GeneratorNode of the action tree. prob is the
// accumulated probability the seeded picker reaches this node; weighted records
// whether any weighted node lies on the path (so weights are shown only when the
// spec actually declared them).
func (v *Verifier) collectNode(node goja.Value, prob float64, weighted bool, labels labelContext, out *[]ActionCandidate) error {
	object := node.ToObject(v.runtime)
	if object == nil {
		return nil
	}
	kind := object.Get("kind")
	if kind == nil || goja.IsUndefined(kind) {
		return nil
	}
	switch kind.String() {
	case "weighted":
		return v.collectWeighted(object, prob, labels, out)
	case "actions":
		return v.collectActions(object, prob, weighted, labels, out)
	case "builtin":
		verb := object.Get("verb")
		if verb != nil && !goja.IsUndefined(verb) {
			v.collectBuiltin(verb.String(), prob, weighted, labels, out)
		}
	case "llm":
		// The llm marker is the generator, not part of the candidate tree.
	}
	return nil
}

// collectWeighted recurses each branch, splitting the incoming probability by
// the branch weight over the sibling total (matching the seeded picker's single
// weighted draw).
func (v *Verifier) collectWeighted(object *goja.Object, prob float64, labels labelContext, out *[]ActionCandidate) error {
	branches := object.Get("branches")
	if branches == nil {
		return nil
	}
	array := branches.ToObject(v.runtime)
	if array == nil {
		return nil
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
		return nil
	}
	for i := range length {
		if children[i] == nil {
			continue
		}
		// The branch number is the author's own path to a refused leaf, which
		// its closure source alone does not give when the leaf is a whenRoute
		// (whose closure belongs to the library, not the spec).
		if err := v.collectNode(children[i], prob*weights[i]/total, true, labels, out); err != nil {
			return fmt.Errorf("branch %d: %w", i+1, err)
		}
	}
	return nil
}

// collectActions calls an authored leaf's generator once (safe: it reads state
// and, off-route, returns []), turning each concrete descriptor into a
// candidate. It runs OUTSIDE the picker's rng scope, so from(...).generate()
// draws nothing and no seed advances.
//
// A generator that throws for its own reasons still contributes nothing and
// nothing more: this walk calls EVERY leaf every step, including leaves the
// seeded picker would have walked once in a hundred steps, so promoting those
// throws would kill runs the seeded arm survives.
func (v *Verifier) collectActions(object *goja.Object, prob float64, weighted bool, labels labelContext, out *[]ActionCandidate) error {
	generatorValue := object.Get("generate")
	generate, ok := goja.AssertFunction(generatorValue)
	if !ok {
		return nil
	}
	result, err := generate(goja.Undefined())
	if err != nil {
		if refusal, refused := v.samplerRefusal(err); refused {
			return fmt.Errorf("authored action %s %s", authoredLeafIdentity(generatorValue), refusal)
		}
		return nil
	}
	array := result.ToObject(v.runtime)
	if array == nil {
		return nil
	}
	length := int(array.Get("length").ToInteger())
	for i := range length {
		candidate, ok := v.candidateFromDescriptor(array.Get(strconv.Itoa(i)), labels)
		if !ok {
			continue
		}
		candidate.prob = prob
		candidate.Weighted = weighted
		*out = append(*out, candidate)
	}
	return nil
}

// samplerRefusalName is the error name pkg/spec/src/sampler-rng.ts stamps on the
// refusal it throws, which is what tells that refusal apart from a spec's own
// runtime errors.
const samplerRefusalName = "SanderlingSamplerRefusal"

// samplerRefusal reports the refusal message when the authored leaf declined to
// sample for this policy.
func (v *Verifier) samplerRefusal(err error) (string, bool) {
	var exception *goja.Exception
	if !errors.As(err, &exception) {
		return "", false
	}
	value := exception.Value()
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return "", false
	}
	thrown := value.ToObject(v.runtime)
	if thrown == nil || stringField(thrown, "name") != samplerRefusalName {
		return "", false
	}
	return stringField(thrown, "message"), true
}

// maxLeafSourceRunes caps the generator excerpt that names a leaf in an error.
const maxLeafSourceRunes = 160

// authoredLeafIdentity renders the leaf's generator source on one line. An
// authored leaf is an anonymous closure among identical-looking tree nodes, so
// its source is the handle an author can search the spec for.
func authoredLeafIdentity(generator goja.Value) string {
	source := []rune(strings.Join(strings.Fields(generator.String()), " "))
	if len(source) > maxLeafSourceRunes {
		return strconv.Quote(string(source[:maxLeafSourceRunes]) + "...")
	}
	return strconv.Quote(string(source))
}

// candidateFromDescriptor lowers one authored ActionDescriptor (as a goja
// object) into a ready-to-run candidate, resolving the target's coordinates,
// selector, and label.
//
// A disabled target is offered like any other. The seeded picker executes
// whatever the leaf authored, disabled or not, and attempting a disabled
// control is where boundary defects live: a control the app forgot to re-enable
// reads as disabled, and a policy that cannot attempt it cannot find that.
func (v *Verifier) candidateFromDescriptor(value goja.Value, labels labelContext) (ActionCandidate, bool) {
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
		target, ok := v.resolveTarget(object.Get("on"), labels)
		if !ok {
			return ActionCandidate{}, false
		}
		return ActionCandidate{
			Kind:   kind,
			Label:  target.label,
			Action: Action{Kind: kind, On: target.selector, X: target.x, Y: target.y},
		}, true
	case ActionKindInputText:
		target, ok := v.resolveTarget(object.Get("into"), labels)
		if !ok {
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
		container, _ := v.resolveTarget(object.Get("in"), labels)
		direction := stringField(object, "direction")
		if direction == "" {
			direction = "down"
		}
		action := Action{
			Kind:           kind,
			On:             container.selector,
			Direction:      direction,
			DurationMillis: gestureDurationMillis,
		}
		// Endpoints only when the descriptor computed the whole gesture (the
		// builtin generator does). Anchoring an authored scroll on the
		// container's own point instead would hand the runner a drag from a
		// point to itself, which it executes as written.
		from, hasFrom := v.resolveTarget(object.Get("from"), labels)
		to, hasTo := v.resolveTarget(object.Get("to"), labels)
		if hasFrom && hasTo {
			action.FromX, action.FromY = from.x, from.y
			action.ToX, action.ToY = to.x, to.y
		}
		return ActionCandidate{Kind: kind, Direction: direction, Action: action}, true
	case ActionKindSwipe:
		from, hasFrom := v.resolveTarget(object.Get("from"), labels)
		to, hasTo := v.resolveTarget(object.Get("to"), labels)
		if !hasFrom || !hasTo {
			return ActionCandidate{}, false
		}
		return ActionCandidate{
			Kind: kind,
			Action: Action{
				Kind:  kind,
				FromX: from.x, FromY: from.y,
				ToX: to.x, ToY: to.y,
				DurationMillis: intFieldOr(object, "durationMillis", gestureDurationMillis),
			},
		}, true
	case ActionKindPressKey:
		return ActionCandidate{
			Kind:   kind,
			Action: Action{Kind: kind, Key: stringField(object, "key")},
		}, true
	case ActionKindWait:
		return ActionCandidate{
			Kind:   kind,
			Action: Action{Kind: kind, DurationMillis: intField(object, "durationMillis")},
		}, true
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
}

// resolveTarget reads an authored action's target. Ax element handles carry
// x/y/__sanderlingSelector plus their own text and id; a bare selector string
// resolves against the current tree; a point carries geometry only.
//
// The second return is false when the value names no target the seeded policy
// could act on either: runtime-entry.ts pointOf accepts a non-empty selector
// string or an object with numeric coordinates, and drops the whole action
// otherwise. Lowering one of those to (0, 0) instead would offer the model an
// action the seeded policy never takes, aimed at the screen corner.
func (v *Verifier) resolveTarget(value goja.Value, labels labelContext) (resolvedTarget, bool) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return resolvedTarget{}, false
	}
	if selector, ok := value.Export().(string); ok {
		if selector == "" {
			return resolvedTarget{}, false
		}
		return v.targetFromSelector(selector, labels), true
	}
	object := value.ToObject(v.runtime)
	if object == nil {
		return resolvedTarget{}, false
	}
	x, hasX := numberField(object, "x")
	y, hasY := numberField(object, "y")
	if !hasX || !hasY {
		return resolvedTarget{}, false
	}
	selector := stringField(object, tagSelector)
	if selector == "" {
		selector = stringField(object, "selector")
	}
	target := resolvedTarget{x: x, y: y, selector: selector}
	if element := v.findBySelector(selector); element != nil {
		target.label = labels.label(element)
		target.inputType = inputTypeHint(element)
	}
	if target.label == "" {
		target.label = truncateLabel(stringField(object, labels.handleField()))
	}
	return target, true
}

// targetFromSelector resolves a bare selector-string target against the tree.
func (v *Verifier) targetFromSelector(selector string, labels labelContext) resolvedTarget {
	target := resolvedTarget{selector: selector}
	element := v.findBySelector(selector)
	if element == nil {
		return target
	}
	target.x, target.y = element.Bounds.Center()
	target.label = labels.label(element)
	target.inputType = inputTypeHint(element)
	return target
}

func (v *Verifier) findBySelector(selector string) *hierarchy.Element {
	if selector == "" || v.lastTree == nil {
		return nil
	}
	return v.lastTree.Find(selector)
}

// collectBuiltin turns the picker's own enumeration of a builtin verb into
// candidates. The list comes from the bundle's __sanderlingEnumerateBuiltin__
// (pick.ts builtinCandidates), which is what the seeded policy draws from, so
// the two policies select over one action space and cannot drift apart. Each
// entry's action arrives on the wire contract DecodeAction already reads, so a
// chosen candidate executes the action the seeded draw would have executed.
func (v *Verifier) collectBuiltin(verb string, prob float64, weighted bool, labels labelContext, out *[]ActionCandidate) {
	entries, err := v.enumerateBuiltin(verb)
	if err != nil {
		return
	}
	targets := v.targets()
	for _, entry := range entries {
		candidate := ActionCandidate{
			Kind:      entry.action.Kind,
			Direction: entry.action.Direction,
			// Builtin typing enumerates the field, not the value: the seeded
			// policy draws its text from the corpus and the model writes its own.
			LLMText:  entry.action.Kind == ActionKindInputText,
			Action:   entry.action,
			prob:     prob,
			Weighted: weighted,
		}
		if entry.targetIndex >= 0 && entry.targetIndex < len(targets) {
			element := targets[entry.targetIndex].element
			candidate.Label = labels.label(element)
			candidate.InputType = inputTypeHint(element)
		}
		*out = append(*out, candidate)
	}
}

// builtinCandidate is one entry of the shared builtin enumeration: the concrete
// action, plus the index of the host candidate it targets (-1 when the verb has
// no target, as for a key press or a wait).
type builtinCandidate struct {
	action      Action
	targetIndex int
}

// enumerateBuiltin invokes the bundle's shared enumeration for one verb. A spec
// loaded without the runtime entry (a raw-JS unit fixture) has no enumerator, so
// the verb contributes nothing rather than falling back to a second enumeration.
func (v *Verifier) enumerateBuiltin(verb string) ([]builtinCandidate, error) {
	if v.enumerateBuiltinFn == nil {
		return nil, errors.New("verifier: builtin enumeration not available")
	}
	value, err := v.enumerateBuiltinFn(goja.Undefined(), v.runtime.ToValue(verb))
	if err != nil {
		return nil, fmt.Errorf("enumerate %s: %w", verb, err)
	}
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return nil, nil
	}
	raw, err := json.Marshal(value.Export())
	if err != nil {
		return nil, fmt.Errorf("marshal %s enumeration: %w", verb, err)
	}
	var wire []struct {
		Action      json.RawMessage `json:"action"`
		TargetIndex int             `json:"targetIndex"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("decode %s enumeration: %w", verb, err)
	}
	entries := make([]builtinCandidate, 0, len(wire))
	for _, item := range wire {
		action, err := DecodeAction(item.Action)
		if err != nil {
			continue
		}
		entries = append(entries, builtinCandidate{action: action, targetIndex: item.TargetIndex})
	}
	return entries, nil
}

// candidateIdentity is what a candidate would DO. Two candidates sharing it are
// the same action reached through two paths of the action tree, so folding them
// into one numbered entry loses nothing; two that differ are different actions
// however alike they read, so folding them would put one of them out of reach.
// llmText is part of it because it decides where the typed text comes from: the
// model writes it for a builtin typing candidate, while an authored one replays
// the value already sitting in Action.Text.
type candidateIdentity struct {
	action  Action
	llmText bool
}

// finalizeCandidates renders each candidate's description, dedups by what the
// candidate executes (summing weight), numbers the survivors 1..N, and rounds
// the accumulated probability into a percentage Weight.
func finalizeCandidates(raw []ActionCandidate) []ActionCandidate {
	seen := make(map[candidateIdentity]int, len(raw))
	result := make([]ActionCandidate, 0, len(raw))
	for _, candidate := range raw {
		candidate.Description = describeCandidate(candidate)
		identity := candidateIdentity{action: candidate.Action, llmText: candidate.LLMText}
		if index, ok := seen[identity]; ok {
			result[index].prob += candidate.prob
			result[index].Weighted = result[index].Weighted || candidate.Weighted
			continue
		}
		seen[identity] = len(result)
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
// numbered list. It is display only: dedup keys on the action, so two entries
// may read alike without merging and Index is what separates them. Do not add an
// ordinal or a coordinate to pull those apart: this string IS the observation
// channel a labelling experiment varies, so a disambiguator here would name a
// target through a channel the label source deliberately withholds.
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
	case ActionKindSwipe:
		// A swipe is a drag across the screen, so where it runs is the whole of
		// what it does and a reader needs the endpoints to picture it. The label
		// is prepended when the origin element has one, because "swipe that row"
		// is the interaction a model reaches for and a bare pair of points does
		// not say which row.
		where := fmt.Sprintf("from (%d,%d) to (%d,%d)",
			candidate.Action.FromX, candidate.Action.FromY,
			candidate.Action.ToX, candidate.Action.ToY)
		if candidate.Label == "" {
			return "Swipe " + where
		}
		return fmt.Sprintf("Swipe %q %s", candidate.Label, where)
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

// labelContext carries what naming a candidate's target takes: the node index
// descendant text is borrowed through, and the channel the name comes from.
type labelContext struct {
	nodeIndex map[*hierarchy.Element]*hierarchy.Node
	source    string
}

func (l labelContext) label(element *hierarchy.Element) string {
	if l.source == LabelSourceResourceID {
		return resourceIdentifierLabel(element)
	}
	return visibleLabel(element, l.nodeIndex)
}

// handleField is the ax-element handle field a label falls back to when the
// target's selector no longer resolves against the current tree. Reading the
// handle's text there would leak visible text into an identifier-labelled run,
// which is the one thing that arm must not see.
func (l labelContext) handleField() string {
	if l.source == LabelSourceResourceID {
		return "id"
	}
	return "text"
}

// resourceIdentifierLabel names a control by the identifier the app assigned it,
// then by its class, then by a bare word. Every rung a user could read (text,
// description, hint, descendant text) is deliberately absent: the point of this
// channel is that the model sees no visible text at all, so a fallback that
// reached for text would silently turn the arm back into the default one.
func resourceIdentifierLabel(element *hierarchy.Element) string {
	if element.ResourceID != "" {
		return truncateLabel(element.ResourceID)
	}
	if element.Class != "" {
		return element.Class
	}
	return "control"
}

// visibleLabel names a control by what a user would read: its own text, then
// description, then a field hint, then text borrowed from its descendants (the
// case that fixes empty-text Compose buttons whose word lives on a child), then
// its class as a last resort.
func visibleLabel(element *hierarchy.Element, nodeIndex map[*hierarchy.Element]*hierarchy.Node) string {
	// An editable field's own text is the transient typed value ("1"); its hint
	// names its purpose ("Amount") and stays stable, so prefer the hint there.
	if element.Editable {
		if hint := element.Attributes["hintText"]; hint != "" {
			return truncateLabel(hint)
		}
	}
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

// stringField reads a string property off a goja object, returning "" when
// absent, null, or undefined.
func stringField(object *goja.Object, key string) string {
	value := object.Get(key)
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return ""
	}
	return value.String()
}

// intField reads a numeric property off a goja object, returning 0 when absent,
// null, or undefined.
func intField(object *goja.Object, key string) int {
	value := object.Get(key)
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return 0
	}
	return int(value.ToInteger())
}

// intFieldOr reads a numeric property, falling back when the descriptor left it
// out. It mirrors the serializer's `??`, so an explicit zero is kept.
func intFieldOr(object *goja.Object, key string, fallback int) int {
	value := object.Get(key)
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return fallback
	}
	return int(value.ToInteger())
}

// numberField reads a property that must actually BE a number, which is what
// tells a target carrying no coordinates apart from one anchored at (0, 0).
func numberField(object *goja.Object, key string) (int, bool) {
	value := object.Get(key)
	if value == nil {
		return 0, false
	}
	switch number := value.Export().(type) {
	case int64:
		return int(number), true
	case float64:
		if math.IsNaN(number) {
			return 0, false
		}
		return int(number), true
	default:
		return 0, false
	}
}
