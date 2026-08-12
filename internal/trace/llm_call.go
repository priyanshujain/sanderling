package trace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// LLMCallFileName is the run-directory file model-call records are appended to,
// one JSON object per line, in step order.
//
// These records live beside trace.jsonl rather than inside it because the two
// have different readers. Every trace line already carries a full accessibility
// hierarchy, and both the replay server and the campaign summarizer scan every
// line of it; folding prompts, candidate lists and raw responses in would grow
// the lines those readers parse by several KB each for data neither one reads.
// The join is by Step, which is the step field of the trace line whose
// next_action the call produced.
const LLMCallFileName = "llm-calls.jsonl"

// Outcomes of one step's action selection. Exactly one is recorded per step of
// a model-driven run, so a step the guard threw away is never confused with one
// where the picker legitimately had nothing to do.
const (
	// LLMOutcomeSelected: the model picked a candidate and the action ran.
	LLMOutcomeSelected = "selected"
	// LLMOutcomeSetupAction: the spec's setup generator drove the step, so the
	// model was not consulted.
	LLMOutcomeSetupAction = "setup_action"
	// LLMOutcomeSetupFailed: the setup generator itself errored, which aborts
	// the run.
	LLMOutcomeSetupFailed = "setup_failed"
	// LLMOutcomeNoCandidates: the action tree yielded nothing on this screen, so
	// no call was made.
	LLMOutcomeNoCandidates = "no_candidates"
	// LLMOutcomeRequestFailed: the provider call failed (transport, timeout,
	// non-2xx).
	LLMOutcomeRequestFailed = "request_failed"
	// LLMOutcomeNoChoices: a 2xx response carrying an empty choices array.
	LLMOutcomeNoChoices = "no_choices"
	// LLMOutcomeUnparsableResponse: the content was empty, not JSON, or carried
	// no choice.
	LLMOutcomeUnparsableResponse = "unparsable_response"
	// LLMOutcomeChoiceOutOfRange: the number picked is not in 1..len(candidates).
	LLMOutcomeChoiceOutOfRange = "choice_out_of_range"
	// LLMOutcomeEchoMismatch: the echoed chosen_action disagrees with the
	// numbered entry, so the pick is discarded.
	LLMOutcomeEchoMismatch = "echo_mismatch"
	// LLMOutcomeActionBuildFailed: the chosen candidate could not be turned into
	// an executable action (e.g. the input sampler was unavailable).
	LLMOutcomeActionBuildFailed = "action_build_failed"
)

// LLMCall is one step's action-selection record: what was sent, what came back,
// what it cost, and how the step ended.
type LLMCall struct {
	// Step joins this record to the trace line of the same step index.
	Step      int       `json:"step"`
	Timestamp time.Time `json:"timestamp"`
	Outcome   string    `json:"outcome"`
	// Model is the requested model id; ServedModel is the one the provider
	// reported serving, which a router may vary per call.
	Model       string `json:"model,omitempty"`
	ServedModel string `json:"served_model,omitempty"`
	// SystemPrompt and UserPrompt are the text parts as sent, not the templates
	// they were assembled from.
	SystemPrompt string `json:"system_prompt,omitempty"`
	UserPrompt   string `json:"user_prompt,omitempty"`
	// Candidates is the numbered list the prompt rendered, so an experiment that
	// varies candidate labelling can recover the labels this call actually saw.
	Candidates []LLMCandidate `json:"candidates,omitempty"`
	// Screenshot is the run-relative path of the image sent with the call. It
	// names the step the image was captured at, which lags Step when the runner
	// skipped a transitional observation.
	Screenshot string `json:"screenshot,omitempty"`
	// RawResponse is the assistant content before parsing.
	RawResponse      string `json:"raw_response,omitempty"`
	PromptTokens     int    `json:"prompt_tokens,omitempty"`
	CompletionTokens int    `json:"completion_tokens,omitempty"`
	TotalTokens      int    `json:"total_tokens,omitempty"`
	LatencyMillis    int64  `json:"latency_millis,omitempty"`
	// Choice, EchoedAction and Reasoning are the parsed output, recorded
	// whenever parsing succeeded and so present on the outcomes that then
	// discarded the pick. EchoedAction is verbatim, including the weight
	// annotation models copy along with the line; on an echo_mismatch it is what
	// disagreed with Candidates[Choice-1].
	Choice       int    `json:"choice,omitempty"`
	EchoedAction string `json:"echoed_action,omitempty"`
	Reasoning    string `json:"reasoning,omitempty"`
	Error        string `json:"error,omitempty"`
}

// LLMCandidate is one numbered line of the candidate list as the model saw it.
type LLMCandidate struct {
	Index int    `json:"index"`
	Kind  string `json:"kind,omitempty"`
	// Description is the rendered line the model echoes back; Label is the
	// target's visible text the description was built from.
	Description string `json:"description"`
	Label       string `json:"label,omitempty"`
	// Weight is the percentage annotation shown on the line, 0 when the spec's
	// action tree declared no weights and none was shown.
	Weight int `json:"weight,omitempty"`
}

// WriteLLMCall appends one record to llm-calls.jsonl, creating the file on the
// first call.
func (w *Writer) WriteLLMCall(call LLMCall) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.file == nil {
		return fmt.Errorf("trace: writer is closed")
	}
	if w.llmCallEncoder == nil {
		file, err := os.OpenFile(
			filepath.Join(w.directory, LLMCallFileName),
			os.O_CREATE|os.O_WRONLY|os.O_APPEND,
			0o644,
		)
		if err != nil {
			return fmt.Errorf("open %s: %w", LLMCallFileName, err)
		}
		w.llmCallFile = file
		w.llmCallEncoder = json.NewEncoder(file)
	}
	return w.llmCallEncoder.Encode(call)
}
