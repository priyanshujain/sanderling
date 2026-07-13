package runner

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"strings"

	"github.com/priyanshujain/sanderling/internal/llmclient"
	"github.com/priyanshujain/sanderling/internal/trace"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

const (
	// llmMaxImageEdge downscales the screenshot's long edge to bound the payload
	// while keeping the UI legible.
	llmMaxImageEdge = 1024
	// llmHistorySize is how many recent actions (and the screen each led to) the
	// prompt carries to discourage loops.
	llmHistorySize = 5
	// llmMaxRanked caps the ranked-index list the model returns.
	llmMaxRanked = 5
	// swipeMinMagnitude is the floor for an LLM-chosen swipe distance, matching
	// the seeded swipe builder's minimum.
	swipeMinMagnitude = 200
)

// llmSystemPrompt frames the selection task. The model only ranks the numbered
// candidates the system already enumerated; it never invents actions. The kind
// semantics matter: every visible element doubles as a Swipe origin, so a
// control whose only candidate is Swipe is NOT pressable — without the
// explanation models pick `Swipe "Submit"` intending to press Submit and loop
// forever on a disabled button.
const llmSystemPrompt = "You are exploring this app to surface bugs. Choose the most useful next action from the numbered candidates. " +
	"Candidate kinds: Tap/DoubleTap/LongPress press a control; InputText types into a field; Scroll and Swipe only pan the view — they never press the element they are labeled with. " +
	"A button that has no Tap candidate is disabled; satisfy its preconditions first (usually InputText into a field) instead of swiping it. " +
	"Avoid repeating recent actions; prefer progress into new screens. Return only your ranked choices."

// llmSource selects each step's action with an OpenAI-compatible vision model
// instead of the seeded random pick. It replaces ONLY the pick: the candidate list, the input
// values, and action execution are all reused unchanged. The spec's JS setup
// still runs first each tick (setup precedence), and the LLM drives once setup
// yields nothing.
type llmSource struct {
	verifier *verifier.Verifier
	client   *llmclient.Client
	model    string
	// instructions is optional spec-level guidance appended to the system prompt
	// to steer the model's bug-hunting (empty when unset).
	instructions string
	logger       *slog.Logger
	history      *actionHistory

	// lastSource/lastReasoning describe the most recent NextAction so the runner
	// can stamp the trace. lastSource is "llm" only when the LLM (not setup)
	// chose the action; lastReasoning is the model's rationale. lastRanked is
	// the model's full ranked list and lastChosenRank the 1-based position in it
	// that won (1 = top pick), so the trace can reconcile reasoning with action.
	lastSource     string
	lastReasoning  string
	lastRanked     []int
	lastChosenRank int
}

// llmSelection is the outcome of one LLM selection call.
type llmSelection struct {
	action     verifier.Action
	reasoning  string
	ranked     []int
	chosenRank int // 1-based position in ranked that produced action
}

// NextAction returns the step's action. Setup precedence is preserved by
// running the JS path first (the llm marker is inert there, so a null result
// means setup yielded nothing); the LLM selection then takes over.
func (s *llmSource) NextAction(ctx context.Context) (verifier.Action, error) {
	s.lastSource = ""
	s.lastReasoning = ""
	s.lastRanked = nil
	s.lastChosenRank = 0
	s.history.completeLast(s.verifier.CurrentScreen())

	action, err := s.verifier.NextAction()
	if err == nil {
		s.history.add(describeAction(action))
		return action, nil
	}
	if !errors.Is(err, verifier.ErrNoAction) {
		return verifier.Action{}, err
	}

	selection, ok := s.selectViaLLM(ctx)
	if !ok {
		// Any failure (HTTP error, unusable output, no valid index) skips the
		// step; the next step re-observes and tries again. No backend mixing.
		return verifier.Action{}, verifier.ErrNoAction
	}
	s.lastSource = "llm"
	s.lastReasoning = selection.reasoning
	s.lastRanked = selection.ranked
	s.lastChosenRank = selection.chosenRank
	s.history.add(describeAction(selection.action))
	return selection.action, nil
}

// selectViaLLM runs one multimodal call and maps the first valid ranked index
// to an action. It returns ok=false on any error/empty/invalid output, logging
// the cause; the caller turns that into a skipped step.
func (s *llmSource) selectViaLLM(ctx context.Context) (llmSelection, bool) {
	candidates := s.verifier.AllCandidates()
	if len(candidates) == 0 {
		return llmSelection{}, false
	}

	response, err := s.client.ChatCompletion(ctx, s.buildRequest(candidates))
	if err != nil {
		s.logger.Warn("llm action selection failed", "err", err)
		return llmSelection{}, false
	}
	if len(response.Choices) == 0 {
		s.logger.Warn("llm returned no choices")
		return llmSelection{}, false
	}

	ranked, reasoning, err := parseRanked(response.Choices[0].Message.Content)
	if err != nil {
		s.logger.Warn("llm output unusable", "err", err)
		return llmSelection{}, false
	}
	for position, index := range ranked {
		if index < 0 || index >= len(candidates) {
			continue
		}
		action, err := actionFromCandidate(candidates[index], s.verifier.SampleInput)
		if err != nil {
			s.logger.Warn("building action from candidate failed", "index", index, "err", err)
			continue
		}
		return llmSelection{action: action, reasoning: reasoning, ranked: ranked, chosenRank: position + 1}, true
	}
	s.logger.Warn("llm returned no valid candidate index", "ranked", ranked, "candidates", len(candidates))
	return llmSelection{}, false
}

// buildRequest assembles the one-shot multimodal request: a system frame, the
// numbered candidate list plus recent-action memory, and the downscaled
// screenshot. The strict json_schema response format pins the ranked output.
func (s *llmSource) buildRequest(candidates []verifier.ActionCandidate) llmclient.Request {
	userParts := []llmclient.ContentPart{llmclient.TextPart(s.userPrompt(candidates))}
	if screenshot := s.verifier.Screenshot(); len(screenshot) > 0 {
		if dataURL, ok := screenshotDataURL(screenshot, llmMaxImageEdge); ok {
			userParts = append(userParts, llmclient.ImagePart(dataURL))
		}
	}
	return llmclient.Request{
		Model: s.model,
		Messages: []llmclient.Message{
			{Role: "system", Content: []llmclient.ContentPart{llmclient.TextPart(s.systemPrompt())}},
			{Role: "user", Content: userParts},
		},
		ResponseFormat: rankedResponseFormat(),
	}
}

// systemPrompt is the base framing plus any spec-level instructions, appended as
// extra guidance so a spec can steer the model's bug-hunting without losing the
// candidate-kind semantics the base prompt establishes.
func (s *llmSource) systemPrompt() string {
	if strings.TrimSpace(s.instructions) == "" {
		return llmSystemPrompt
	}
	return llmSystemPrompt + "\n\n" + s.instructions
}

// userPrompt renders the numbered candidate list and the recent-action memory.
func (s *llmSource) userPrompt(candidates []verifier.ActionCandidate) string {
	var builder strings.Builder
	builder.WriteString("Candidate actions on the current screen:\n")
	for _, candidate := range candidates {
		fmt.Fprintf(&builder, "#%d %s %q\n", candidate.Index, candidate.Kind, candidate.Label)
	}
	if recent := s.history.recent(); len(recent) > 0 {
		builder.WriteString("\nYour recent actions (oldest first) and the screen each led to:\n")
		for _, entry := range recent {
			screen := entry.screen
			if screen == "" {
				screen = "(current screen)"
			}
			fmt.Fprintf(&builder, "- %s -> %s\n", entry.action, screen)
		}
	}
	builder.WriteString("\nReturn your ranked candidate indices, most useful first.")
	return builder.String()
}

// rankedResponseFormat is the strict structured-output schema: a short
// reasoning string and a ranked list of candidate indices.
func rankedResponseFormat() *llmclient.ResponseFormat {
	return &llmclient.ResponseFormat{
		Type: "json_schema",
		JSONSchema: llmclient.JSONSchema{
			Name:   "ranked_actions",
			Strict: true,
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"reasoning": map[string]any{
						"type":        "string",
						"description": "One short sentence on why the top choice is most useful.",
					},
					"ranked": map[string]any{
						"type":     "array",
						"items":    map[string]any{"type": "integer"},
						"minItems": 1,
						"maxItems": llmMaxRanked,
					},
				},
				"required":             []string{"reasoning", "ranked"},
				"additionalProperties": false,
			},
		},
	}
}

// rankedOutput is the model's structured response.
type rankedOutput struct {
	Reasoning string `json:"reasoning"`
	Ranked    []int  `json:"ranked"`
}

// parseRanked decodes the model's JSON content into ranked indices + reasoning.
func parseRanked(content string) ([]int, string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, "", errors.New("empty content")
	}
	var out rankedOutput
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return nil, "", err
	}
	if len(out.Ranked) == 0 {
		return nil, "", errors.New("no ranked indices")
	}
	return out.Ranked, out.Reasoning, nil
}

// actionFromCandidate maps a chosen candidate to a concrete action, reusing the
// corpus sampler for InputText text and the seeded gesture geometry for
// swipe/scroll. sampleInput is verifier.SampleInput, injected for testability.
func actionFromCandidate(candidate verifier.ActionCandidate, sampleInput func() (string, error)) (verifier.Action, error) {
	action := verifier.Action{Kind: candidate.Kind, On: candidate.Selector, X: candidate.X, Y: candidate.Y}
	switch candidate.Kind {
	case verifier.ActionKindInputText:
		text, err := sampleInput()
		if err != nil {
			return verifier.Action{}, err
		}
		action.Text = text
	case verifier.ActionKindScroll:
		action.Direction = "down"
		// Leave endpoints zero so the runner derives the gesture from the target
		// bounds (scrollEndpoints), exactly as for an authored Scroll.
		action.X, action.Y = 0, 0
	case verifier.ActionKindSwipe:
		// A vertical drag upward from the center reveals lower content, sized off
		// the element height like the seeded swipe builder.
		magnitude := max(swipeMinMagnitude, candidate.Height*4/10)
		action.FromX, action.FromY = candidate.X, candidate.Y
		action.ToX = candidate.X
		action.ToY = max(0, candidate.Y-magnitude)
		action.X, action.Y = 0, 0
	}
	return action, nil
}

// describeAction renders a short action summary for the recent-action memory.
func describeAction(action verifier.Action) string {
	switch action.Kind {
	case verifier.ActionKindInputText:
		return fmt.Sprintf("InputText %s = %q", actionTarget(action), action.Text)
	case verifier.ActionKindScroll:
		return fmt.Sprintf("Scroll %s %s", action.Direction, action.On)
	case verifier.ActionKindSwipe:
		// Coordinates make a repeated identical swipe recognizable in the
		// prompt's recent-action memory.
		return fmt.Sprintf("Swipe from (%d,%d)", action.FromX, action.FromY)
	case verifier.ActionKindPressKey:
		return "PressKey " + action.Key
	case verifier.ActionKindWait:
		return "Wait"
	default:
		return fmt.Sprintf("%s %s", action.Kind, actionTarget(action))
	}
}

func actionTarget(action verifier.Action) string {
	if action.On != "" {
		return action.On
	}
	return fmt.Sprintf("(%d,%d)", action.X, action.Y)
}

// historyEntry records one performed action and the screen it led to (filled on
// the following step, once that screen is observed).
type historyEntry struct {
	action string
	screen string
}

// actionHistory is a bounded ring of recent actions for the prompt.
type actionHistory struct {
	entries []historyEntry
	size    int
}

func newActionHistory(size int) *actionHistory {
	return &actionHistory{size: size}
}

// completeLast fills the most recent action's led-to screen with the
// just-observed screen, if it was still pending.
func (h *actionHistory) completeLast(screen string) {
	if n := len(h.entries); n > 0 && h.entries[n-1].screen == "" {
		h.entries[n-1].screen = screen
	}
}

// add appends an action (its led-to screen pending) and trims to size.
func (h *actionHistory) add(action string) {
	h.entries = append(h.entries, historyEntry{action: action})
	if len(h.entries) > h.size {
		h.entries = h.entries[len(h.entries)-h.size:]
	}
}

func (h *actionHistory) recent() []historyEntry {
	return h.entries
}

// stampActionSource records the backend that chose an action on the trace.
// Only an LLM-selected action (not a setup action the JS path produced) carries
// source="llm" and the model's reasoning.
func stampActionSource(traceAction *trace.Action, source ActionSource) {
	if traceAction == nil {
		return
	}
	llm, ok := source.(*llmSource)
	if !ok || llm.lastSource == "" {
		return
	}
	traceAction.Source = llm.lastSource
	traceAction.LLMReasoning = llm.lastReasoning
	traceAction.LLMRanked = llm.lastRanked
	traceAction.LLMChosenRank = llm.lastChosenRank
}

// screenshotDataURL downscales the PNG and encodes it as a data URL for the
// image content part.
func screenshotDataURL(pngBytes []byte, maxEdge int) (string, bool) {
	scaled := downscalePNG(pngBytes, maxEdge)
	if len(scaled) == 0 {
		return "", false
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(scaled), true
}

// downscalePNG shrinks the image so its long edge is at most maxEdge, returning
// the original bytes when it is already small enough and nil on decode failure.
func downscalePNG(pngBytes []byte, maxEdge int) []byte {
	source, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil
	}
	longEdge := max(width, height)
	if longEdge <= maxEdge {
		return pngBytes
	}
	scale := float64(maxEdge) / float64(longEdge)
	newWidth := max(1, int(float64(width)*scale))
	newHeight := max(1, int(float64(height)*scale))

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, boxDownscale(source, newWidth, newHeight)); err != nil {
		return nil
	}
	return buffer.Bytes()
}

// boxDownscale averages each destination pixel over its source box, a cheap
// dependency-free downscale that keeps text legible enough for the model.
func boxDownscale(source image.Image, newWidth, newHeight int) image.Image {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	dest := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	for dy := range newHeight {
		sy0 := dy * height / newHeight
		sy1 := max((dy+1)*height/newHeight, sy0+1)
		for dx := range newWidth {
			sx0 := dx * width / newWidth
			sx1 := max((dx+1)*width/newWidth, sx0+1)
			var r, g, b, a, count uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					pr, pg, pb, pa := source.At(bounds.Min.X+sx, bounds.Min.Y+sy).RGBA()
					r += uint64(pr)
					g += uint64(pg)
					b += uint64(pb)
					a += uint64(pa)
					count++
				}
			}
			count = max(1, count)
			dest.Set(dx, dy, color.RGBA64{
				R: uint16(r / count),
				G: uint16(g / count),
				B: uint16(b / count),
				A: uint16(a / count),
			})
		}
	}
	return dest
}
