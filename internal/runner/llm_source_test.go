package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/priyanshujain/sanderling/internal/driver"
	mockdriver "github.com/priyanshujain/sanderling/internal/driver/mock"
	"github.com/priyanshujain/sanderling/internal/hierarchy"
	"github.com/priyanshujain/sanderling/internal/llmclient"
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
			Choices: []llmclient.Choice{{Message: llmclient.ResponseMessage{Content: string(content)}}},
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(response)
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func newLLMSource(t *testing.T, fake *fakeOpenRouter) (*llmSource, *verifier.Verifier) {
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
	if err := verifierInstance.Load(bundleSpec(t, llmFixtureSpec)); err != nil {
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
	}
	return source, verifierInstance
}

func pushLLMSnapshot(t *testing.T, v *verifier.Verifier) {
	t.Helper()
	tree, err := hierarchy.Parse(llmTreeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.PushSnapshot(verifier.SnapshotInput{Tree: tree, ScreenshotPNG: tinyPNG(t)}); err != nil {
		t.Fatalf("PushSnapshot: %v", err)
	}
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
	candidates := verifierInstance.Candidates()

	// Step 1: the model picks the Tap on Submit by its number, echoing its
	// description.
	tap := candidateByKind(t, candidates, verifier.ActionKindTap)
	fake.choice = tap.Index
	fake.chosenAction = tap.Description
	fake.reasoning = "tap submit"
	fake.text = ""
	action, err := source.NextAction(context.Background())
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
	action, err = source.NextAction(context.Background())
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
	_, err := source.NextAction(context.Background())
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
	candidates := verifierInstance.Candidates()

	tap := candidateByKind(t, candidates, verifier.ActionKindTap)
	fake.choice = tap.Index
	fake.chosenAction = tap.Description + "  (w" + strconv.Itoa(tap.Weight) + ")"
	action, err := source.NextAction(context.Background())
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
	candidates := verifierInstance.Candidates()

	// A valid number, but the echoed action disagrees with that numbered entry:
	// the model reasoned about one control and picked another's number.
	tap := candidateByKind(t, candidates, verifier.ActionKindTap)
	fake.choice = tap.Index
	fake.chosenAction = "Tap \"Something Else\""
	_, err := source.NextAction(context.Background())
	if !errors.Is(err, verifier.ErrNoAction) {
		t.Fatalf("NextAction err = %v, want ErrNoAction on chosen_action mismatch", err)
	}
	if source.lastSource != "" {
		t.Errorf("lastSource = %q, want empty after a strict skip", source.lastSource)
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
	_, err := source.NextAction(context.Background())
	if !errors.Is(err, verifier.ErrNoAction) {
		t.Fatalf("NextAction err = %v, want ErrNoAction on HTTP failure", err)
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
