package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/driver"
	mockdriver "github.com/priyanshujain/sanderling/internal/driver/mock"
	"github.com/priyanshujain/sanderling/internal/hierarchy"
	"github.com/priyanshujain/sanderling/internal/llmclient"
	"github.com/priyanshujain/sanderling/internal/trace"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

// llmInputCorpus mirrors pkg/spec/src/corpus.ts INPUT_CORPUS so an InputText
// value drawn by the shared sampler can be asserted to come from the pool.
var llmInputCorpus = []string{
	"", "a", strings.Repeat("a", 4096), "🙂🔥💸", "  ", "\t\n", "-1",
	"999999999999999999999", "0.0000001", "1e10", "'; DROP TABLE--",
	"<script>alert(1)</script>", "../../etc/passwd", "%s%n", "NaN",
}

const llmFixtureSpec = `
import { llm, always, taps, typing, weighted } from "@sanderling/spec";
globalThis.properties = { ok: always(() => true) };
globalThis.actions = weighted([1, taps], [1, typing]);
globalThis.generator = llm({ model: "test/model" });
`

const llmTreeJSON = `{
  "attributes": {"bounds": "[0,0,400,800]"},
  "children": [
    {"attributes": {"resource-id": "Submit", "text": "Submit", "bounds": "[0,0,400,100]"}, "clickable": true, "enabled": true, "children": []},
    {"attributes": {"resource-id": "Name", "class": "EditText", "bounds": "[0,100,400,200]"}, "enabled": true, "children": []}
  ]
}`

func TestActionForCandidatePassesNonTypingThrough(t *testing.T) {
	source := &llmSource{}
	candidate := verifier.ActionCandidate{
		Kind:   verifier.ActionKindTap,
		Action: verifier.Action{Kind: verifier.ActionKindTap, On: "id:Submit", X: 10, Y: 20},
	}
	action, err := source.actionForCandidate(candidate, "ignored")
	if err != nil {
		t.Fatalf("actionForCandidate: %v", err)
	}
	if action.Kind != verifier.ActionKindTap || action.On != "id:Submit" || action.X != 10 || action.Y != 20 {
		t.Errorf("tap = %+v, want the candidate action verbatim", action)
	}
	if action.Text != "" {
		t.Errorf("non-typing action must not carry text, got %q", action.Text)
	}
}

func TestDescribeActionNamesGesturesByOrigin(t *testing.T) {
	builtin := verifier.Action{
		Kind: verifier.ActionKindScroll, Direction: "down",
		FromX: 200, FromY: 500, ToX: 200, ToY: 340,
	}
	if got := describeAction(builtin); got != "Scroll down (200,500)" {
		t.Errorf("builtin gesture described as %q, want the drag origin", got)
	}
	authored := verifier.Action{Kind: verifier.ActionKindScroll, Direction: "up", On: "id:List"}
	if got := describeAction(authored); got != "Scroll up id:List" {
		t.Errorf("authored scroll described as %q, want its selector", got)
	}
}

func TestActionForCandidateUsesModelText(t *testing.T) {
	source := &llmSource{}
	candidate := verifier.ActionCandidate{
		Kind:    verifier.ActionKindInputText,
		LLMText: true,
		Action:  verifier.Action{Kind: verifier.ActionKindInputText, On: "id:Name"},
	}
	action, err := source.actionForCandidate(candidate, "Priya")
	if err != nil {
		t.Fatalf("actionForCandidate: %v", err)
	}
	if action.Text != "Priya" {
		t.Errorf("text = %q, want the model-supplied value", action.Text)
	}
}

func TestActionForCandidateAuthoredTypingKeepsSampledValue(t *testing.T) {
	source := &llmSource{}
	candidate := verifier.ActionCandidate{
		Kind:    verifier.ActionKindInputText,
		LLMText: false, // authored InputText: replay the spec's sampled value
		Action:  verifier.Action{Kind: verifier.ActionKindInputText, On: "id:Amount", Text: "42"},
	}
	action, err := source.actionForCandidate(candidate, "ignored")
	if err != nil {
		t.Fatalf("actionForCandidate: %v", err)
	}
	if action.Text != "42" {
		t.Errorf("text = %q, want the authored value 42", action.Text)
	}
}

func TestActionForCandidateFallsBackToSampler(t *testing.T) {
	fake := newFakeOpenRouter(t)
	source, _ := newLLMSource(t, fake)
	candidate := verifier.ActionCandidate{
		Kind:    verifier.ActionKindInputText,
		LLMText: true,
		Action:  verifier.Action{Kind: verifier.ActionKindInputText, On: "id:Name"},
	}
	action, err := source.actionForCandidate(candidate, "   ")
	if err != nil {
		t.Fatalf("actionForCandidate: %v", err)
	}
	if !slices.Contains(llmInputCorpus, action.Text) {
		t.Errorf("empty model text should fall back to the corpus, got %q", action.Text)
	}
}

func TestParseChoice(t *testing.T) {
	out, err := parseChoice(`{"reasoning":"go home","choice":3,"chosen_action":"Tap \"Home\"","text":""}`)
	if err != nil {
		t.Fatalf("parseChoice: %v", err)
	}
	if out.Reasoning != "go home" || out.Choice != 3 || out.ChosenAction != `Tap "Home"` {
		t.Errorf("parseChoice = %+v", out)
	}

	if _, err := parseChoice(""); err == nil {
		t.Error("expected error for empty content")
	}
	if _, err := parseChoice(`{"reasoning":"x","choice":0,"chosen_action":"","text":""}`); err == nil {
		t.Error("expected error for a zero choice")
	}
	if _, err := parseChoice(`not json`); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestSystemPromptAppendsInstructions(t *testing.T) {
	if got := (&llmSource{}).systemPrompt(); got != llmSystemPrompt {
		t.Error("empty instructions should yield the base prompt unchanged")
	}
	withInstr := (&llmSource{instructions: "hunt for double submits"}).systemPrompt()
	if !strings.Contains(withInstr, llmSystemPrompt) {
		t.Error("system prompt must retain the base framing")
	}
	if !strings.Contains(withInstr, "hunt for double submits") {
		t.Error("system prompt must include the spec instructions")
	}
}

// fakeOpenRouter is a configurable in-process OpenRouter server. Set ranked /
// reasoning before each call; it echoes them as a json_schema content body.
type fakeOpenRouter struct {
	server       *httptest.Server
	choice       int
	chosenAction string
	text         string
	reasoning    string
	usage        llmclient.Usage
	servedModel  string
	delay        time.Duration
	lastRequest  map[string]any
}

func newFakeOpenRouter(t *testing.T) *fakeOpenRouter {
	t.Helper()
	fake := &fakeOpenRouter{reasoning: "because"}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &fake.lastRequest)
		content, _ := json.Marshal(map[string]any{
			"reasoning":     fake.reasoning,
			"choice":        fake.choice,
			"chosen_action": fake.chosenAction,
			"text":          fake.text,
		})
		response, _ := json.Marshal(llmclient.Response{
			Model:   fake.servedModel,
			Choices: []llmclient.Choice{{Message: llmclient.ResponseMessage{Content: string(content)}}},
			Usage:   fake.usage,
		})
		time.Sleep(fake.delay)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(response)
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

// fakeCallRecorder keeps the per-step selection records in memory so a test can
// assert on them without opening the run directory.
type fakeCallRecorder struct {
	calls []trace.LLMCall
}

func (r *fakeCallRecorder) WriteLLMCall(call trace.LLMCall) error {
	r.calls = append(r.calls, call)
	return nil
}

func recordedCalls(t *testing.T, source *llmSource) []trace.LLMCall {
	t.Helper()
	recorder, ok := source.recorder.(*fakeCallRecorder)
	if !ok {
		t.Fatalf("recorder = %T, want *fakeCallRecorder", source.recorder)
	}
	return recorder.calls
}

func lastCall(t *testing.T, source *llmSource) trace.LLMCall {
	t.Helper()
	calls := recordedCalls(t, source)
	if len(calls) == 0 {
		t.Fatal("no selection record written")
	}
	return calls[len(calls)-1]
}

// mustCandidates enumerates the model policy's list, failing the test on the
// refusal an authored multi-item sampler raises.
func mustCandidates(t *testing.T, v *verifier.Verifier, labelSource string) []verifier.ActionCandidate {
	t.Helper()
	candidates, err := v.Candidates(labelSource)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	return candidates
}

func newLLMSource(t *testing.T, fake *fakeOpenRouter) (*llmSource, *verifier.Verifier) {
	t.Helper()
	return newLLMSourceWithSpec(t, fake, llmFixtureSpec)
}

func newLLMSourceWithSpec(t *testing.T, fake *fakeOpenRouter, spec string) (*llmSource, *verifier.Verifier) {
	t.Helper()
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("OPENROUTER_BASE_URL", fake.server.URL)
	client, err := llmclient.New()
	if err != nil {
		t.Fatalf("llmclient.New: %v", err)
	}

	verifierInstance, err := verifier.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := verifierInstance.Load(bundleSpec(t, spec)); err != nil {
		t.Fatal(err)
	}
	if _, ok := verifierInstance.LLMConfig(); !ok {
		t.Fatal("llm fixture spec did not register the llm action backend")
	}

	source := &llmSource{
		verifier: verifierInstance,
		client:   client,
		model:    "test/model",
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		history:  newActionHistory(llmHistorySize),
		recorder: &fakeCallRecorder{},
	}
	return source, verifierInstance
}

func pushLLMSnapshot(t *testing.T, v *verifier.Verifier) {
	t.Helper()
	pushLLMSnapshotAtStep(t, v, 0)
}

func pushSnapshotTree(t *testing.T, v *verifier.Verifier, treeJSON string) {
	t.Helper()
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.PushSnapshot(verifier.SnapshotInput{
		Tree:          tree,
		ScreenshotPNG: tinyPNG(t),
	}); err != nil {
		t.Fatalf("PushSnapshot: %v", err)
	}
}

func pushLLMSnapshotAtStep(t *testing.T, v *verifier.Verifier, stepIndex int) {
	t.Helper()
	tree, err := hierarchy.Parse(llmTreeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.PushSnapshot(verifier.SnapshotInput{
		Tree:          tree,
		ScreenshotPNG: tinyPNG(t),
		StepIndex:     stepIndex,
	}); err != nil {
		t.Fatalf("PushSnapshot: %v", err)
	}
}

func readLLMCalls(t *testing.T, directory string) []trace.LLMCall {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(directory, trace.LLMCallFileName))
	if err != nil {
		t.Fatalf("read %s: %v", trace.LLMCallFileName, err)
	}
	var calls []trace.LLMCall
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		var call trace.LLMCall
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			t.Fatalf("decode record %q: %v", line, err)
		}
		calls = append(calls, call)
	}
	return calls
}

func candidateByKind(t *testing.T, candidates []verifier.ActionCandidate, kind verifier.ActionKind) verifier.ActionCandidate {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.Kind == kind {
			return candidate
		}
	}
	t.Fatalf("no candidate of kind %q in %v", kind, candidates)
	return verifier.ActionCandidate{}
}

func TestPickSourcesSelectsLLMWhenRequested(t *testing.T) {
	fake := newFakeOpenRouter(t)
	_, verifierInstance := newLLMSource(t, fake)
	action, _, err := pickSources(Options{
		Verifier:  verifierInstance,
		Generator: "llm",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("pickSources: %v", err)
	}
	if _, ok := action.(*llmSource); !ok {
		t.Errorf("action source = %T, want *llmSource for --generator llm", action)
	}
}

func TestPickSourcesSeededByDefault(t *testing.T) {
	fake := newFakeOpenRouter(t)
	_, verifierInstance := newLLMSource(t, fake)
	action, _, err := pickSources(Options{
		Verifier:  verifierInstance,
		Generator: "seeded",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("pickSources: %v", err)
	}
	// Even with a generator = llm() config present, the seeded flag wins.
	if _, ok := action.(gojaSource); !ok {
		t.Errorf("action source = %T, want gojaSource for --generator seeded", action)
	}
}

// TestPickSourcesGivesTheLabelSourceToTheModelPickerOnly pins the asymmetry the
// labelling factorial depends on: the label channel reaches the model picker,
// and the seeded picker is handed a source that has nowhere to put one.
func TestPickSourcesGivesTheLabelSourceToTheModelPickerOnly(t *testing.T) {
	fake := newFakeOpenRouter(t)
	_, verifierInstance := newLLMSource(t, fake)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	action, _, err := pickSources(Options{
		Verifier:    verifierInstance,
		Generator:   "llm",
		LabelSource: verifier.LabelSourceResourceID,
		Logger:      logger,
	})
	if err != nil {
		t.Fatalf("pickSources: %v", err)
	}
	model, ok := action.(*llmSource)
	if !ok {
		t.Fatalf("action source = %T, want *llmSource", action)
	}
	if model.labelSource != verifier.LabelSourceResourceID {
		t.Errorf("labelSource = %q, want %q", model.labelSource, verifier.LabelSourceResourceID)
	}

	seeded, _, err := pickSources(Options{
		Verifier:    verifierInstance,
		Generator:   "seeded",
		LabelSource: verifier.LabelSourceResourceID,
		Logger:      logger,
	})
	if err != nil {
		t.Fatalf("pickSources: %v", err)
	}
	if _, ok := seeded.(gojaSource); !ok {
		t.Errorf("action source = %T, want gojaSource, which carries no label channel", seeded)
	}
}

// labelSplitTreeJSON names one control two ways, so a record can say which
// channel the model was reading.
const labelSplitTreeJSON = `{
  "attributes": {"bounds": "[0,0,400,800]"},
  "children": [
    {"attributes": {"resource-id": "add_credit_button", "text": "Add credit", "bounds": "[0,0,400,100]"}, "clickable": true, "enabled": true, "children": []}
  ]
}`

// TestLLMSourceRecordsTheLabelsTheModelSaw closes the loop from the flag to the
// artifact: llm-calls.jsonl carries the candidate list as rendered, so a
// directory of runs can be checked against the cell it claims rather than
// trusted.
func TestLLMSourceRecordsTheLabelsTheModelSaw(t *testing.T) {
	for _, want := range []struct{ labelSource, label string }{
		{verifier.LabelSourceVisibleText, "Add credit"},
		{verifier.LabelSourceResourceID, "add_credit_button"},
	} {
		t.Run(want.labelSource, func(t *testing.T) {
			fake := newFakeOpenRouter(t)
			source, verifierInstance := newLLMSource(t, fake)
			source.labelSource = want.labelSource
			pushSnapshotTree(t, verifierInstance, labelSplitTreeJSON)

			tap := candidateByKind(t, mustCandidates(t, verifierInstance, want.labelSource), verifier.ActionKindTap)
			fake.choice = tap.Index
			fake.chosenAction = tap.Description
			if _, err := source.NextAction(context.Background(), 0); err != nil {
				t.Fatalf("NextAction: %v", err)
			}

			call := lastCall(t, source)
			if call.Outcome != trace.LLMOutcomeSelected {
				t.Fatalf("outcome = %q, want selected", call.Outcome)
			}
			if len(call.Candidates) == 0 {
				t.Fatal("no candidates recorded")
			}
			if call.Candidates[0].Label != want.label {
				t.Errorf("recorded label = %q, want %q", call.Candidates[0].Label, want.label)
			}
			if !strings.Contains(call.UserPrompt, want.label) {
				t.Errorf("prompt does not carry %q:\n%s", want.label, call.UserPrompt)
			}
		})
	}
}

// seededFixtureSpec declares no generator = llm(...), so --generator llm has
// nothing to build a picker from.
const seededFixtureSpec = `
import { always, taps, typing, weighted } from "@sanderling/spec";
globalThis.properties = { ok: always(() => true) };
globalThis.actions = weighted([1, taps], [1, typing]);
`

func newSeededVerifier(t *testing.T) *verifier.Verifier {
	t.Helper()
	verifierInstance, err := verifier.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := verifierInstance.Load(bundleSpec(t, seededFixtureSpec)); err != nil {
		t.Fatal(err)
	}
	if _, ok := verifierInstance.LLMConfig(); ok {
		t.Fatal("seeded fixture spec must not register an llm action backend")
	}
	return verifierInstance
}

// TestPickSourcesRejectsLLMWithoutSpecConfig pins the abort. Falling back to the
// seeded picker here completes the run, writes a well-formed output directory,
// and records it under the requested arm, so a campaign cell silently reports
// the wrong policy's numbers.
func TestPickSourcesRejectsLLMWithoutSpecConfig(t *testing.T) {
	for name, activeDriver := range map[string]driver.DeviceDriver{
		"native": nil,
		"web":    &webMockDriver{Driver: mockdriver.New()},
	} {
		t.Run(name, func(t *testing.T) {
			action, extractor, err := pickSources(Options{
				Driver:    activeDriver,
				Verifier:  newSeededVerifier(t),
				Generator: "llm",
				Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
			})
			if err == nil {
				t.Fatalf("pickSources = (%T, %T), want an error", action, extractor)
			}
			if action != nil || extractor != nil {
				t.Errorf("sources = (%v, %v), want both nil alongside the error", action, extractor)
			}
			if !strings.Contains(err.Error(), "generator = llm(...)") {
				t.Errorf("error = %q, want it to name the missing spec declaration", err)
			}
		})
	}
}

// TestPickSourcesOnWebComposesLLMWithWebExtractors covers the second half of the
// same claim: the picker is chosen by --generator and the extractor source by
// the driver, so the llm policy runs on web instead of being silently replaced
// by the V8 picker.
func TestPickSourcesOnWebComposesLLMWithWebExtractors(t *testing.T) {
	fake := newFakeOpenRouter(t)
	_, verifierInstance := newLLMSource(t, fake)
	action, extractor, err := pickSources(Options{
		Driver:    &webMockDriver{Driver: mockdriver.New()},
		Verifier:  verifierInstance,
		Generator: "llm",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("pickSources: %v", err)
	}
	if _, ok := action.(*llmSource); !ok {
		t.Errorf("action source = %T, want *llmSource on web with --generator llm", action)
	}
	if _, ok := extractor.(webSource); !ok {
		t.Errorf("extractor source = %T, want webSource so overrides still come from V8", extractor)
	}
}

func TestPickSourcesOnWebSeededKeepsBothOnV8(t *testing.T) {
	action, extractor, err := pickSources(Options{
		Driver:    &webMockDriver{Driver: mockdriver.New()},
		Verifier:  newSeededVerifier(t),
		Generator: "seeded",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("pickSources: %v", err)
	}
	if _, ok := action.(webSource); !ok {
		t.Errorf("action source = %T, want webSource for the seeded web path", action)
	}
	if _, ok := extractor.(webSource); !ok {
		t.Errorf("extractor source = %T, want webSource for the seeded web path", extractor)
	}
}

func TestLLMSourceDrivesExecutedActions(t *testing.T) {
	fake := newFakeOpenRouter(t)
	source, verifierInstance := newLLMSource(t, fake)
	pushLLMSnapshot(t, verifierInstance)
	candidates := mustCandidates(t, verifierInstance, verifier.LabelSourceVisibleText)

	// Step 1: the model picks the Tap on Submit by its number, echoing its
	// description.
	tap := candidateByKind(t, candidates, verifier.ActionKindTap)
	fake.choice = tap.Index
	fake.chosenAction = tap.Description
	fake.reasoning = "tap submit"
	fake.text = ""
	action, err := source.NextAction(context.Background(), 1)
	if err != nil {
		t.Fatalf("NextAction: %v", err)
	}
	if action.Kind != verifier.ActionKindTap || action.On != "id:Submit" {
		t.Errorf("step 1 action = %+v, want Tap on id:Submit", action)
	}
	if source.lastSource != "llm" || source.lastReasoning != "tap submit" {
		t.Errorf("source state = %q/%q, want llm/tap submit", source.lastSource, source.lastReasoning)
	}

	// The request carried the model and a screenshot image part.
	if fake.lastRequest["model"] != "test/model" {
		t.Errorf("request model = %v", fake.lastRequest["model"])
	}
	if !requestHasImage(fake.lastRequest) {
		t.Error("request carried no screenshot image part")
	}

	// Step 2: the model picks the typing candidate and supplies the value.
	pushLLMSnapshot(t, verifierInstance)
	typing := candidateByKind(t, candidates, verifier.ActionKindInputText)
	fake.choice = typing.Index
	fake.chosenAction = typing.Description
	fake.reasoning = "type a name"
	fake.text = "Priya"
	action, err = source.NextAction(context.Background(), 1)
	if err != nil {
		t.Fatalf("NextAction: %v", err)
	}
	if action.Kind != verifier.ActionKindInputText || action.On != "id:Name" {
		t.Errorf("step 2 action = %+v, want InputText on id:Name", action)
	}
	if action.Text != "Priya" {
		t.Errorf("InputText text = %q, want the model-supplied Priya", action.Text)
	}

	// The trace records source=llm, the reasoning, the choice, and the echo.
	traceAction := traceActionFor(action, nil)
	stampActionSource(traceAction, source)
	if traceAction.Source != "llm" || traceAction.LLMReasoning != "type a name" {
		t.Errorf("trace action = %+v, want source=llm reasoning=type a name", traceAction)
	}
	if traceAction.LLMChoice != typing.Index || traceAction.LLMChosenAction != typing.Description {
		t.Errorf("trace choice = %d/%q, want %d/%q", traceAction.LLMChoice, traceAction.LLMChosenAction, typing.Index, typing.Description)
	}
}

func TestLLMSourceSkipsOnOutOfRangeChoice(t *testing.T) {
	fake := newFakeOpenRouter(t)
	source, verifierInstance := newLLMSource(t, fake)
	pushLLMSnapshot(t, verifierInstance)

	fake.choice = 9999
	fake.chosenAction = "whatever"
	_, err := source.NextAction(context.Background(), 1)
	if !errors.Is(err, verifier.ErrNoAction) {
		t.Fatalf("NextAction err = %v, want ErrNoAction for an out-of-range choice", err)
	}
	if source.lastSource != "" {
		t.Errorf("lastSource = %q, want empty after a skipped step", source.lastSource)
	}
}

func TestLLMSourceAcceptsEchoWithWeightSuffix(t *testing.T) {
	// Real models copy the whole numbered line, including its trailing "(w34)"
	// weight annotation. That must still count as a match, not a strict skip.
	fake := newFakeOpenRouter(t)
	source, verifierInstance := newLLMSource(t, fake)
	pushLLMSnapshot(t, verifierInstance)
	candidates := mustCandidates(t, verifierInstance, verifier.LabelSourceVisibleText)

	tap := candidateByKind(t, candidates, verifier.ActionKindTap)
	fake.choice = tap.Index
	fake.chosenAction = tap.Description + "  (w" + strconv.Itoa(tap.Weight) + ")"
	action, err := source.NextAction(context.Background(), 1)
	if err != nil {
		t.Fatalf("NextAction: %v", err)
	}
	if action.Kind != verifier.ActionKindTap {
		t.Errorf("action = %+v, want Tap; the weight-suffixed echo was wrongly rejected", action)
	}
	if source.lastSource != "llm" {
		t.Error("weight-suffixed echo should be accepted, not strict-skipped")
	}
}

func TestStripWeightSuffix(t *testing.T) {
	cases := map[string]string{
		`Tap "+ Add account"  (w34)`: `Tap "+ Add account"`,
		`Tap "Sign in"`:              `Tap "Sign in"`,
		`Scroll down (w7)`:           `Scroll down`,
		`  Tap "x" (w1)  `:           `Tap "x"`,
	}
	for in, want := range cases {
		if got := stripWeightSuffix(in); got != want {
			t.Errorf("stripWeightSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLLMSourceStrictSkipsOnEchoMismatch(t *testing.T) {
	fake := newFakeOpenRouter(t)
	source, verifierInstance := newLLMSource(t, fake)
	pushLLMSnapshot(t, verifierInstance)
	candidates := mustCandidates(t, verifierInstance, verifier.LabelSourceVisibleText)

	// A valid number, but the echoed action disagrees with that numbered entry:
	// the model reasoned about one control and picked another's number.
	tap := candidateByKind(t, candidates, verifier.ActionKindTap)
	fake.choice = tap.Index
	fake.chosenAction = "Tap \"Something Else\""
	_, err := source.NextAction(context.Background(), 1)
	if !errors.Is(err, verifier.ErrNoAction) {
		t.Fatalf("NextAction err = %v, want ErrNoAction on chosen_action mismatch", err)
	}
	if source.lastSource != "" {
		t.Errorf("lastSource = %q, want empty after a strict skip", source.lastSource)
	}
}

// llmSharedLabelTreeJSON has two rows a user reads as the same word, so the
// numbered list holds two entries rendering identically.
const llmSharedLabelTreeJSON = `{
  "attributes": {"bounds": "[0,0,400,800]"},
  "children": [
    {"attributes": {"resource-id": "delete_alpha", "text": "Delete", "bounds": "[0,0,400,100]"}, "clickable": true, "enabled": true, "children": []},
    {"attributes": {"resource-id": "delete_beta", "text": "Delete", "bounds": "[0,100,400,200]"}, "clickable": true, "enabled": true, "children": []}
  ]
}`

// TestLLMSourceEchoGuardAdmitsARepeatedDescription is the other half of the
// strict skip: it compares the echo against the entry the model NUMBERED, so a
// description shared by two entries still selects the one whose number came
// back. A guard that looked the echo up by description instead would run the
// first row for both numbers.
func TestLLMSourceEchoGuardAdmitsARepeatedDescription(t *testing.T) {
	fake := newFakeOpenRouter(t)
	source, verifierInstance := newLLMSource(t, fake)
	pushSnapshotTree(t, verifierInstance, llmSharedLabelTreeJSON)

	var repeated []verifier.ActionCandidate
	for _, candidate := range mustCandidates(t, verifierInstance, verifier.LabelSourceVisibleText) {
		if candidate.Description == `Tap "Delete"` {
			repeated = append(repeated, candidate)
		}
	}
	if len(repeated) != 2 {
		t.Fatalf("want two entries sharing one description, got %d", len(repeated))
	}

	second := repeated[1]
	fake.choice = second.Index
	fake.chosenAction = second.Description
	action, err := source.NextAction(context.Background(), 1)
	if err != nil {
		t.Fatalf("NextAction err = %v, want the second row's tap", err)
	}
	if action.On != "id:delete_beta" {
		t.Errorf("action targets %q, want id:delete_beta", action.On)
	}
	if source.lastSource != "llm" {
		t.Errorf("lastSource = %q, want llm; a repeated description must not strict-skip", source.lastSource)
	}
}

func TestLLMSourceSkipsOnHTTPError(t *testing.T) {
	fake := newFakeOpenRouter(t)
	// Replace the handler with one that always errors.
	fake.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	source, verifierInstance := newLLMSource(t, fake)
	pushLLMSnapshot(t, verifierInstance)

	fake.choice = 1
	_, err := source.NextAction(context.Background(), 1)
	if !errors.Is(err, verifier.ErrNoAction) {
		t.Fatalf("NextAction err = %v, want ErrNoAction on HTTP failure", err)
	}
}

// llmSetupFixtureSpec drives the first action from setup, so the model is never
// consulted for that step.
const llmSetupFixtureSpec = `
import { llm, always, actions, taps, typing, weighted, Tap } from "@sanderling/spec";
globalThis.properties = { ok: always(() => true) };
globalThis.setup = actions(() => [Tap({ on: "id:Submit" })]);
globalThis.actions = weighted([1, taps], [1, typing]);
globalThis.generator = llm({ model: "test/model" });
`

// TestLLMCallRecordSeparatesGuardSkipFromDecline pins the reason these records
// exist. A step the echo guard threw away and a step where the picker had
// nothing to choose both used to leave nothing behind but a log line, so any
// defect-yield or actions-per-hour figure computed from a model run silently
// mixed the two with each other and with steps that acted.
func TestLLMCallRecordSeparatesGuardSkipFromDecline(t *testing.T) {
	const stepIndex = 7

	guardSkipped := func(t *testing.T) trace.LLMCall {
		fake := newFakeOpenRouter(t)
		source, verifierInstance := newLLMSource(t, fake)
		pushLLMSnapshot(t, verifierInstance)
		tap := candidateByKind(t, mustCandidates(t, verifierInstance, verifier.LabelSourceVisibleText), verifier.ActionKindTap)
		fake.choice = tap.Index
		fake.chosenAction = `Tap "Something Else"`
		if _, err := source.NextAction(context.Background(), stepIndex); !errors.Is(err, verifier.ErrNoAction) {
			t.Fatalf("NextAction err = %v, want ErrNoAction on echo mismatch", err)
		}
		return lastCall(t, source)
	}
	declined := func(t *testing.T) trace.LLMCall {
		fake := newFakeOpenRouter(t)
		// No snapshot pushed, so the action tree yields nothing to pick from.
		source, _ := newLLMSource(t, fake)
		if _, err := source.NextAction(context.Background(), stepIndex); !errors.Is(err, verifier.ErrNoAction) {
			t.Fatalf("NextAction err = %v, want ErrNoAction with no candidates", err)
		}
		return lastCall(t, source)
	}
	acted := func(t *testing.T) trace.LLMCall {
		fake := newFakeOpenRouter(t)
		source, verifierInstance := newLLMSource(t, fake)
		pushLLMSnapshot(t, verifierInstance)
		tap := candidateByKind(t, mustCandidates(t, verifierInstance, verifier.LabelSourceVisibleText), verifier.ActionKindTap)
		fake.choice = tap.Index
		fake.chosenAction = tap.Description
		if _, err := source.NextAction(context.Background(), stepIndex); err != nil {
			t.Fatalf("NextAction: %v", err)
		}
		return lastCall(t, source)
	}

	skip, decline, pick := guardSkipped(t), declined(t), acted(t)
	if skip.Outcome != trace.LLMOutcomeEchoMismatch {
		t.Errorf("guard-skipped outcome = %q, want %q", skip.Outcome, trace.LLMOutcomeEchoMismatch)
	}
	if decline.Outcome != trace.LLMOutcomeNoCandidates {
		t.Errorf("declined outcome = %q, want %q", decline.Outcome, trace.LLMOutcomeNoCandidates)
	}
	if pick.Outcome != trace.LLMOutcomeSelected {
		t.Errorf("executed outcome = %q, want %q", pick.Outcome, trace.LLMOutcomeSelected)
	}
	for _, call := range []trace.LLMCall{skip, decline, pick} {
		if call.Step != stepIndex {
			t.Errorf("record step = %d, want %d so it joins its trace line", call.Step, stepIndex)
		}
		if call.Timestamp.IsZero() {
			t.Error("record carries no timestamp")
		}
	}
	// The guard skip must carry what only the dropped log line used to hold.
	if skip.Choice == 0 || skip.EchoedAction != `Tap "Something Else"` || skip.RawResponse == "" {
		t.Errorf("guard-skip record = %+v, want the choice, the echo, and the raw response", skip)
	}
	if len(skip.Candidates) == 0 {
		t.Error("guard-skip record must keep the candidate list the mismatch is judged against")
	}
	// A decline never reached the provider, so it must not look like a call.
	if decline.RawResponse != "" || decline.UserPrompt != "" || len(decline.Candidates) != 0 {
		t.Errorf("decline record = %+v, want no prompt, candidates, or response", decline)
	}
}

// TestLLMCallRecordsCandidateListAsShown covers the companion experiment that
// varies how candidates are labelled: the labels are an independent variable, so
// each call must be recoverable with the exact numbered list it saw.
func TestLLMCallRecordsCandidateListAsShown(t *testing.T) {
	fake := newFakeOpenRouter(t)
	source, verifierInstance := newLLMSource(t, fake)
	source.instructions = "hunt for double submits"
	pushLLMSnapshot(t, verifierInstance)
	tap := candidateByKind(t, mustCandidates(t, verifierInstance, verifier.LabelSourceVisibleText), verifier.ActionKindTap)
	fake.choice = tap.Index
	fake.chosenAction = tap.Description
	if _, err := source.NextAction(context.Background(), 1); err != nil {
		t.Fatalf("NextAction: %v", err)
	}

	call := lastCall(t, source)
	if len(call.Candidates) == 0 {
		t.Fatal("no candidate list recorded")
	}
	for _, candidate := range call.Candidates {
		line := fmt.Sprintf("%d. %s", candidate.Index, candidate.Description)
		if !strings.Contains(call.UserPrompt, line) {
			t.Errorf("recorded candidate %q is not a line of the prompt:\n%s", line, call.UserPrompt)
		}
		if candidate.Weight > 0 && !strings.Contains(call.UserPrompt, fmt.Sprintf("%s  (w%d)", candidate.Description, candidate.Weight)) {
			t.Errorf("recorded weight %d for %q is not the weight the prompt showed:\n%s",
				candidate.Weight, candidate.Description, call.UserPrompt)
		}
	}
	numberedLine := regexp.MustCompile(`(?m)^\d+\. `)
	if shown := len(numberedLine.FindAllString(call.UserPrompt, -1)); shown != len(call.Candidates) {
		t.Errorf("prompt showed %d numbered lines but %d candidates were recorded", shown, len(call.Candidates))
	}
	if got := candidateLabels(call.Candidates); !slices.Contains(got, tap.Label) {
		t.Errorf("recorded labels = %v, want the target label %q among them", got, tap.Label)
	}
	// The system prompt is assembled from a constant plus spec instructions, so
	// the record has to be the assembled text, not the constant.
	if call.SystemPrompt != source.systemPrompt() {
		t.Errorf("recorded system prompt = %q, want the assembled prompt", call.SystemPrompt)
	}
	if !strings.Contains(call.SystemPrompt, "hunt for double submits") {
		t.Error("recorded system prompt dropped the spec instructions")
	}
	if call.Model != "test/model" {
		t.Errorf("recorded model = %q, want test/model", call.Model)
	}
}

func candidateLabels(candidates []trace.LLMCandidate) []string {
	labels := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		labels = append(labels, candidate.Label)
	}
	return labels
}

func TestLLMCallRecordsSetupDrivenStep(t *testing.T) {
	fake := newFakeOpenRouter(t)
	source, verifierInstance := newLLMSourceWithSpec(t, fake, llmSetupFixtureSpec)
	pushLLMSnapshot(t, verifierInstance)

	action, err := source.NextAction(context.Background(), 2)
	if err != nil {
		t.Fatalf("NextAction: %v", err)
	}
	if action.On != "id:Submit" {
		t.Fatalf("action = %+v, want the setup tap on id:Submit", action)
	}
	call := lastCall(t, source)
	if call.Outcome != trace.LLMOutcomeSetupAction {
		t.Errorf("outcome = %q, want %q", call.Outcome, trace.LLMOutcomeSetupAction)
	}
	if call.UserPrompt != "" || call.RawResponse != "" || call.TotalTokens != 0 {
		t.Errorf("setup-driven step = %+v, want no model call recorded", call)
	}
	if source.lastSource != "" {
		t.Errorf("lastSource = %q, want empty: setup chose the action, not the model", source.lastSource)
	}
}

func TestLLMCallFileRecordsUsageLatencyAndScreenshot(t *testing.T) {
	fake := newFakeOpenRouter(t)
	fake.usage = llmclient.Usage{PromptTokens: 1200, CompletionTokens: 34, TotalTokens: 1234}
	fake.servedModel = "vendor/model-2026-05"
	fake.delay = 15 * time.Millisecond
	source, verifierInstance := newLLMSource(t, fake)

	directory := t.TempDir()
	writer, err := trace.NewWriter(directory)
	if err != nil {
		t.Fatal(err)
	}
	source.recorder = writer

	pushLLMSnapshotAtStep(t, verifierInstance, 4)
	tap := candidateByKind(t, mustCandidates(t, verifierInstance, verifier.LabelSourceVisibleText), verifier.ActionKindTap)
	fake.choice = tap.Index
	fake.chosenAction = tap.Description
	if _, err := source.NextAction(context.Background(), 4); err != nil {
		t.Fatalf("NextAction: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	calls := readLLMCalls(t, directory)
	if len(calls) != 1 {
		t.Fatalf("recorded %d calls, want 1", len(calls))
	}
	call := calls[0]
	if call.PromptTokens != 1200 || call.CompletionTokens != 34 || call.TotalTokens != 1234 {
		t.Errorf("tokens = %d/%d/%d, want 1200/34/1234",
			call.PromptTokens, call.CompletionTokens, call.TotalTokens)
	}
	if call.LatencyMillis < 15 {
		t.Errorf("latency = %dms, want at least the server's 15ms", call.LatencyMillis)
	}
	if call.ServedModel != "vendor/model-2026-05" {
		t.Errorf("served model = %q, want the id the provider reported", call.ServedModel)
	}
	if want := trace.ScreenshotReference(4); call.Screenshot != want {
		t.Errorf("screenshot = %q, want %q", call.Screenshot, want)
	}
	if call.Reasoning == "" || call.EchoedAction != tap.Description {
		t.Errorf("record = %+v, want the parsed reasoning and echo", call)
	}
}

// TestLLMCallScreenshotNamesObservedStep guards the one case where the image
// sent is not the current step's: the runner skips PushSnapshot on a
// transitional observation, so the model still sees the last observed screen.
func TestLLMCallScreenshotNamesObservedStep(t *testing.T) {
	fake := newFakeOpenRouter(t)
	source, verifierInstance := newLLMSource(t, fake)
	pushLLMSnapshotAtStep(t, verifierInstance, 4)
	tap := candidateByKind(t, mustCandidates(t, verifierInstance, verifier.LabelSourceVisibleText), verifier.ActionKindTap)
	fake.choice = tap.Index
	fake.chosenAction = tap.Description
	if _, err := source.NextAction(context.Background(), 6); err != nil {
		t.Fatalf("NextAction: %v", err)
	}

	call := lastCall(t, source)
	if call.Step != 6 {
		t.Errorf("record step = %d, want the current step 6", call.Step)
	}
	if want := trace.ScreenshotReference(4); call.Screenshot != want {
		t.Errorf("screenshot = %q, want %q: the image sent was step 4's", call.Screenshot, want)
	}
}

func TestDownscalePNGShrinksLongEdge(t *testing.T) {
	large := image.NewRGBA(image.Rect(0, 0, 2048, 1024))
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, large); err != nil {
		t.Fatal(err)
	}
	scaled := downscalePNG(buffer.Bytes(), 1024)
	decoded, err := png.Decode(bytes.NewReader(scaled))
	if err != nil {
		t.Fatalf("decode scaled: %v", err)
	}
	if decoded.Bounds().Dx() != 1024 {
		t.Errorf("scaled width = %d, want 1024", decoded.Bounds().Dx())
	}
	if decoded.Bounds().Dy() != 512 {
		t.Errorf("scaled height = %d, want 512", decoded.Bounds().Dy())
	}
}

func TestDownscalePNGKeepsSmallImage(t *testing.T) {
	original := tinyPNG(t)
	if got := downscalePNG(original, 1024); !bytes.Equal(got, original) {
		t.Error("a sub-maxEdge image should be returned unchanged")
	}
}

func requestHasImage(request map[string]any) bool {
	messages, ok := request["messages"].([]any)
	if !ok {
		return false
	}
	for _, message := range messages {
		parts, ok := message.(map[string]any)["content"].([]any)
		if !ok {
			continue
		}
		for _, part := range parts {
			if part.(map[string]any)["type"] == "image_url" {
				return true
			}
		}
	}
	return false
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// llmSamplerFixtureSpec drives the model policy over an authored leaf that
// samples one of three targets, which is the shape the seeded picker draws from
// and the model policy cannot.
const llmSamplerFixtureSpec = `
import { actions, from, llm, Tap, always } from "@sanderling/spec";
globalThis.properties = { ok: always(() => true) };
const targets = from(["id:Submit", "id:Name"]);
globalThis.actions = actions(() => [Tap({ on: targets.generate() })]);
globalThis.generator = llm({ model: "test/model" });
`

// TestLLMSourceRefusesAMultiItemAuthoredSampler: the step must fail the run, not
// skip. A skip would leave the model quietly fuzzing a spec whose authored
// targets it can never reach past the first, which is the comparison the seeded
// arm is measured against.
func TestLLMSourceRefusesAMultiItemAuthoredSampler(t *testing.T) {
	fake := newFakeOpenRouter(t)
	source, verifierInstance := newLLMSourceWithSpec(t, fake, llmSamplerFixtureSpec)
	pushLLMSnapshot(t, verifierInstance)

	_, err := source.NextAction(context.Background(), 1)
	if err == nil || errors.Is(err, verifier.ErrNoAction) {
		t.Fatalf("NextAction err = %v, want the run to stop on a sampler the model cannot draw", err)
	}
	if !strings.Contains(err.Error(), "targets.generate()") {
		t.Errorf("error does not name the offending leaf: %v", err)
	}
	if outcome := lastCall(t, source).Outcome; outcome != trace.LLMOutcomeCandidatesFailed {
		t.Errorf("recorded outcome = %q, want %q", outcome, trace.LLMOutcomeCandidatesFailed)
	}
}
