package verifier

import (
	"strings"
	"testing"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// enumTreeJSON exercises every labeling path: a clickable wrapper whose own text
// is empty but whose child Text reads "Add credit" (descendant borrowing), an
// editable field labeled by its hint, a text-labeled button, a DISABLED button,
// and a scrollable list (the only valid scroll origin).
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

// labelChannelTreeJSON pulls the two label channels apart: every identifier
// differs from the text a user reads, one control has a class but no identifier,
// one has neither, and one has an identifier but nothing readable at all.
const labelChannelTreeJSON = `{
  "attributes": {"bounds": "[0,0,1080,2400]"},
  "children": [
    {"attributes": {"resource-id": "add_credit_button", "class": "android.widget.Button", "bounds": "[0,100,1080,200]"}, "clickable": true, "enabled": true, "children": [
      {"attributes": {"text": "Add credit", "bounds": "[0,100,540,200]"}, "children": []}
    ]},
    {"attributes": {"class": "android.widget.CheckBox", "text": "Remember me", "bounds": "[0,250,1080,300]"}, "clickable": true, "enabled": true, "children": []},
    {"attributes": {"class": "android.widget.CheckBox", "text": "Stay signed in", "bounds": "[0,300,1080,350]"}, "clickable": true, "enabled": true, "children": []},
    {"attributes": {"text": "Sign in", "bounds": "[0,400,1080,450]"}, "clickable": true, "enabled": true, "children": []},
    {"attributes": {"resource-id": "amount_field", "class": "EditText", "hintText": "Amount", "bounds": "[0,500,1080,600]"}, "enabled": true, "children": []},
    {"attributes": {"resource-id": "silent_row", "bounds": "[0,650,1080,700]"}, "clickable": true, "enabled": true, "children": []}
  ]
}`

// sharedLabelTreeJSON is a list where two rows read exactly the same to a user
// ("Delete") while the app tells them apart by identifier. It is the shape a
// list of removable items has in any real app.
const sharedLabelTreeJSON = `{
  "attributes": {"bounds": "[0,0,1080,2400]"},
  "children": [
    {"attributes": {"resource-id": "delete_alpha", "text": "Delete", "bounds": "[0,100,1080,200]"}, "clickable": true, "enabled": true, "children": []},
    {"attributes": {"resource-id": "delete_beta", "text": "Delete", "bounds": "[0,200,1080,300]"}, "clickable": true, "enabled": true, "children": []},
    {"attributes": {"resource-id": "checkout", "text": "Checkout", "bounds": "[0,400,1080,500]"}, "clickable": true, "enabled": true, "children": []}
  ]
}`

// enumVerifier loads a spec whose actions root is the given plain-object graph
// and stages the given tree, so Candidates walks a controlled action tree. The
// spec is bundled with the goja runtime entry because the model arm reads the
// picker's own builtin enumeration out of that bundle.
func enumVerifier(t *testing.T, actionsJS, treeJSON string) *Verifier {
	t.Helper()
	v := newVerifier(t)
	loadActionSpec(t, v, "globalThis.actions = "+actionsJS+";")
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatalf("parse tree: %v", err)
	}
	v.lastTree = tree
	return v
}

// mustCandidates enumerates the model policy's list, failing the test on the
// refusal an authored multi-item sampler raises.
func mustCandidates(t *testing.T, v *Verifier, labelSource string) []ActionCandidate {
	t.Helper()
	candidates, err := v.Candidates(labelSource)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	return candidates
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
	candidates := mustCandidates(t, v, LabelSourceVisibleText)

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

func TestCandidatesLabelsControlsByResourceIdentifier(t *testing.T) {
	candidates := mustCandidates(t, enumVerifier(t, "{kind:'builtin', verb:'taps'}", labelChannelTreeJSON), LabelSourceResourceID)

	if !hasCandidate(candidates, `Tap "add_credit_button"`) {
		t.Errorf("want the control named by its identifier, got %v", descriptions(candidates))
	}
	// Nothing a user could read may reach this channel, including through a
	// fallback rung: an arm that sees the text is the other arm.
	for _, readable := range []string{`Tap "Add credit"`, `Tap "Remember me"`, `Tap "Sign in"`} {
		if hasCandidate(candidates, readable) {
			t.Errorf("visible text leaked in as %s: %v", readable, descriptions(candidates))
		}
	}
}

func TestCandidatesIdentifierChannelFallsBackToClassThenBareControl(t *testing.T) {
	candidates := mustCandidates(t, enumVerifier(t, "{kind:'builtin', verb:'taps'}", labelChannelTreeJSON), LabelSourceResourceID)

	if !hasCandidate(candidates, `Tap "android.widget.CheckBox"`) {
		t.Errorf("a control with no identifier falls back to its class, got %v", descriptions(candidates))
	}
	if !hasCandidate(candidates, `Tap "control"`) {
		t.Errorf("a control with neither identifier nor class falls back to a bare word, got %v",
			descriptions(candidates))
	}
}

// TestCandidatesIdentifierChannelKeepsControlsItCannotNameApartReachable is the
// cost of the channel, bounded: two identifier-less controls of one class read
// the same in the numbered list, but they stay TWO entries, each carrying its
// own action, so the model can act on either by number. A channel that renames
// controls must never shrink the action space.
func TestCandidatesIdentifierChannelKeepsControlsItCannotNameApartReachable(t *testing.T) {
	text := mustCandidates(t, enumVerifier(t, "{kind:'builtin', verb:'taps'}", labelChannelTreeJSON), LabelSourceVisibleText)
	identifier := mustCandidates(t, enumVerifier(t, "{kind:'builtin', verb:'taps'}", labelChannelTreeJSON), LabelSourceResourceID)

	if count(text, `Tap "Remember me"`) != 1 || count(text, `Tap "Stay signed in"`) != 1 {
		t.Fatalf("the text channel should address both checkboxes, got %v", descriptions(text))
	}
	checkboxes := candidatesMatching(identifier, `Tap "android.widget.CheckBox"`)
	if len(checkboxes) != 2 {
		t.Fatalf("the two checkboxes should be two entries, got %d: %v",
			len(checkboxes), descriptions(identifier))
	}
	if checkboxes[0].Action == checkboxes[1].Action {
		t.Errorf("both entries execute the same action: %+v", checkboxes[0].Action)
	}
	if len(identifier) != len(text) {
		t.Errorf("identifier list (%d) and text list (%d) must offer the same actions: %v vs %v",
			len(identifier), len(text), descriptions(identifier), descriptions(text))
	}
}

// TestCandidatesReachBothControlsSharingOneVisibleLabel is the reachability
// floor: two rows a user reads as the same word are two different controls, so
// both get a number and the second number taps the second row. Dedup that keyed
// on the rendered line dropped the second one, putting it out of reach of any
// prompt or policy.
func TestCandidatesReachBothControlsSharingOneVisibleLabel(t *testing.T) {
	candidates := mustCandidates(t, enumVerifier(t, "{kind:'builtin', verb:'taps'}", sharedLabelTreeJSON), LabelSourceVisibleText)

	deletes := candidatesMatching(candidates, `Tap "Delete"`)
	if len(deletes) != 2 {
		t.Fatalf("want both Delete rows reachable, got %d: %v", len(deletes), descriptions(candidates))
	}
	if got := deletes[0].Action.On; got != "id:delete_alpha" {
		t.Errorf("first entry targets %q, want id:delete_alpha", got)
	}
	if got := deletes[1].Action.On; got != "id:delete_beta" {
		t.Errorf("second entry targets %q, want id:delete_beta", got)
	}
	if deletes[0].Index == deletes[1].Index {
		t.Errorf("both entries share number %d, so the model cannot address them apart", deletes[0].Index)
	}
}

// TestCandidatesVisibleTextFallsBackToTheIdentifier is where the two channels
// agree: a control carrying nothing readable is named by its identifier in both,
// so a screen built entirely from such controls is one cell, not two.
func TestCandidatesVisibleTextFallsBackToTheIdentifier(t *testing.T) {
	candidates := mustCandidates(t, enumVerifier(t, "{kind:'builtin', verb:'taps'}", labelChannelTreeJSON), LabelSourceVisibleText)

	if !hasCandidate(candidates, `Tap "silent_row"`) {
		t.Errorf("a control with no readable text falls back to its identifier, got %v",
			descriptions(candidates))
	}
}

func TestCandidatesTypingLabelFollowsTheLabelSource(t *testing.T) {
	text := mustCandidates(t, enumVerifier(t, "{kind:'builtin', verb:'typing'}", labelChannelTreeJSON), LabelSourceVisibleText)
	if !hasCandidate(text, `Type into "Amount" (number)`) {
		t.Errorf("want the field named by its hint, got %v", descriptions(text))
	}

	identifier := mustCandidates(t, enumVerifier(t, "{kind:'builtin', verb:'typing'}", labelChannelTreeJSON), LabelSourceResourceID)
	if !hasCandidate(identifier, `Type into "amount_field" (number)`) {
		t.Errorf("want the field named by its identifier, got %v", descriptions(identifier))
	}
}

// TestLabelSourceChangesOnlyTheDescription is the claim the labelling factorial
// rests on: the channel renames every target and does nothing else. Both arms
// enumerate the same candidates, in the same order, at the same weights,
// carrying the same executable actions; the description and the label are the
// only things that move. Anything else and the two cells would be picking from
// different action spaces, so a difference in defect yield could not be
// attributed to how the controls were named.
func TestLabelSourceChangesOnlyTheDescription(t *testing.T) {
	const everyLabelledVerb = `{kind:'weighted', branches:[
      [1,{kind:'builtin',verb:'taps'}],
      [1,{kind:'builtin',verb:'typing'}],
      [1,{kind:'builtin',verb:'swipes'}]
    ]}`
	fixtures := []struct {
		name string
		tree string
	}{
		{"identifiers collide", labelChannelTreeJSON},
		{"visible text collides", sharedLabelTreeJSON},
	}
	withoutNames := func(candidate ActionCandidate) ActionCandidate {
		candidate.Description = ""
		candidate.Label = ""
		return candidate
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			text := mustCandidates(t, enumVerifier(t, everyLabelledVerb, fixture.tree), LabelSourceVisibleText)
			identifier := mustCandidates(t, enumVerifier(t, everyLabelledVerb, fixture.tree), LabelSourceResourceID)
			if len(text) == 0 {
				t.Fatal("fixture yielded no candidates")
			}
			if len(text) != len(identifier) {
				t.Fatalf("different action spaces: %d text candidates vs %d identifier ones:\n%v\n%v",
					len(text), len(identifier), descriptions(text), descriptions(identifier))
			}
			renamed := false
			for i := range text {
				if withoutNames(text[i]) != withoutNames(identifier[i]) {
					t.Errorf("candidate %d differs beyond its name:\n text=%+v\n   id=%+v",
						i+1, text[i], identifier[i])
				}
				if text[i].Description != identifier[i].Description {
					renamed = true
				}
			}
			if !renamed {
				t.Error("no candidate was renamed, so this fixture does not exercise the channel")
			}
		})
	}
}

func TestCandidatesDropsDisabledControlsFromBuiltinVerbs(t *testing.T) {
	v := enumVerifier(t, "{kind:'builtin', verb:'taps'}", enumTreeJSON)
	for _, candidate := range mustCandidates(t, v, LabelSourceVisibleText) {
		if strings.Contains(candidate.Description, "Off") {
			t.Errorf("disabled control surfaced as %q", candidate.Description)
		}
	}
}

func TestCandidatesTypingExposesInputType(t *testing.T) {
	v := enumVerifier(t, "{kind:'builtin', verb:'typing'}", enumTreeJSON)
	candidates := mustCandidates(t, v, LabelSourceVisibleText)
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
	candidates := mustCandidates(t, v, LabelSourceVisibleText)
	if hasCandidate(candidates, `Type into "99" (number)`) || hasCandidate(candidates, `Type into "99"`) {
		t.Errorf("editable field labeled by its typed value: %v", descriptions(candidates))
	}
	if !hasCandidate(candidates, `Type into "Amount" (number)`) {
		t.Errorf("want the field labeled by its hint, got %v", descriptions(candidates))
	}
}

func TestCandidatesKeepsGestureVerbsDistinct(t *testing.T) {
	v := enumVerifier(t,
		"{kind:'weighted', branches:[[1,{kind:'builtin',verb:'scrolls'}],[1,{kind:'builtin',verb:'swipes'}]]}",
		enumTreeJSON)
	candidates := mustCandidates(t, v, LabelSourceVisibleText)

	// `scrolls` folds to one directional pair over the single scrollable
	// container, which is what keeps the list short.
	if !hasCandidate(candidates, "Scroll down") || !hasCandidate(candidates, "Scroll up") {
		t.Errorf("want directional scrolls, got %v", descriptions(candidates))
	}
	if got := count(candidates, "Scroll down"); got != 1 {
		t.Errorf("Scroll down appears %d times, want 1 over the one container", got)
	}
	// `swipes` is a different verb, not a second name for the scroll: a
	// free-form drag from any element, named by the control it starts on. That
	// is what puts swipe-to-dismiss on a row within the model's reach.
	if !hasCandidatePrefix(candidates, `Swipe "Sign in"`) {
		t.Errorf("want a swipe naming the non-scrollable row, got %v", descriptions(candidates))
	}
	if hasCandidatePrefix(candidates, "Scroll \"") {
		t.Errorf("scroll candidates must stay container-scoped: %v", descriptions(candidates))
	}
}

func hasCandidatePrefix(candidates []ActionCandidate, prefix string) bool {
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate.Description, prefix) {
			return true
		}
	}
	return false
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
	candidates := mustCandidates(t, v, LabelSourceVisibleText)
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
	candidates := mustCandidates(t, v, LabelSourceVisibleText)
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
	for _, candidate := range mustCandidates(t, v, LabelSourceVisibleText) {
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
	candidates := mustCandidates(t, v, LabelSourceVisibleText)

	// Authored Tap resolves its selector to the visible-text label.
	if !hasCandidate(candidates, `Tap "Sign in"`) {
		t.Errorf("authored tap missing: %v", descriptions(candidates))
	}
	// A disabled authored target is offered, not dropped: the seeded picker
	// executes it, and attempting a disabled control is where boundary defects
	// live, so a policy that cannot attempt it cannot find them.
	if !hasCandidate(candidates, `Tap "Off"`) {
		t.Errorf("authored action on a disabled control was dropped: %v", descriptions(candidates))
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

func TestCandidatesSurfaceAuthoredUntargetedActions(t *testing.T) {
	// A spec that authors a swipe, a key press, or a wait must reach the model
	// with all three: the seeded picker executes whatever the leaf returns, so a
	// kind the enumeration drops is an action only one policy can take.
	actions := `{kind:'actions', generate: () => [
      {kind:'Swipe', from:{x:10,y:600}, to:{x:10,y:100}, durationMillis: 250},
      {kind:'PressKey', key:'back'},
      {kind:'Wait'}
    ]}`
	candidates := mustCandidates(t, enumVerifier(t, actions, enumTreeJSON), LabelSourceVisibleText)
	for _, want := range []string{"Swipe from (10,600) to (10,100)", "Press back", "Wait"} {
		if !hasCandidate(candidates, want) {
			t.Errorf("authored %q missing: %v", want, descriptions(candidates))
		}
	}
}

// TestCandidatesAuthoredWaitKeepsItsDuration: a Wait that loses its duration is
// a wait of zero, which the runner cannot dispatch at all, so the model would be
// idling on paper while the seeded arm really waits.
func TestCandidatesAuthoredWaitKeepsItsDuration(t *testing.T) {
	actions := `{kind:'actions', generate: () => [{kind:'Wait', durationMillis: 500}]}`
	candidates := mustCandidates(t, enumVerifier(t, actions, enumTreeJSON), LabelSourceVisibleText)
	candidate, ok := findCandidate(candidates, "Wait")
	if !ok {
		t.Fatalf("authored wait missing: %v", descriptions(candidates))
	}
	if candidate.Action.DurationMillis != 500 {
		t.Errorf("wait duration = %d, want the authored 500", candidate.Action.DurationMillis)
	}
}

// TestCandidatesAuthoredScrollNamesItsContainer: the container is the whole
// point of an authored scroll. Dropped, the runner re-derives the gesture from
// the screen and the scroll lands on whatever else is scrollable.
func TestCandidatesAuthoredScrollNamesItsContainer(t *testing.T) {
	actions := `{kind:'actions', generate: () => [{kind:'Scroll', direction:'down', in:'id:List'}]}`
	candidates := mustCandidates(t, enumVerifier(t, actions, enumTreeJSON), LabelSourceVisibleText)
	candidate, ok := findCandidate(candidates, "Scroll down")
	if !ok {
		t.Fatalf("authored scroll missing: %v", descriptions(candidates))
	}
	if candidate.Action.On != "id:List" {
		t.Errorf("scroll container = %q, want id:List", candidate.Action.On)
	}
	if candidate.Action.DurationMillis != gestureDurationMillis {
		t.Errorf("scroll duration = %d, want %d", candidate.Action.DurationMillis, gestureDurationMillis)
	}
}

// TestCandidatesAuthoredScrollKeepsPrecomputedEndpoints: a descriptor that
// already carries the gesture is executed as written rather than re-derived.
func TestCandidatesAuthoredScrollKeepsPrecomputedEndpoints(t *testing.T) {
	actions := `{kind:'actions', generate: () => [
      {kind:'Scroll', direction:'down', in:'id:List', from:{x:540,y:1400}, to:{x:540,y:920}}
    ]}`
	candidates := mustCandidates(t, enumVerifier(t, actions, enumTreeJSON), LabelSourceVisibleText)
	candidate, ok := findCandidate(candidates, "Scroll down")
	if !ok {
		t.Fatalf("authored scroll missing: %v", descriptions(candidates))
	}
	action := candidate.Action
	got := [4]int{action.FromX, action.FromY, action.ToX, action.ToY}
	if got != [4]int{540, 1400, 540, 920} {
		t.Errorf("scroll endpoints = %v, want the descriptor's (540,1400)->(540,920)", got)
	}
}

// TestCandidatesAuthoredSwipeDefaultsItsDuration keeps the gesture default in
// one place: the serializer the seeded arm goes through fills an omitted
// duration, and a candidate that left it at zero would depend on the runner
// happening to pick the same fallback.
func TestCandidatesAuthoredSwipeDefaultsItsDuration(t *testing.T) {
	actions := `{kind:'actions', generate: () => [
      {kind:'Swipe', from:{x:10,y:600}, to:{x:10,y:100}},
      {kind:'Swipe', from:{x:20,y:600}, to:{x:20,y:100}, durationMillis: 400}
    ]}`
	candidates := mustCandidates(t, enumVerifier(t, actions, enumTreeJSON), LabelSourceVisibleText)
	omitted, ok := findCandidate(candidates, "Swipe from (10,600) to (10,100)")
	if !ok {
		t.Fatalf("authored swipe missing: %v", descriptions(candidates))
	}
	if omitted.Action.DurationMillis != gestureDurationMillis {
		t.Errorf("omitted duration = %d, want %d", omitted.Action.DurationMillis, gestureDurationMillis)
	}
	authored, ok := findCandidate(candidates, "Swipe from (20,600) to (20,100)")
	if !ok {
		t.Fatalf("authored swipe missing: %v", descriptions(candidates))
	}
	if authored.Action.DurationMillis != 400 {
		t.Errorf("authored duration = %d, want 400", authored.Action.DurationMillis)
	}
}

// TestCandidatesDropTargetsThatResolveToNothing: the seeded picker drops an
// action whose target resolves to neither coordinates nor a selector. Offering
// it to the model instead would put a tap on the screen origin within reach,
// which on Android is the corner that pulls the notification shade down.
func TestCandidatesDropTargetsThatResolveToNothing(t *testing.T) {
	actions := `{kind:'actions', generate: () => [
      {kind:'Tap', on: null},
      {kind:'Tap', on: {}},
      {kind:'InputText', into: {}, text:'x'},
      {kind:'Swipe', from:{x:1,y:2}, to:{}}
    ]}`
	candidates := mustCandidates(t, enumVerifier(t, actions, enumTreeJSON), LabelSourceVisibleText)
	if len(candidates) != 0 {
		t.Errorf("targetless actions reached the model: %v", descriptions(candidates))
	}
}

// TestCandidatesKeepATargetOnTheScreenOrigin is the other side of that rule: a
// point at (0,0) IS a target the seeded picker executes, so the drop must key on
// a target with no coordinates rather than on coordinates that are zero.
func TestCandidatesKeepATargetOnTheScreenOrigin(t *testing.T) {
	actions := `{kind:'actions', generate: () => [{kind:'Tap', on: {x: 0, y: 0}}]}`
	candidates := mustCandidates(t, enumVerifier(t, actions, enumTreeJSON), LabelSourceVisibleText)
	if len(candidates) != 1 {
		t.Fatalf("want the origin tap kept, got %v", descriptions(candidates))
	}
}

func TestCandidatesOffRouteLeafYieldsNothing(t *testing.T) {
	v := enumVerifier(t, "{kind:'actions', generate: () => []}", enumTreeJSON)
	if got := mustCandidates(t, v, LabelSourceVisibleText); len(got) != 0 {
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
	if got := mustCandidates(t, v, LabelSourceVisibleText); len(got) != 0 {
		t.Errorf("cross-fade frame should yield no candidates, got %v", descriptions(got))
	}
	// The seeded policy is skipped by the SAME guard, in the shared producer,
	// so neither arm acts on a mid-animation layout.
	if got := v.targets(); len(got) != 0 {
		t.Errorf("cross-fade frame should yield no host targets, got %d", len(got))
	}
}

func TestCandidatesNilWithoutTreeOrActions(t *testing.T) {
	withActions := newLoadedVerifier(t, "globalThis.actions = {kind:'builtin', verb:'taps'};")
	if got := mustCandidates(t, withActions, LabelSourceVisibleText); got != nil {
		t.Errorf("Candidates with no tree = %v, want nil", got)
	}
	noActions := newLoadedVerifier(t, "globalThis.properties = {};")
	tree, _ := hierarchy.Parse(enumTreeJSON)
	noActions.lastTree = tree
	if got := mustCandidates(t, noActions, LabelSourceVisibleText); got != nil {
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

func candidatesMatching(candidates []ActionCandidate, description string) []ActionCandidate {
	var matched []ActionCandidate
	for _, candidate := range candidates {
		if candidate.Description == description {
			matched = append(matched, candidate)
		}
	}
	return matched
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

// samplerSpec authors one leaf that taps a target drawn from the given list.
func samplerSpec(items string) string {
	return `
import { actions, from, Tap } from "@sanderling/spec";
const targets = from(` + items + `);
globalThis.actions = actions(() => [Tap({ on: targets.generate() })]);
`
}

// TestCandidatesRefuseAMultiItemAuthoredSampler pins the refusal: a sampler
// reads the picker's rng, which this policy has no way to enter, so the draw
// would collapse to the first item on every step while the seeded picker keeps
// reaching all three. Offering that silently is what would make a comparison of
// the two policies meaningless, so the spec is refused instead.
func TestCandidatesRefuseAMultiItemAuthoredSampler(t *testing.T) {
	v := newVerifier(t)
	loadActionSpec(t, v, samplerSpec(`["id:SignIn", "id:Amount", "id:List"]`))
	pushTree(t, v, enumTreeJSON)

	_, err := v.Candidates(LabelSourceVisibleText)
	if err == nil {
		t.Fatal("a multi-item authored sampler must refuse to run under the model policy")
	}
	message := err.Error()
	if !strings.Contains(message, "targets.generate()") {
		t.Errorf("error does not name the offending leaf, so the author cannot find it: %s", message)
	}
	if !strings.Contains(message, "draws 1 of 3 sampled items") {
		t.Errorf("error does not say what the leaf did: %s", message)
	}
}

// TestCandidatesAcceptASingleItemAuthoredSampler: a one-item sampler short
// circuits before the rng, so both policies get that one value and there is no
// divergence to refuse.
func TestCandidatesAcceptASingleItemAuthoredSampler(t *testing.T) {
	v := newVerifier(t)
	loadActionSpec(t, v, samplerSpec(`["id:SignIn"]`))
	pushTree(t, v, enumTreeJSON)

	candidates, err := v.Candidates(LabelSourceVisibleText)
	if err != nil {
		t.Fatalf("a single-item sampler is not a divergence: %v", err)
	}
	if !hasCandidate(candidates, `Tap "Sign in"`) {
		t.Errorf("sampled tap missing: %v", descriptions(candidates))
	}
}
