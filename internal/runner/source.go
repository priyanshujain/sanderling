package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/priyanshujain/sanderling/internal/driver"
	"github.com/priyanshujain/sanderling/internal/llmclient"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

// ActionSource resolves the next action for a step. Both runtimes (the goja
// picker and the web/V8 picker) implement it so the runner loop has one path
// and no per-step driver type assertion. NextAction returns verifier.ErrNoAction
// when the picker declined to act this tick. stepIndex is the trace line the
// decision belongs to, so a source that keeps its own records can key them to it
// rather than counting steps a second time.
type ActionSource interface {
	NextAction(ctx context.Context, stepIndex int) (verifier.Action, error)
}

// ExtractorSource yields per-step extractor overrides the runner applies after
// PushSnapshot. The mobile path has none (returns nil); the web path returns the
// values its extractors computed in V8 against the real DOM.
type ExtractorSource interface {
	ExtractorOverrides(ctx context.Context) (map[int]json.RawMessage, error)
}

// gojaSource drives both action selection and (trivially) extractor overrides
// for the mobile path, where the goja-bundled picker runs in-process and no V8
// extractor values exist.
type gojaSource struct {
	verifier *verifier.Verifier
}

func (s gojaSource) NextAction(context.Context, int) (verifier.Action, error) {
	return s.verifier.NextAction()
}

func (gojaSource) ExtractorOverrides(context.Context) (map[int]json.RawMessage, error) {
	return nil, nil
}

// webSource adapts the chrome driver's V8 picker: it evaluates the bundled
// __sanderlingNextAction__ / __sanderlingExtractors__ globals and decodes their
// JSON into the unified action and override shapes.
type webSource struct {
	web driver.WebDriver
}

func (s webSource) NextAction(ctx context.Context, _ int) (verifier.Action, error) {
	raw, err := s.web.NextActionFromV8(ctx)
	if err != nil {
		return verifier.Action{}, fmt.Errorf("v8 next action: %w", err)
	}
	// Both engines emit the unified flat camelCase wire contract; one decoder
	// reads it. A null payload means the generator declined to act this tick.
	return verifier.DecodeAction(raw)
}

func (s webSource) ExtractorOverrides(ctx context.Context) (map[int]json.RawMessage, error) {
	return s.web.EvaluateExtractors(ctx)
}

// pickSources selects the runtime's action and extractor sources ONCE at setup
// from the driver's capabilities, the --generator flag, and the spec, so the
// step loop never type-asserts.
//
// The two axes are independent. The DRIVER decides where extractor overrides
// come from: the web path reads the values its extractors computed in V8, every
// other path has none. The --generator flag decides who picks the action, and
// the llm picker composes with either extractor source because it reads the
// goja-side candidate list and screenshot, both of which the runner populates on
// every platform.
//
// A missing generator = llm(...) is fatal rather than a fallback to the seeded
// picker. Falling back silently corrupts a comparison campaign: the run
// completes and the output directory looks correct while the wrong policy drove
// it.
func pickSources(options Options) (ActionSource, ExtractorSource, error) {
	seeded := ActionSource(gojaSource{verifier: options.Verifier})
	extractor := ExtractorSource(gojaSource{verifier: options.Verifier})
	if web, ok := options.Driver.(driver.WebDriver); ok {
		source := webSource{web: web}
		seeded, extractor = source, source
	}
	if options.Generator != "llm" {
		return seeded, extractor, nil
	}
	config, ok := options.Verifier.LLMConfig()
	if !ok {
		return nil, nil, errors.New("--generator llm: the spec declares no generator = llm(...); add one to the spec or drop --generator llm")
	}
	client, err := llmclient.New()
	if err != nil {
		return nil, nil, fmt.Errorf("llm action generator: %w", err)
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	action := &llmSource{
		verifier:     options.Verifier,
		client:       client,
		model:        config.Model,
		instructions: config.Instructions,
		logger:       logger,
		history:      newActionHistory(llmHistorySize),
	}
	// Assigned only when present so the interface field stays nil rather than
	// holding a typed nil pointer that would panic on the first record.
	if options.TraceWriter != nil {
		action.recorder = options.TraceWriter
	}
	return action, extractor, nil
}
