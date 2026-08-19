package runner

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	mockdriver "github.com/priyanshujain/sanderling/internal/driver/mock"

	"github.com/priyanshujain/sanderling/internal/verifier"
)

// typedCredential stands in for what a login setup types. Nothing the run
// records or sends may carry it.
const typedCredential = "fixture-passphrase-9f21"

// iosLoginTreeJSON is the shape internal/driver/ioscompanion produces for a
// login form: every editable field reports `secure`, so a field carrying
// `secure:false` is positively known not to be a credential entry.
const iosLoginTreeJSON = `{
  "attributes": {"bounds": "[0,0,390,844]", "class": "Window"},
  "children": [
    {"attributes": {"identifier": "LoginEmail", "class": "TextField", "hintText": "Email", "bounds": "[20,200,370,244]"},
     "editable": true, "enabled": true, "secure": false, "children": []},
    {"attributes": {"identifier": "LoginPassword", "class": "SecureTextField", "hintText": "Password", "bounds": "[20,260,370,304]"},
     "editable": true, "enabled": true, "secure": true, "children": []}
  ]
}`

// webLoginTreeJSON is the same form as the chrome dump reports it.
const webLoginTreeJSON = `{
  "attributes": {"bounds": "[0,0,1280,800]", "tag": "html"},
  "children": [
    {"attributes": {"resource-id": "login-email", "tag": "input", "hintText": "Email", "bounds": "[20,200,400,240]"},
     "editable": true, "enabled": true, "secure": false, "children": []},
    {"attributes": {"resource-id": "login-password", "tag": "input", "hintText": "Password", "bounds": "[20,260,400,300]"},
     "editable": true, "enabled": true, "secure": true, "children": []}
  ]
}`

// androidLoginTreeJSON is the same form with the fact missing from both fields,
// which is what an Android tree looks like when the sidecar could not match its
// text fields against the device's own view hierarchy.
const androidLoginTreeJSON = `{
  "attributes": {"bounds": "[0,0,1080,2340]"},
  "children": [
    {"attributes": {"resource-id": "login_email", "class": "android.widget.EditText", "hintText": "Email", "bounds": "[20,200,1060,300]"},
     "enabled": true, "children": []},
    {"attributes": {"resource-id": "login_password", "class": "android.widget.EditText", "hintText": "Password", "bounds": "[20,320,1060,420]"},
     "enabled": true, "children": []}
  ]
}`

func typeInto(selector string) verifier.Action {
	return verifier.Action{Kind: verifier.ActionKindInputText, On: selector, Text: typedCredential}
}

func TestTraceActionForRedactsTypedValuesTheTargetCannotClear(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		treeJSON string
		selector string
	}{
		{"ios secure field", iosLoginTreeJSON, "id:LoginPassword"},
		{"web secure field", webLoginTreeJSON, "id:login-password"},
		{"field reported as neither", androidLoginTreeJSON, "id:login_email"},
		{"password field reported as neither", androidLoginTreeJSON, "id:login_password"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			traceAction := traceActionFor(typeInto(testCase.selector), mustParseTree(t, testCase.treeJSON))
			if traceAction.Text != verifier.RedactedInputText {
				t.Errorf("trace action text = %q, want it redacted", traceAction.Text)
			}
			if traceAction.Selector != testCase.selector {
				t.Errorf("trace selector = %q, want %q so the record still shows which field was typed into",
					traceAction.Selector, testCase.selector)
			}
		})
	}
}

func TestTraceActionForKeepsTypedValuesForAFieldReportedNotSecure(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		treeJSON string
		selector string
	}{
		{"ios", iosLoginTreeJSON, "id:LoginEmail"},
		{"web", webLoginTreeJSON, "id:login-email"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			const address = "ada@example.com"
			action := verifier.Action{Kind: verifier.ActionKindInputText, On: testCase.selector, Text: address}
			traceAction := traceActionFor(action, mustParseTree(t, testCase.treeJSON))
			if traceAction.Text != address {
				t.Errorf("trace action text = %q, want the typed value on a field reported not secure", traceAction.Text)
			}
		})
	}
}

// The redaction is a rendering rule, never a change to what is dispatched: the
// app has to receive the keystrokes a user would have produced.
func TestApplyActionTypesTheRealValueIntoEveryField(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		treeJSON string
		selector string
	}{
		{"ios secure field", iosLoginTreeJSON, "id:LoginPassword"},
		{"web secure field", webLoginTreeJSON, "id:login-password"},
		{"android field", androidLoginTreeJSON, "id:login_password"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fastFocusSettle(t)
			driverMock := mockdriver.New()
			mustDispatch(t, driverMock, typeInto(testCase.selector), mustParseTree(t, testCase.treeJSON))

			typed := ""
			for _, recorded := range driverMock.Actions() {
				if recorded.Kind == mockdriver.ActionInputText {
					typed = recorded.Text
				}
			}
			if typed != typedCredential {
				t.Errorf("driver received %q, want the real typed value", typed)
			}
		})
	}
}

// lastActionExtractorSpec is what examples/folio/sanderling/spec.ts does with
// the previous step's action: it extracts state.lastAction whole. Extractor
// values are written to the trace as extractor_changes, so a typed value that
// reaches state.lastAction.text lands in the run directory through the spec
// rather than through the runner.
const lastActionExtractorSpec = `
import { actions, always, extract, InputText } from "@sanderling/spec";
const reported = extract("lastAction", state => state.lastAction);
globalThis.properties = {
  theActionReachesTheSpec: always(() => reported.current !== undefined),
};
globalThis.actions = actions(() => [InputText({ into: "%s", text: "` + typedCredential + `" })]);
`

func TestTheTraceNeverCarriesATypedSecretThroughALastActionExtractor(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		treeJSON string
		selector string
	}{
		{"ios secure field", iosLoginTreeJSON, "id:LoginPassword"},
		{"web secure field", webLoginTreeJSON, "id:login-password"},
		{"android field reported as neither", androidLoginTreeJSON, "id:login_email"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fastFocusSettle(t)
			state := newHarnessWithSpec(t, fmt.Sprintf(lastActionExtractorSpec, testCase.selector))
			state.mock.HierarchyJSON = testCase.treeJSON

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if _, err := Run(ctx, Options{
				Duration:    time.Hour,
				IdleTimeout: 20 * time.Millisecond,
				MaxSteps:    2,
				Driver:      state.mock,
				Verifier:    state.verifier,
				TraceWriter: state.writer,
			}); err != nil {
				t.Fatalf("Run: %v", err)
			}

			steps := traceSteps(t, state.writer.Directory())
			if len(steps) < 2 {
				t.Fatalf("trace holds %d step(s), want the step that reports the action back", len(steps))
			}
			change, ok := steps[1].ExtractorChanges["lastAction"]
			if !ok {
				t.Fatalf("step 2 recorded no reading of state.lastAction: %+v", steps[1])
			}
			if strings.Contains(string(change.Curr), typedCredential) {
				t.Errorf("the trace carries the typed value through state.lastAction: %s", change.Curr)
			}
			if !strings.Contains(string(change.Curr), verifier.RedactedInputText) {
				t.Errorf("state.lastAction reported %s, want the typed value redacted in place",
					change.Curr)
			}
		})
	}
}

const secureLoginSpec = `
import { llm, always, actions, InputText, taps, typing, weighted } from "@sanderling/spec";
globalThis.properties = { ok: always(() => true) };
globalThis.setup = actions(() => {
  if (globalThis.__typed) return [];
  globalThis.__typed = true;
  return [InputText({ into: "id:LoginPassword", text: "` + typedCredential + `" })];
});
globalThis.actions = weighted([1, taps], [1, typing]);
globalThis.generator = llm({ model: "test/model" });
`

// The recorded call is llm-calls.jsonl: its user prompt carries the
// recent-action memory and its candidates carry the numbered list, which is
// where a login run put the account password in cleartext beside a screenshot
// of the same screen.
func TestLLMCallRecordKeepsTypedSecretsOutOfThePromptAndCandidates(t *testing.T) {
	fake := newFakeOpenRouter(t)
	source, verifierInstance := newLLMSourceWithSpec(t, fake, secureLoginSpec)

	pushSnapshotTree(t, verifierInstance, iosLoginTreeJSON)
	action, err := source.NextAction(context.Background(), 0)
	if err != nil {
		t.Fatalf("setup NextAction: %v", err)
	}
	if action.Text != typedCredential {
		t.Fatalf("setup action text = %q, want the real value dispatched to the app", action.Text)
	}

	pushSnapshotTree(t, verifierInstance, iosLoginTreeJSON)
	fake.choice = 1
	fake.chosenAction = mustCandidates(t, verifierInstance, verifier.LabelSourceVisibleText)[0].Description
	if _, err := source.NextAction(context.Background(), 1); err != nil {
		t.Fatalf("llm NextAction: %v", err)
	}

	call := lastCall(t, source)
	if strings.Contains(call.UserPrompt, typedCredential) {
		t.Error("the recorded user prompt carries the typed value")
	}
	if !strings.Contains(call.UserPrompt, "InputText") {
		t.Errorf("the recorded user prompt lost the recent-action memory entirely: %q", call.UserPrompt)
	}
	for _, candidate := range call.Candidates {
		if strings.Contains(candidate.Description, typedCredential) {
			t.Errorf("recorded candidate %d carries the typed value: %q", candidate.Index, candidate.Description)
		}
	}
}
