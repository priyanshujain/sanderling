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
// when the picker declined to act this tick.
type ActionSource interface {
	NextAction(ctx context.Context) (verifier.Action, error)
}

// ExtractorSource yields per-step extractor overrides the runner applies after
// PushSnapshot. The mobile path has none (returns nil); the web path returns the
// values its extractors computed in V8 against the real DOM.
//
// lastAction and logs are what PushSnapshot hands the goja state. The web path
// has to install both in the page before its extractors run: a spec extractor
// reading state.lastAction or state.logs runs in V8 there, and V8 knows neither
// what the runner dispatched nor what the driver's log fetch returned.
type ExtractorSource interface {
	ExtractorOverrides(
		ctx context.Context,
		lastAction *verifier.Action,
		logs []verifier.LogEntry,
	) (map[int]json.RawMessage, error)
}

// lastActionInstaller is the web driver's channel for the previous step's
// action. It is declared here rather than folded into driver.WebDriver so the
// mobile drivers stay untouched; every web driver must implement it.
type lastActionInstaller interface {
	SetLastAction(ctx context.Context, encoded json.RawMessage) error
}

// logInstaller is the same channel for the entries this step's log fetch
// returned. Console output reaches the driver over CDP, so the page can only
// learn about it from the runner.
type logInstaller interface {
	SetLogs(ctx context.Context, encoded json.RawMessage) error
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

func (gojaSource) ExtractorOverrides(
	context.Context,
	*verifier.Action,
	[]verifier.LogEntry,
) (map[int]json.RawMessage, error) {
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

// ExtractorOverrides installs the previous step's action and this step's log
// entries in the page, then reads back what the spec's extractors computed
// against the live DOM. Neither install is best-effort: a web driver that
// cannot take them leaves state.lastAction null and state.logs empty in V8,
// which silently turns every action-gated property and every log property
// vacuously true, so both are reported as errors instead.
func (s webSource) ExtractorOverrides(
	ctx context.Context,
	lastAction *verifier.Action,
	logs []verifier.LogEntry,
) (map[int]json.RawMessage, error) {
	actions, ok := s.web.(lastActionInstaller)
	if !ok {
		return nil, fmt.Errorf(
			"web driver %T cannot install state.lastAction; every property gated "+
				"on the last action would be vacuously true", s.web)
	}
	if err := actions.SetLastAction(ctx, verifier.EncodeLastAction(lastAction)); err != nil {
		return nil, fmt.Errorf("install last action: %w", err)
	}
	entries, ok := s.web.(logInstaller)
	if !ok {
		return nil, fmt.Errorf(
			"web driver %T cannot install state.logs; every property reading the "+
				"log stream would be vacuously true", s.web)
	}
	if err := entries.SetLogs(ctx, verifier.EncodeLogs(logs)); err != nil {
		return nil, fmt.Errorf("install logs: %w", err)
	}
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
	return action, extractor, nil
}
