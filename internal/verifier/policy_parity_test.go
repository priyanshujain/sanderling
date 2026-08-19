package verifier

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
)

// policyTreeJSON gives every builtin verb something to act on, with every label
// distinct so no two candidates render the same way.
const policyTreeJSON = `{
  "attributes": {"bounds": "[0,0,400,800]"},
  "children": [
    {"attributes": {"resource-id": "Save", "text": "Save", "bounds": "[0,0,200,60]"}, "clickable": true, "enabled": true, "children": []},
    {"attributes": {"resource-id": "Cancel", "text": "Cancel", "bounds": "[200,0,400,60]"}, "clickable": true, "enabled": true, "children": []},
    {"attributes": {"resource-id": "Amount", "class": "EditText", "hintText": "Amount", "bounds": "[0,100,400,160]"}, "enabled": true, "children": []},
    {"attributes": {"resource-id": "Note", "class": "EditText", "hintText": "Note", "bounds": "[0,200,400,260]"}, "enabled": true, "children": []},
    {"attributes": {"resource-id": "List", "scrollable": "true", "bounds": "[0,300,400,700]"}, "children": []}
  ]
}`

// policyVerbs is every builtin verb a spec can put in its action tree.
var policyVerbs = []string{
	"taps", "doubleTaps", "longPresses", "typing", "scrolls", "swipes", "pressKeys", "waitOnce",
}

// seededDrawBudget is how many times the seeded picker is driven per verb. The
// candidate sets here are a handful of entries wide, so this exhausts them many
// times over; a miss would mean the picker cannot reach one of its own
// candidates, which is itself the bug worth failing on.
const seededDrawBudget = 300

// TestPoliciesEnumerateTheSameActions is the guard on the claim the paper makes
// about the two action policies: they differ in the pick and in nothing else.
// For every builtin verb, the actions the seeded picker can draw and the
// candidates the model policy is offered must be the same set. Enumeration lives
// in one place (pick.ts builtinCandidates) precisely so this cannot drift, and
// this test is what notices if a second one ever grows back.
func TestPoliciesEnumerateTheSameActions(t *testing.T) {
	for _, verb := range policyVerbs {
		t.Run(verb, func(t *testing.T) {
			seeded := seededReachableActions(t, verb)
			model := modelOfferedActions(t, verb)
			if len(model) == 0 {
				t.Fatalf("%s offered the model no candidates at all", verb)
			}
			if !slices.Equal(slices.Sorted(maps.Keys(seeded)), slices.Sorted(maps.Keys(model))) {
				t.Errorf("action spaces differ for %s\n seeded=%v\n  model=%v",
					verb, slices.Sorted(maps.Keys(seeded)), slices.Sorted(maps.Keys(model)))
			}
		})
	}
}

// TestModelCandidateDescriptionsAreUniqueAndNamed checks the rendering half of
// the contract: every enumerated action reaches the model as its own distinctly
// named line, so the number it picks and the action that executes agree.
func TestModelCandidateDescriptionsAreUniqueAndNamed(t *testing.T) {
	for _, verb := range policyVerbs {
		t.Run(verb, func(t *testing.T) {
			verifier := loadVerbSpec(t, verb)
			seen := map[string]bool{}
			for _, candidate := range mustCandidates(t, verifier, LabelSourceVisibleText) {
				if candidate.Description == "" {
					t.Fatalf("%s produced a candidate with no description: %+v", verb, candidate.Action)
				}
				if seen[candidate.Description] {
					t.Errorf("%s rendered %q twice, so the model cannot address both",
						verb, candidate.Description)
				}
				seen[candidate.Description] = true
			}
		})
	}
}

// TestModelIsOfferedTheUntargetedVerbs pins the two verbs the model arm used to
// be blind to: with no element to enumerate over, a key press and a wait were
// dropped, so the model could never navigate back or let the app settle.
func TestModelIsOfferedTheUntargetedVerbs(t *testing.T) {
	verifier := loadVerbSpec(t, "pressKeys")
	if !hasCandidate(mustCandidates(t, verifier, LabelSourceVisibleText), "Press back") {
		t.Errorf("pressKeys missing from the model's candidates: %v",
			descriptions(mustCandidates(t, verifier, LabelSourceVisibleText)))
	}
	verifier = loadVerbSpec(t, "waitOnce")
	if !hasCandidate(mustCandidates(t, verifier, LabelSourceVisibleText), "Wait") {
		t.Errorf("waitOnce missing from the model's candidates: %v",
			descriptions(mustCandidates(t, verifier, LabelSourceVisibleText)))
	}
}

// TestSeededDrawStreamIgnoresLabelSource is the manipulation check the
// labelling factorial needs: the seeded picker selects by index and never asks
// for a label, so its draw stream must be bit-identical whichever channel the
// model policy would have been given, and identical again to a run where the
// candidate list was never enumerated at all. Any difference between two seeded
// cells is then the application and the harness, not the factor.
func TestSeededDrawStreamIgnoresLabelSource(t *testing.T) {
	for _, verb := range policyVerbs {
		t.Run(verb, func(t *testing.T) {
			never := seededDrawStream(t, verb, "")
			text := seededDrawStream(t, verb, LabelSourceVisibleText)
			identifier := seededDrawStream(t, verb, LabelSourceResourceID)
			if !slices.Equal(never, text) {
				t.Errorf("enumerating visible-text candidates moved the seeded stream for %s", verb)
			}
			if !slices.Equal(never, identifier) {
				t.Errorf("enumerating identifier candidates moved the seeded stream for %s", verb)
			}
		})
	}
}

// seededDrawStream drives the seeded picker for the draw budget and returns
// every action in order. An empty labelSource enumerates nothing; otherwise the
// model's candidate list is built under that channel before each draw, which is
// the only way the two could ever touch.
func seededDrawStream(t *testing.T, verb, labelSource string) []string {
	t.Helper()
	verifier := loadVerbSpec(t, verb)
	stream := make([]string, 0, seededDrawBudget)
	for range seededDrawBudget {
		if labelSource != "" {
			mustCandidates(t, verifier, labelSource)
		}
		action, err := verifier.NextAction()
		if errors.Is(err, ErrNoAction) {
			stream = append(stream, "no action")
			continue
		}
		if err != nil {
			t.Fatalf("%s next action: %v", verb, err)
		}
		stream = append(stream, fmt.Sprintf("%+v", action))
	}
	return stream
}

// loadVerbSpec builds a verifier whose whole action tree is one builtin verb,
// with policyTreeJSON pushed as the current state.
func loadVerbSpec(t *testing.T, verb string) *Verifier {
	t.Helper()
	verifier := newVerifier(t, WithSeed(0x5eed))
	loadActionSpec(t, verifier, fmt.Sprintf(
		"import { %s } from \"@sanderling/spec\";\nglobalThis.actions = %s;", verb, verb))
	pushTree(t, verifier, policyTreeJSON)
	return verifier
}

// seededReachableActions drives the seeded picker over its draw budget and
// returns every distinct action it produced.
func seededReachableActions(t *testing.T, verb string) map[string]Action {
	t.Helper()
	verifier := loadVerbSpec(t, verb)
	reachable := map[string]Action{}
	for range seededDrawBudget {
		action, err := verifier.NextAction()
		if errors.Is(err, ErrNoAction) {
			continue
		}
		if err != nil {
			t.Fatalf("%s next action: %v", verb, err)
		}
		reachable[actionIdentity(action)] = action
	}
	return reachable
}

// modelOfferedActions returns the actions behind the numbered list the model
// policy picks from.
func modelOfferedActions(t *testing.T, verb string) map[string]Action {
	t.Helper()
	verifier := loadVerbSpec(t, verb)
	offered := map[string]Action{}
	for _, candidate := range mustCandidates(t, verifier, LabelSourceVisibleText) {
		offered[actionIdentity(candidate.Action)] = candidate.Action
	}
	return offered
}

// actionIdentity keys an action by everything except the values the policy owns
// rather than the candidate set: the typed text, which the seeded arm draws from
// the edge-case corpus and the model writes itself, a swipe's drag distance,
// which the seeded arm draws and the enumeration lists at a nominal length, and
// the source, which names who produced an action rather than what it does (the
// enumeration names nobody, because the model has not chosen yet).
// Comparing those would compare policies instead of action spaces. A swipe's
// direction is NOT policy-owned, so it survives as the sign of the drag.
func actionIdentity(action Action) string {
	action.Text = ""
	action.Source = ""
	if action.Kind == ActionKindSwipe {
		action.ToX = sign(action.ToX - action.FromX)
		action.ToY = sign(action.ToY - action.FromY)
	}
	return fmt.Sprintf("%+v", action)
}

func sign(value int) int {
	switch {
	case value > 0:
		return 1
	case value < 0:
		return -1
	default:
		return 0
	}
}

// samplerParitySpec authors one leaf that taps a target drawn from three: the
// pattern the model policy refuses and the seeded picker exists to draw.
const samplerParitySpec = `
import { actions, from, Tap } from "@sanderling/spec";
const targets = from(["id:Save", "id:Cancel", "id:Amount"]);
globalThis.actions = actions(() => [Tap({ on: targets.generate() })]);
`

// TestSeededSamplingSurvivesTheModelPolicysRefusal keeps the refusal on the one
// policy it belongs to. Sampling inside the picker's rng scope is correct, so
// the seeded stream must be identical whether or not the model policy tried and
// failed to enumerate the same leaf first, and it must still reach every item.
func TestSeededSamplingSurvivesTheModelPolicysRefusal(t *testing.T) {
	alone := seededSamplerStream(t, samplerParitySpec, false)
	afterRefusal := seededSamplerStream(t, samplerParitySpec, true)
	if !slices.Equal(alone, afterRefusal) {
		t.Error("a refused enumeration moved the seeded draw stream")
	}
	drawn := map[string]bool{}
	for _, action := range alone {
		drawn[action] = true
	}
	if len(drawn) != 3 {
		t.Errorf("seeded picker reached %d of the 3 sampled targets: %v", len(drawn), slices.Sorted(maps.Keys(drawn)))
	}
}

// seededSamplerStream drives the seeded picker over the draw budget, optionally
// letting the model policy refuse the same spec before every draw.
func seededSamplerStream(t *testing.T, specSource string, enumerateFirst bool) []string {
	t.Helper()
	verifier := newVerifier(t, WithSeed(0x5eed))
	loadActionSpec(t, verifier, specSource)
	pushTree(t, verifier, policyTreeJSON)
	stream := make([]string, 0, seededDrawBudget)
	for range seededDrawBudget {
		if enumerateFirst {
			if _, err := verifier.Candidates(LabelSourceVisibleText); err == nil {
				t.Fatal("the model policy must refuse a spec that samples")
			}
		}
		action, err := verifier.NextAction()
		if err != nil {
			t.Fatalf("seeded picker declined the sampled tap: %v", err)
		}
		stream = append(stream, fmt.Sprintf("%+v", action))
	}
	return stream
}

// valueGeneratorSpec authors one leaf that types a drawn value, which is the
// same divergence from() has: the draw reaches the seeded picker's rng and never
// this enumeration, so the model would be handed one fixed value forever.
func valueGeneratorSpec(generator string) string {
	return fmt.Sprintf(`
import { actions, InputText, integers, strings, emails, edgeCaseText } from "@sanderling/spec";
const authoredValues = %s;
globalThis.actions = actions(() => [InputText({ into: "id:Amount", text: String(authoredValues.generate()) })]);
`, generator)
}

// TestModelPolicyRefusesAnAuthoredValueGenerator covers every generator in
// values.ts whose span is wider than one value.
func TestModelPolicyRefusesAnAuthoredValueGenerator(t *testing.T) {
	for _, generator := range []struct{ name, expression string }{
		{"integers", "integers().between(1, 500)"},
		{"strings", "strings().length(3, 6).alpha()"},
		{"emails", `emails().domain("folio.app")`},
		{"edgeCaseText", "edgeCaseText()"},
	} {
		t.Run(generator.name, func(t *testing.T) {
			verifier := newVerifier(t, WithSeed(0x5eed))
			loadActionSpec(t, verifier, valueGeneratorSpec(generator.expression))
			pushTree(t, verifier, policyTreeJSON)

			_, err := verifier.Candidates(LabelSourceVisibleText)
			if err == nil {
				t.Fatalf("%s was enumerated for the model policy, which cannot draw it", generator.name)
			}
			if !strings.Contains(err.Error(), "authoredValues.generate()") {
				t.Errorf("error does not name the offending leaf: %v", err)
			}
			if !strings.Contains(err.Error(), generator.name+"()") {
				t.Errorf("error does not name %s(): %v", generator.name, err)
			}
		})
	}
}

// TestModelPolicyAcceptsASingleValuedGenerator is the boundary: a generator that
// spans one value hands both policies the same value, so refusing it would stop
// runs that have nothing wrong with them.
func TestModelPolicyAcceptsASingleValuedGenerator(t *testing.T) {
	verifier := newVerifier(t, WithSeed(0x5eed))
	loadActionSpec(t, verifier, valueGeneratorSpec("integers().between(7, 7)"))
	pushTree(t, verifier, policyTreeJSON)

	candidates := mustCandidates(t, verifier, LabelSourceVisibleText)
	if len(candidates) != 1 || candidates[0].Action.Text != "7" {
		t.Fatalf("model was offered %+v, want the one authored InputText typing 7", candidates)
	}
}

// TestSeededValueDrawsSurviveTheModelPolicysRefusal is the values.ts half of the
// guard above: the seeded arm keeps its whole range, and its draw stream does not
// move because the model policy refused the same spec first.
func TestSeededValueDrawsSurviveTheModelPolicysRefusal(t *testing.T) {
	spec := valueGeneratorSpec("integers().between(1, 500)")
	alone := seededSamplerStream(t, spec, false)
	afterRefusal := seededSamplerStream(t, spec, true)
	if !slices.Equal(alone, afterRefusal) {
		t.Error("a refused enumeration moved the seeded draw stream")
	}
	drawn := map[string]bool{}
	for _, action := range alone {
		drawn[action] = true
	}
	if len(drawn) < 100 {
		t.Errorf("seeded picker typed %d distinct values over %d draws", len(drawn), seededDrawBudget)
	}
}
