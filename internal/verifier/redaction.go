package verifier

import (
	"github.com/dop251/goja"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// RedactedInputText stands in for a typed value in every record. It is fixed,
// so it gives away neither the value nor its length.
const RedactedInputText = "[redacted]"

// secureFact is what a target says about being a secure text entry. `reported`
// separates "the platform says this is not one" from "the platform says
// nothing", which are the two cases the redaction rule has to tell apart.
type secureFact struct {
	reported bool
	secure   bool
}

// secureFactFromHandle reads an element handle's own report. The web host
// injects handles built in the page, which carry no selector to resolve against
// the goja-side tree, so the handle is the only thing that knows.
func secureFactFromHandle(object *goja.Object) secureFact {
	value := object.Get("secure")
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return secureFact{}
	}
	return secureFact{reported: true, secure: value.ToBoolean()}
}

func secureFactOf(element *hierarchy.Element) secureFact {
	if element == nil {
		return secureFact{}
	}
	return secureFact{reported: element.SecureReported(), secure: element.Secure}
}

// RecordedActionText renders the typed value of an action for anything that is
// persisted or sent, resolving the action's target in the tree it was chosen
// against.
func RecordedActionText(action Action, tree *hierarchy.Tree) string {
	if action.Kind != ActionKindInputText {
		return action.Text
	}
	var target *hierarchy.Element
	if tree != nil && action.On != "" {
		target = tree.Find(action.On)
	}
	return recordedInputText(action.Text, secureFactOf(target))
}

// RecordedAction is the action as everything downstream of the dispatch sees
// it. The runner reports the previous step's action to the spec as
// state.lastAction, and a spec extracting it (examples/folio/sanderling/spec.ts)
// writes it to the trace, so the copy the runner keeps carries the recorded
// text rather than the typed one.
//
// The decision is made here rather than where the two hosts render
// state.lastAction because only the caller holds the tree that can answer it.
// The hosts hold the NEXT step's tree, where the selector may name a different
// element (a revealed password field reports secure:false) or none at all, and
// cmd/internal-tools/oracle-reduction replays state.lastAction from the trace's
// already-recorded text, which redacting here matches exactly.
func RecordedAction(action Action, tree *hierarchy.Tree) Action {
	action.Text = RecordedActionText(action, tree)
	return action
}

// recordedInputText is the one place a typed value is rendered for a record.
// The prompt's recent-action memory, the numbered candidate list, the trace and
// the action the runner reports back as state.lastAction all go through it, so
// a fifth record added later cannot publish a value the other four withhold.
// The driver dispatch reads Action.Text directly and is the only reader of the
// real value, which is what keeps the app receiving the keystrokes a user would
// have produced.
//
// A target the platform reports as a secure entry is redacted, and so is a
// target carrying no report at all. iOS and web state the fact on every
// editable element, so a missing one means Android, whose native tree mapper
// drops uiautomator's password attribute before the sidecar sees it. There a
// password field cannot be told from a search box, and the target that cannot
// be told apart is treated as the credential.
func recordedInputText(text string, target secureFact) string {
	if target.reported && !target.secure {
		return text
	}
	return RedactedInputText
}
