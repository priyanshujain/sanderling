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
	"strings"
	"testing"

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
import { llm, always } from "@sanderling/spec";
globalThis.properties = { ok: always(() => true) };
globalThis.actions = llm({ model: "test/model" });
`

const llmTreeJSON = `{
  "attributes": {"bounds": "[0,0,400,800]"},
  "children": [
    {"attributes": {"resource-id": "Submit", "text": "Submit", "bounds": "[0,0,400,100]"}, "clickable": true, "enabled": true, "children": []},
    {"attributes": {"resource-id": "Name", "class": "EditText", "bounds": "[0,100,400,200]"}, "enabled": true, "children": []}
  ]
}`

func TestActionFromCandidateMapping(t *testing.T) {
	sampler := func() (string, error) { return "typed-value", nil }
	cases := []struct {
		name   string
		input  verifier.ActionCandidate
		assert func(t *testing.T, action verifier.Action)
	}{
		{
			name:  "tap keeps coordinates and selector",
			input: verifier.ActionCandidate{Kind: verifier.ActionKindTap, X: 10, Y: 20, Selector: "id:Submit"},
			assert: func(t *testing.T, action verifier.Action) {
				if action.Kind != verifier.ActionKindTap || action.X != 10 || action.Y != 20 || action.On != "id:Submit" {
					t.Errorf("tap = %+v", action)
				}
			},
		},
		{
			name:  "typing fills text from the sampler",
			input: verifier.ActionCandidate{Kind: verifier.ActionKindInputText, X: 5, Y: 6, Selector: "id:Name"},
			assert: func(t *testing.T, action verifier.Action) {
				if action.Kind != verifier.ActionKindInputText || action.Text != "typed-value" || action.On != "id:Name" {
					t.Errorf("inputText = %+v", action)
				}
			},
		},
		{
			name:  "scroll defaults down with derived endpoints",
			input: verifier.ActionCandidate{Kind: verifier.ActionKindScroll, X: 50, Y: 50, Selector: "id:List"},
			assert: func(t *testing.T, action verifier.Action) {
				if action.Direction != "down" {
					t.Errorf("scroll direction = %q, want down", action.Direction)
				}
				if action.X != 0 || action.Y != 0 || action.FromX != 0 || action.ToY != 0 {
					t.Errorf("scroll endpoints should be left zero for runner derivation, got %+v", action)
				}
			},
		},
		{
			name:  "swipe builds a vertical gesture sized off height",
			input: verifier.ActionCandidate{Kind: verifier.ActionKindSwipe, X: 100, Y: 1000, Height: 1000},
			assert: func(t *testing.T, action verifier.Action) {
				if action.FromX != 100 || action.FromY != 1000 || action.ToX != 100 {
					t.Errorf("swipe origin = %+v", action)
				}
				if action.ToY != 1000-400 {
					t.Errorf("swipe ToY = %d, want 600", action.ToY)
				}
			},
		},
		{
			name:  "swipe magnitude floors at the minimum",
			input: verifier.ActionCandidate{Kind: verifier.ActionKindSwipe, X: 10, Y: 500, Height: 100},
			assert: func(t *testing.T, action verifier.Action) {
				// 100*4/10 = 40 < 200, so the floor applies.
				if action.ToY != 500-swipeMinMagnitude {
					t.Errorf("swipe ToY = %d, want %d", action.ToY, 500-swipeMinMagnitude)
				}
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			action, err := actionFromCandidate(testCase.input, sampler)
			if err != nil {
				t.Fatalf("actionFromCandidate: %v", err)
			}
			testCase.assert(t, action)
		})
	}
}

func TestActionFromCandidateSamplerError(t *testing.T) {
	failing := func() (string, error) { return "", errors.New("no sampler") }
	_, err := actionFromCandidate(verifier.ActionCandidate{Kind: verifier.ActionKindInputText}, failing)
	if err == nil {
		t.Fatal("expected sampler error to propagate")
	}
}

func TestParseRanked(t *testing.T) {
	ranked, reasoning, err := parseRanked(`{"reasoning":"go home","ranked":[3,1,0]}`)
	if err != nil {
		t.Fatalf("parseRanked: %v", err)
	}
	if reasoning != "go home" || !slices.Equal(ranked, []int{3, 1, 0}) {
		t.Errorf("parseRanked = %v %q", ranked, reasoning)
	}

	if _, _, err := parseRanked(""); err == nil {
		t.Error("expected error for empty content")
	}
	if _, _, err := parseRanked(`{"reasoning":"x","ranked":[]}`); err == nil {
		t.Error("expected error for empty ranked list")
	}
	if _, _, err := parseRanked(`not json`); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

// fakeOpenRouter is a configurable in-process OpenRouter server. Set ranked /
// reasoning before each call; it echoes them as a json_schema content body.
type fakeOpenRouter struct {
	server      *httptest.Server
	ranked      []int
	reasoning   string
	lastRequest map[string]any
}

func newFakeOpenRouter(t *testing.T) *fakeOpenRouter {
	t.Helper()
	fake := &fakeOpenRouter{reasoning: "because"}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &fake.lastRequest)
		content, _ := json.Marshal(map[string]any{"reasoning": fake.reasoning, "ranked": fake.ranked})
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

func candidateIndex(candidates []verifier.ActionCandidate, kind verifier.ActionKind) int {
	for _, candidate := range candidates {
		if candidate.Kind == kind {
			return candidate.Index
		}
	}
	return -1
}

func TestLLMSourceDrivesExecutedActions(t *testing.T) {
	fake := newFakeOpenRouter(t)
	source, verifierInstance := newLLMSource(t, fake)
	pushLLMSnapshot(t, verifierInstance)
	candidates := verifierInstance.AllCandidates()

	// Step 1: the model ranks the Tap on Submit first.
	fake.ranked = []int{candidateIndex(candidates, verifier.ActionKindTap)}
	fake.reasoning = "tap submit"
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

	// Step 2: the model ranks the InputText on Name. Its text must come from the
	// shared corpus sampler, not the model.
	pushLLMSnapshot(t, verifierInstance)
	fake.ranked = []int{candidateIndex(candidates, verifier.ActionKindInputText)}
	fake.reasoning = "type a name"
	action, err = source.NextAction(context.Background())
	if err != nil {
		t.Fatalf("NextAction: %v", err)
	}
	if action.Kind != verifier.ActionKindInputText || action.On != "id:Name" {
		t.Errorf("step 2 action = %+v, want InputText on id:Name", action)
	}
	if !slices.Contains(llmInputCorpus, action.Text) {
		t.Errorf("InputText text %q was not drawn from the corpus", action.Text)
	}

	// The trace records source=llm and the reasoning.
	traceAction := traceActionFor(action, nil)
	stampActionSource(traceAction, source)
	if traceAction.Source != "llm" || traceAction.LLMReasoning != "type a name" {
		t.Errorf("trace action = %+v, want source=llm reasoning=type a name", traceAction)
	}
}

func TestLLMSourceSkipsOnInvalidIndex(t *testing.T) {
	fake := newFakeOpenRouter(t)
	source, verifierInstance := newLLMSource(t, fake)
	pushLLMSnapshot(t, verifierInstance)

	fake.ranked = []int{9999}
	_, err := source.NextAction(context.Background())
	if !errors.Is(err, verifier.ErrNoAction) {
		t.Fatalf("NextAction err = %v, want ErrNoAction for an out-of-range index", err)
	}
	if source.lastSource != "" {
		t.Errorf("lastSource = %q, want empty after a skipped step", source.lastSource)
	}
}

func TestLLMSourceFirstValidIndexWins(t *testing.T) {
	fake := newFakeOpenRouter(t)
	source, verifierInstance := newLLMSource(t, fake)
	pushLLMSnapshot(t, verifierInstance)
	candidates := verifierInstance.AllCandidates()

	tapIndex := candidateIndex(candidates, verifier.ActionKindTap)
	// First index is invalid; the second is the valid Tap candidate.
	fake.ranked = []int{-1, tapIndex}
	action, err := source.NextAction(context.Background())
	if err != nil {
		t.Fatalf("NextAction: %v", err)
	}
	if action.Kind != verifier.ActionKindTap {
		t.Errorf("action = %+v, want Tap from the first valid index", action)
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

	fake.ranked = []int{0}
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
