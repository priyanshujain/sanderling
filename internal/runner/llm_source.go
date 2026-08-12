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
	"regexp"
	"strings"
	"time"

	"github.com/priyanshujain/sanderling/internal/llmclient"
	"github.com/priyanshujain/sanderling/internal/trace"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

const (
	// llmMaxImageEdge downscales the screenshot's long edge to bound the payload
	// while keeping the UI legible.
	llmMaxImageEdge = 1024
	// llmHistorySize is how many recent actions (and the screen each led to) the
	// prompt carries as context.
	llmHistorySize = 5
)

// llmSystemPrompt frames the selection task: a short, generic bug-hunting
// instruction. Each candidate is already a concrete, correctly-labeled action
// with a weight hinting the spec's testing priority; the model reads the
// screenshot, picks ONE number, and echoes that action so a mismatch can be
// caught. The app-specific description (spec instructions) is appended.
const llmSystemPrompt = "You are exercising a UI to find bugs. Each turn you get a screenshot and a numbered list of concrete actions, each with a weight hinting how much the test author wants it exercised (higher = more). " +
	"Pick the ONE action most likely to make progress or expose a defect. Bugs often hide in repeated or rapid actions, so once a screen works, deliberately stress it (for example submitting the same form twice in a row to check it is not applied more than once) rather than only advancing. " +
	"Respond with your reasoning, the chosen number, and chosen_action copied verbatim from that line. For a typing action, also provide the text to enter."

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
	// labelSource is the channel each candidate's target is named by in the
	// numbered list. It lives here rather than on the verifier because only this
	// policy reads labels at all.
	labelSource string
	logger      *slog.Logger
	history     *actionHistory
	// recorder persists one record per step: what was sent, what came back, and
	// how the step ended. Nil only in unit tests that never select.
	recorder llmCallRecorder

	// lastSource/lastReasoning describe the most recent NextAction so the runner
	// can stamp the trace. lastSource is "llm" only when the LLM (not setup)
	// chose the action; lastReasoning is the model's rationale. lastChoice is the
	// 1-based number it picked and lastChosenAction the description it echoed, so
	// the trace shows what the model believed it was doing.
	lastSource       string
	lastReasoning    string
	lastChoice       int
	lastChosenAction string
}

// llmSelection is the outcome of one LLM selection call.
type llmSelection struct {
	action       verifier.Action
	reasoning    string
	choice       int
	chosenAction string
}

// llmCallRecorder persists one selection record per step. *trace.Writer
// implements it.
type llmCallRecorder interface {
	WriteLLMCall(call trace.LLMCall) error
}

// NextAction returns the step's action. Setup precedence is preserved by
// running the JS path first (the llm marker is inert there, so a null result
// means setup yielded nothing); the LLM selection then takes over. Every way a
// step can end lands one record keyed to stepIndex, so a step the guards threw
// away is never confused with one the picker declined.
func (s *llmSource) NextAction(ctx context.Context, stepIndex int) (verifier.Action, error) {
	s.lastSource = ""
	s.lastReasoning = ""
	s.lastChoice = 0
	s.lastChosenAction = ""
	s.history.completeLast(s.verifier.CurrentScreen())

	// Setup precedence only: the LLM replaces the seeded action root, so we run
	// setup (e.g. login) first but never the weighted picker.
	action, err := s.verifier.SetupAction()
	if err == nil {
		s.record(stepIndex, trace.LLMCall{Outcome: trace.LLMOutcomeSetupAction})
		s.history.add(describeAction(action))
		return action, nil
	}
	if !errors.Is(err, verifier.ErrNoAction) {
		s.record(stepIndex, trace.LLMCall{Outcome: trace.LLMOutcomeSetupFailed, Error: err.Error()})
		return verifier.Action{}, err
	}

	selection, call, err := s.selectViaLLM(ctx)
	s.record(stepIndex, call)
	if err != nil {
		return verifier.Action{}, err
	}
	if call.Outcome != trace.LLMOutcomeSelected {
		// Every other outcome skips the step; the next step re-observes and
		// tries again. The record says which one it was.
		return verifier.Action{}, verifier.ErrNoAction
	}
	s.lastSource = "llm"
	s.lastReasoning = selection.reasoning
	s.lastChoice = selection.choice
	s.lastChosenAction = selection.chosenAction
	s.history.add(describeAction(selection.action))
	return selection.action, nil
}

// selectViaLLM runs one multimodal call and maps the chosen number to an action.
// The returned record is complete whichever way the selection ended: its
// Outcome is trace.LLMOutcomeSelected exactly when the returned selection is
// usable.
//
// The error is the spec refusing this policy rather than a step going nowhere:
// every later step would refuse identically, so the run stops instead of
// recording two hundred skipped steps.
func (s *llmSource) selectViaLLM(ctx context.Context) (llmSelection, trace.LLMCall, error) {
	call := trace.LLMCall{Timestamp: time.Now(), Model: s.model}
	candidates, err := s.verifier.Candidates(s.labelSource)
	if err != nil {
		call.Outcome = trace.LLMOutcomeCandidatesFailed
		call.Error = err.Error()
		return llmSelection{}, call, err
	}
	if len(candidates) == 0 {
		call.Outcome = trace.LLMOutcomeNoCandidates
		return llmSelection{}, call, nil
	}
	call.Candidates = recordCandidates(candidates)

	request, screenshotReference := s.buildRequest(candidates)
	call.Screenshot = screenshotReference
	call.SystemPrompt, call.UserPrompt = promptTexts(request)

	requestStart := time.Now()
	response, err := s.client.ChatCompletion(ctx, request)
	call.LatencyMillis = time.Since(requestStart).Milliseconds()
	if err != nil {
		s.logger.Warn("llm action selection failed", "err", err)
		call.Outcome = trace.LLMOutcomeRequestFailed
		call.Error = err.Error()
		return llmSelection{}, call, nil
	}
	call.ServedModel = response.Model
	call.PromptTokens = response.Usage.PromptTokens
	call.CompletionTokens = response.Usage.CompletionTokens
	call.TotalTokens = response.Usage.TotalTokens
	if len(response.Choices) == 0 {
		s.logger.Warn("llm returned no choices")
		call.Outcome = trace.LLMOutcomeNoChoices
		return llmSelection{}, call, nil
	}
	call.RawResponse = response.Choices[0].Message.Content

	output, err := parseChoice(call.RawResponse)
	if err != nil {
		s.logger.Warn("llm output unusable", "err", err)
		call.Outcome = trace.LLMOutcomeUnparsableResponse
		call.Error = err.Error()
		return llmSelection{}, call, nil
	}
	call.Choice = output.Choice
	call.EchoedAction = output.ChosenAction
	call.Reasoning = output.Reasoning
	// choice is 1-based into the numbered list.
	if output.Choice < 1 || output.Choice > len(candidates) {
		s.logger.Warn("llm choice out of range", "choice", output.Choice, "candidates", len(candidates))
		call.Outcome = trace.LLMOutcomeChoiceOutOfRange
		return llmSelection{}, call, nil
	}
	candidate := candidates[output.Choice-1]
	// Strict skip: the echoed action must match the numbered entry, so a model
	// that reasoned about one target but named a number for another cannot act.
	// Models copy the whole rendered line including its trailing "(w34)" weight
	// annotation, so strip that before comparing to the (weight-free) description.
	if stripWeightSuffix(output.ChosenAction) != candidate.Description {
		s.logger.Warn("llm chosen_action mismatch; skipping",
			"choice", output.Choice, "echoed", output.ChosenAction, "candidate", candidate.Description)
		call.Outcome = trace.LLMOutcomeEchoMismatch
		return llmSelection{}, call, nil
	}
	action, err := s.actionForCandidate(candidate, output.Text)
	if err != nil {
		s.logger.Warn("building action from candidate failed", "choice", output.Choice, "err", err)
		call.Outcome = trace.LLMOutcomeActionBuildFailed
		call.Error = err.Error()
		return llmSelection{}, call, nil
	}
	call.Outcome = trace.LLMOutcomeSelected
	return llmSelection{
		action:       action,
		reasoning:    output.Reasoning,
		choice:       output.Choice,
		chosenAction: candidate.Description,
	}, call, nil
}

// record stamps the step this selection belongs to and appends the record. A
// write failure must not kill the run, but it does mean the step is
// unattributable, so it is logged.
func (s *llmSource) record(stepIndex int, call trace.LLMCall) {
	if s.recorder == nil {
		return
	}
	call.Step = stepIndex
	if call.Timestamp.IsZero() {
		call.Timestamp = time.Now()
	}
	if err := s.recorder.WriteLLMCall(call); err != nil {
		s.logger.Warn("llm call record failed", "step", stepIndex, "err", err)
	}
}

// recordCandidates snapshots the numbered list as the prompt rendered it, so a
// campaign that varies candidate labelling can recover the labels each call saw.
func recordCandidates(candidates []verifier.ActionCandidate) []trace.LLMCandidate {
	recorded := make([]trace.LLMCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		entry := trace.LLMCandidate{
			Index:       candidate.Index,
			Kind:        string(candidate.Kind),
			Description: candidate.Description,
			Label:       candidate.Label,
		}
		if candidate.Weighted {
			entry.Weight = candidate.Weight
		}
		recorded = append(recorded, entry)
	}
	return recorded
}

// promptTexts reads the text parts back off the built request, so the record is
// what went on the wire rather than a second rendering of the same templates.
func promptTexts(request llmclient.Request) (systemPrompt, userPrompt string) {
	for _, message := range request.Messages {
		var parts []string
		for _, part := range message.Content {
			if part.Type == "text" {
				parts = append(parts, part.Text)
			}
		}
		text := strings.Join(parts, "\n")
		switch message.Role {
		case "system":
			systemPrompt = text
		case "user":
			userPrompt = text
		}
	}
	return systemPrompt, userPrompt
}

// actionForCandidate turns a chosen candidate into the executable action. The
// candidate already carries a ready action; only builtin typing needs the
// model's value spliced in (authored InputText keeps its sampled value, and any
// other kind runs verbatim).
func (s *llmSource) actionForCandidate(candidate verifier.ActionCandidate, text string) (verifier.Action, error) {
	action := candidate.Action
	if candidate.Kind == verifier.ActionKindInputText && candidate.LLMText {
		if strings.TrimSpace(text) == "" {
			// The model omitted a value; fall back to the shared corpus sampler
			// so typing still exercises an edge-case string.
			sampled, err := s.verifier.SampleInput()
			if err != nil {
				return verifier.Action{}, err
			}
			text = sampled
		}
		action.Text = text
	}
	return action, nil
}

// buildRequest assembles the one-shot multimodal request: a system frame, the
// numbered candidate list plus recent-action memory, and the downscaled
// screenshot. The strict json_schema response format pins the ranked output.
//
// The second return is the run-relative path of the screenshot attached, empty
// when none was, so a record can never claim an image the call did not carry.
// It names the step the image was captured at, which is the last observed step
// rather than the current one whenever an observation was skipped.
func (s *llmSource) buildRequest(candidates []verifier.ActionCandidate) (llmclient.Request, string) {
	userParts := []llmclient.ContentPart{llmclient.TextPart(s.userPrompt(candidates))}
	screenshotReference := ""
	if screenshot := s.verifier.Screenshot(); len(screenshot) > 0 {
		if dataURL, ok := screenshotDataURL(screenshot, llmMaxImageEdge); ok {
			userParts = append(userParts, llmclient.ImagePart(dataURL))
			if step := s.verifier.SnapshotStep(); step > 0 {
				screenshotReference = trace.ScreenshotReference(step)
			}
		}
	}
	request := llmclient.Request{
		Model: s.model,
		Messages: []llmclient.Message{
			{Role: "system", Content: []llmclient.ContentPart{llmclient.TextPart(s.systemPrompt())}},
			{Role: "user", Content: userParts},
		},
		ResponseFormat: choiceResponseFormat(len(candidates)),
	}
	return request, screenshotReference
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

// userPrompt renders the numbered candidate list (with weights) and the
// recent-action memory.
func (s *llmSource) userPrompt(candidates []verifier.ActionCandidate) string {
	var builder strings.Builder
	builder.WriteString("Actions available on the current screen:\n")
	for _, candidate := range candidates {
		fmt.Fprintf(&builder, "%d. %s", candidate.Index, candidate.Description)
		if candidate.Weighted {
			fmt.Fprintf(&builder, "  (w%d)", candidate.Weight)
		}
		builder.WriteByte('\n')
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
	builder.WriteString("\nPick one action by its number.")
	return builder.String()
}

// choiceResponseFormat is the strict structured-output schema. Field order is
// pinned via raw JSON with reasoning FIRST, so the model reasons before it
// commits to a number (a materially better ordering than answer-first). text is
// required by strict mode but empty for non-typing actions.
func choiceResponseFormat(candidateCount int) *llmclient.ResponseFormat {
	schema := fmt.Sprintf(`{
  "type": "object",
  "properties": {
    "reasoning": {"type": "string", "description": "One short sentence on what you are trying to do and why this action."},
    "choice": {"type": "integer", "minimum": 1, "maximum": %d, "description": "The number of the chosen action."},
    "chosen_action": {"type": "string", "description": "The chosen action's text, copied verbatim from its numbered line."},
    "text": {"type": "string", "description": "For a typing action, the text to enter; otherwise an empty string."}
  },
  "required": ["reasoning", "choice", "chosen_action", "text"],
  "additionalProperties": false
}`, candidateCount)
	return &llmclient.ResponseFormat{
		Type: "json_schema",
		JSONSchema: llmclient.JSONSchema{
			Name:   "action_choice",
			Strict: true,
			Schema: json.RawMessage(schema),
		},
	}
}

// choiceOutput is the model's structured response, reasoning first.
type choiceOutput struct {
	Reasoning    string `json:"reasoning"`
	Choice       int    `json:"choice"`
	ChosenAction string `json:"chosen_action"`
	Text         string `json:"text"`
}

// weightSuffix matches the trailing "  (w34)" annotation appended to each
// numbered line, which models copy verbatim into chosen_action.
var weightSuffix = regexp.MustCompile(`\s*\(w\d+\)$`)

// stripWeightSuffix trims surrounding whitespace and a trailing weight
// annotation from the model's echoed action so it can be compared to the
// weight-free candidate description.
func stripWeightSuffix(echo string) string {
	return strings.TrimSpace(weightSuffix.ReplaceAllString(strings.TrimSpace(echo), ""))
}

// parseChoice decodes the model's JSON content into the structured choice.
func parseChoice(content string) (choiceOutput, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return choiceOutput{}, errors.New("empty content")
	}
	var out choiceOutput
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return choiceOutput{}, err
	}
	if out.Choice == 0 {
		return choiceOutput{}, errors.New("no choice")
	}
	return out, nil
}

// describeAction renders a short action summary for the recent-action memory.
func describeAction(action verifier.Action) string {
	switch action.Kind {
	case verifier.ActionKindInputText:
		return fmt.Sprintf("InputText %s = %q", actionTarget(action), action.Text)
	case verifier.ActionKindScroll:
		// A builtin gesture carries endpoints rather than a selector, so name the
		// container by where the drag starts; that is what tells two scrollable
		// regions apart in the recent-action memory.
		target := action.On
		if target == "" {
			target = fmt.Sprintf("(%d,%d)", action.FromX, action.FromY)
		}
		return fmt.Sprintf("Scroll %s %s", action.Direction, target)
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
	traceAction.LLMChoice = llm.lastChoice
	traceAction.LLMChosenAction = llm.lastChosenAction
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
