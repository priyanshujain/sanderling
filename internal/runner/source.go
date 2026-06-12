package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/priyanshujain/sanderling/internal/driver"
	"github.com/priyanshujain/sanderling/internal/llmclient"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

// ActionSource resolves the next action for a step. Both runtimes (the goja
// picker and the web/V8 picker) implement it so the runner loop has one path
// and no per-step driver type assertion. NextAction returns verifier.ErrNoAction
// when the picker declined to act this tick.
type ActionSource interface {
	NextAction(ctx context.Context) (verifier.Action, error)
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

func (s gojaSource) NextAction(context.Context) (verifier.Action, error) {
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

func (s webSource) NextAction(ctx context.Context) (verifier.Action, error) {
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
// from the driver's capabilities and the spec, so the step loop never
// type-asserts. When the spec selected the LLM action backend (actions =
// llm({...})) it constructs the chat-completions client and returns an
// llmSource for selection while extractor overrides still come from the goja
// path.
func pickSources(options Options) (ActionSource, ExtractorSource, error) {
	if web, ok := options.Driver.(driver.WebDriver); ok {
		source := webSource{web: web}
		return source, source, nil
	}
	if config, ok := options.Verifier.LLMConfig(); ok {
		client, err := llmclient.New()
		if err != nil {
			return nil, nil, fmt.Errorf("llm action backend: %w", err)
		}
		logger := options.Logger
		if logger == nil {
			logger = slog.Default()
		}
		action := &llmSource{
			verifier: options.Verifier,
			client:   client,
			model:    config.Model,
			logger:   logger,
			history:  newActionHistory(llmHistorySize),
		}
		return action, gojaSource{verifier: options.Verifier}, nil
	}
	source := gojaSource{verifier: options.Verifier}
	return source, source, nil
}
