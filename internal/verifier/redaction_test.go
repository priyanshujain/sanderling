package verifier

import (
	"strings"
	"testing"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// typedCredential stands in for what a login setup types. Nothing rendered for
// a record may carry it.
const typedCredential = "fixture-passphrase-9f21"

// secureLoginTreeJSON is the shape iOS and web produce for a login form: both
// report `secure` on every editable field, so a field carrying `secure:false`
// is positively known not to be a credential entry.
const secureLoginTreeJSON = `{
  "attributes": {"bounds": "[0,0,390,844]", "class": "Window"},
  "children": [
    {"attributes": {"identifier": "LoginEmail", "class": "TextField", "hintText": "Email", "bounds": "[20,200,370,244]"},
     "editable": true, "enabled": true, "secure": false, "children": []},
    {"attributes": {"identifier": "LoginPassword", "class": "SecureTextField", "hintText": "Password", "bounds": "[20,260,370,304]"},
     "editable": true, "enabled": true, "secure": true, "children": []}
  ]
}`

// androidLoginTreeJSON is the same form as Android reports it. The native tree
// mapper drops uiautomator's password attribute, so neither field carries the
// fact and the two are indistinguishable.
const androidLoginTreeJSON = `{
  "attributes": {"bounds": "[0,0,1080,2340]"},
  "children": [
    {"attributes": {"resource-id": "login_email", "class": "android.widget.EditText", "hintText": "Email", "bounds": "[20,200,1060,300]"},
     "enabled": true, "children": []},
    {"attributes": {"resource-id": "login_password", "class": "android.widget.EditText", "hintText": "Password", "bounds": "[20,320,1060,420]"},
     "enabled": true, "children": []}
  ]
}`

func authoredTypingCandidates(t *testing.T, selector, text, treeJSON string) []ActionCandidate {
	t.Helper()
	actions := `{kind:'actions', generate: () => [{kind:'InputText', into:'` + selector +
		`', text:'` + text + `'}]}`
	return mustCandidates(t, enumVerifier(t, actions, treeJSON), LabelSourceVisibleText)
}

func TestCandidatesRedactTextTypedIntoASecureField(t *testing.T) {
	candidates := authoredTypingCandidates(t, "id:LoginPassword", typedCredential, secureLoginTreeJSON)
	if len(candidates) != 1 {
		t.Fatalf("candidates = %v, want the one authored typing action", descriptions(candidates))
	}
	candidate := candidates[0]
	if !strings.Contains(candidate.Description, RedactedInputText) {
		t.Errorf("candidate description = %q, want the typed value redacted", candidate.Description)
	}
	if !strings.Contains(candidate.Description, "Password") {
		t.Errorf("candidate description = %q, want it to still name the field typed into", candidate.Description)
	}
	if candidate.Action.Text != typedCredential {
		t.Errorf("candidate action text = %q, want the real value so the driver still types it", candidate.Action.Text)
	}
}

func TestCandidatesRedactEveryTypedValueWhereTheTargetCannotReportSecure(t *testing.T) {
	for _, selector := range []string{"id:login_email", "id:login_password"} {
		candidates := authoredTypingCandidates(t, selector, typedCredential, androidLoginTreeJSON)
		if len(candidates) != 1 {
			t.Fatalf("%s: candidates = %v, want the one authored typing action", selector, descriptions(candidates))
		}
		if strings.Contains(candidates[0].Description, typedCredential) {
			t.Errorf("%s: candidate description carries the typed value: %q", selector, candidates[0].Description)
		}
	}
}

func TestCandidatesKeepTextTypedIntoAFieldReportedNotSecure(t *testing.T) {
	const address = "ada@example.com"
	candidates := authoredTypingCandidates(t, "id:LoginEmail", address, secureLoginTreeJSON)
	if len(candidates) != 1 {
		t.Fatalf("candidates = %v, want the one authored typing action", descriptions(candidates))
	}
	if !strings.Contains(candidates[0].Description, address) {
		t.Errorf("candidate description = %q, want the typed value on a field reported not secure", candidates[0].Description)
	}
}

// A spec that types into an element HANDLE is the shape every login setup has
// (examples/folio/sanderling/spec.ts). The handle carries the goja host's own
// `secure` field, which is a plain boolean and reads false on a platform that
// reports nothing, so a rule trusting it would leave every Android value in the
// clear. The tree the handle names is the only carrier of the three-way fact.
func TestCandidatesRedactTextTypedIntoAnAndroidElementHandle(t *testing.T) {
	v := newVerifier(t)
	loadActionSpec(t, v, `
		import { InputText, actions, extract } from "@sanderling/spec";
		const field = extract((s) => s.ax.find({ "resource-id": "login_password" })).named("field");
		globalThis.actions = actions(() =>
			field.current ? [InputText({ into: field.current, text: "`+typedCredential+`" })] : []);
	`)
	pushTree(t, v, androidLoginTreeJSON)

	candidates := mustCandidates(t, v, LabelSourceVisibleText)
	if len(candidates) != 1 {
		t.Fatalf("candidates = %v, want the one authored typing action", descriptions(candidates))
	}
	if strings.Contains(candidates[0].Description, typedCredential) {
		t.Errorf("candidate description carries the typed value: %q", candidates[0].Description)
	}
	if candidates[0].Action.Text != typedCredential {
		t.Errorf("candidate action text = %q, want the real value so the driver still types it",
			candidates[0].Action.Text)
	}
}

// lastActionOnBothHosts reports what a spec reading state.lastAction sees on
// each host for an action the runner has recorded: the goja host stringifies
// its own object, the web host receives EncodeLastAction's JSON and installs
// the parsed value in the page.
func lastActionOnBothHosts(t *testing.T, action Action, treeJSON string) (string, string) {
	t.Helper()
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatalf("parse tree: %v", err)
	}
	recorded := RecordedAction(action, tree)
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.last = __sanderling__.extract(state => JSON.stringify(state.lastAction));
	`)
	if err := verifier.PushSnapshot(SnapshotInput{
		Snapshots:  Snapshots{},
		Tree:       tree,
		LastAction: &recorded,
	}); err != nil {
		t.Fatalf("PushSnapshot: %v", err)
	}
	handle := verifier.runtime.GlobalObject().Get("last").ToObject(verifier.runtime)
	return handle.Get("current").String(), string(EncodeLastAction(&recorded))
}

func typedInto(selector, text string) Action {
	return Action{Kind: ActionKindInputText, On: selector, Text: text}
}

// A spec extracting state.lastAction (examples/folio/sanderling/spec.ts) writes
// what it reads into the trace, so state.lastAction is a record like the other
// three and carries the same rule on both hosts.
func TestStateLastActionRedactsATypedValueTheTargetCannotClear(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		treeJSON string
		selector string
	}{
		{"secure field", secureLoginTreeJSON, "id:LoginPassword"},
		{"android field reported as neither", androidLoginTreeJSON, "id:login_email"},
		{"android password field", androidLoginTreeJSON, "id:login_password"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			goja, web := lastActionOnBothHosts(
				t, typedInto(testCase.selector, typedCredential), testCase.treeJSON)
			for _, host := range []struct{ name, reported string }{
				{"goja", goja},
				{"web", web},
			} {
				if strings.Contains(host.reported, typedCredential) {
					t.Errorf("the %s host publishes the typed value in state.lastAction: %s",
						host.name, host.reported)
				}
				if !strings.Contains(host.reported, RedactedInputText) {
					t.Errorf("the %s host reports state.lastAction as %s, want the typed value "+
						"redacted in place", host.name, host.reported)
				}
			}
			if goja != web {
				t.Errorf("the two hosts disagree on state.lastAction\n goja: %s\n  web: %s", goja, web)
			}
		})
	}
}

func TestStateLastActionKeepsATypedValueForAFieldReportedNotSecure(t *testing.T) {
	const address = "ada@example.com"
	goja, web := lastActionOnBothHosts(t, typedInto("id:LoginEmail", address), secureLoginTreeJSON)
	for _, host := range []struct{ name, reported string }{
		{"goja", goja},
		{"web", web},
	} {
		if !strings.Contains(host.reported, address) {
			t.Errorf("the %s host reports state.lastAction as %s, want the typed value on a "+
				"field reported not secure", host.name, host.reported)
		}
	}
	if goja != web {
		t.Errorf("the two hosts disagree on state.lastAction\n goja: %s\n  web: %s", goja, web)
	}
}
