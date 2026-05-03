package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/priyanshujain/sanderling/internal/driver"
	"github.com/priyanshujain/sanderling/internal/hierarchy"
	"github.com/priyanshujain/sanderling/internal/ltl"
	"github.com/priyanshujain/sanderling/internal/trace"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

type Options struct {
	Duration    time.Duration
	IdleTimeout time.Duration

	BundleID    string
	Driver      driver.DeviceDriver
	Verifier    *verifier.Verifier
	TraceWriter *trace.Writer
	Logger      *slog.Logger
}

type Summary struct {
	StartTime  time.Time
	EndTime    time.Time
	Steps      int
	Violations []ViolationRecord
}

type ViolationRecord struct {
	StepIndex  int
	Properties []string
}

// Run drives the evaluate/act loop until the duration elapses or the context
// is canceled. The caller is responsible for launching the app before Run is
// called and for terminating it afterwards.
func Run(ctx context.Context, options Options) (Summary, error) {
	if err := validate(options); err != nil {
		return Summary{}, err
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	summary := Summary{StartTime: time.Now()}
	deadline := summary.StartTime.Add(options.Duration)
	stepIndex := 0
	var lastAction *verifier.Action
	var lastLogTime time.Time
	var pendingPostScreenshotStep int
	pendingPostScreenshot := false
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			break
		}
		stepIndex++
		stepStart := time.Now()

		// Hierarchy, metrics, and logs are independent device reads — run
		// them concurrently so metrics+logs hide behind the hierarchy fetch.
		var tree *hierarchy.Tree
		var hierarchyErr error
		var metrics *trace.Metrics
		var logs []verifier.LogEntry

		// gctx is bound to the errgroup so a returned error (or outer
		// cancellation) propagates to siblings - notably the V8 extractor
		// goroutine, whose CDP round-trip can otherwise outrun the step
		// budget on a hung tab.
		g, gctx := errgroup.WithContext(ctx)
		g.Go(func() error {
			tree, hierarchyErr = fetchHierarchy(gctx, options.Driver)
			return nil
		})
		si := stepIndex
		g.Go(func() error {
			metrics = captureMetrics(gctx, options, logger, si)
			return nil
		})
		logSince := lastLogTime
		g.Go(func() error {
			logs = collectLogs(gctx, options.Driver, logSince)
			return nil
		})
		var v8Overrides map[int]json.RawMessage
		if web, ok := options.Driver.(driver.WebDriver); ok {
			g.Go(func() error {
				overrides, err := web.EvaluateExtractors(gctx)
				if err != nil {
					logger.Warn("v8 extractor evaluation failed", "step", si, "err", err)
					return nil
				}
				v8Overrides = overrides
				return nil
			})
		}
		if pendingPostScreenshot {
			postStep := pendingPostScreenshotStep
			g.Go(func() error {
				captureScreenshot(gctx, options, logger, postStep, true)
				return nil
			})
			pendingPostScreenshot = false
		}
		// All goroutines write to local variables and return nil, so the Wait
		// error is always nil; ignored intentionally.
		_ = g.Wait()

		if hierarchyErr != nil {
			if isWDADrop(hierarchyErr) {
				return summary, fmt.Errorf("WDA connection permanently lost at step %d - re-run the test: %w", stepIndex, hierarchyErr)
			}
			logger.Warn("hierarchy fetch failed", "step", stepIndex, "err", hierarchyErr)
		}
		treeSize := 0
		if tree != nil {
			treeSize = len(tree.Elements)
		}
		lastLogTime = stepStart

		if err := options.Verifier.PushSnapshot(verifier.SnapshotInput{
			Tree:       tree,
			LastAction: lastAction,
			StepTime:   stepStart,
			RunStart:   summary.StartTime,
			Logs:       logs,
		}); err != nil {
			return summary, fmt.Errorf("step %d push: %w", stepIndex, err)
		}
		skipped, overrideErr := options.Verifier.OverrideExtractorValues(v8Overrides)
		if overrideErr != nil {
			logger.Warn("v8 override apply failed", "step", stepIndex, "err", overrideErr)
		}
		if skipped > 0 {
			logger.Warn("v8 override skipped out-of-range entries",
				"step", stepIndex, "skipped", skipped, "have", len(v8Overrides))
		}

		screen := ""
		if tree != nil && len(tree.Elements) > 0 {
			screen = tree.Elements[0].Screen
		}
		logger.Info("step", "index", stepIndex, "screen", screen, "nodes", treeSize)
		verdicts := options.Verifier.EvaluateProperties()
		violations := violationNames(verdicts)
		for _, name := range violations {
			if predicateErr := options.Verifier.PredicateError(name); predicateErr != nil {
				logger.Warn("predicate error", "step", stepIndex, "property", name, "err", predicateErr)
			}
		}

		var nextAction verifier.Action
		var nextErr error
		if web, ok := options.Driver.(driver.WebDriver); ok {
			nextAction, nextErr = nextActionFromV8(ctx, web)
		} else {
			nextAction, nextErr = options.Verifier.NextAction()
		}
		var traceAction *trace.Action
		if nextErr == nil {
			traceAction = traceActionFor(nextAction, tree)
		} else if !errors.Is(nextErr, verifier.ErrNoAction) {
			return summary, fmt.Errorf("step %d next action: %w", stepIndex, nextErr)
		}

		residuals, residualErr := encodeResiduals(options.Verifier.Residuals())
		if residualErr != nil {
			logger.Warn("residual encode failed", "step", stepIndex, "err", residualErr)
		}

		step := trace.Step{
			Index:      stepIndex,
			Timestamp:  stepStart,
			Screen:     screen,
			Action:     traceAction,
			Violations: violations,
			Hierarchy:  tree,
			Residuals:  residuals,
			Metrics:    metrics,
		}
		if err := options.TraceWriter.WriteStep(step); err != nil {
			return summary, fmt.Errorf("step %d trace: %w", stepIndex, err)
		}
		captureScreenshot(ctx, options, logger, stepIndex, false)
		summary.Steps = stepIndex
		if len(violations) > 0 {
			summary.Violations = append(summary.Violations, ViolationRecord{
				StepIndex:  stepIndex,
				Properties: violations,
			})
		}

		if nextErr == nil {
			if err := applyAction(ctx, options.Driver, nextAction, tree); err != nil {
				if isWDADrop(err) {
					return summary, fmt.Errorf("step %d: iOS XCTest runner lost connection - known WDA startup flake, re-run the test: %w", stepIndex, err)
				}
				return summary, fmt.Errorf("step %d apply: %w", stepIndex, err)
			}
			actionCopy := nextAction
			lastAction = &actionCopy
		} else {
			lastAction = nil
		}

		idleCtx, idleCancel := context.WithTimeout(ctx, options.IdleTimeout)
		idleErr := options.Driver.WaitForIdle(idleCtx, options.IdleTimeout)
		if nextErr == nil {
			pendingPostScreenshot = true
			pendingPostScreenshotStep = stepIndex
		}
		if idleErr != nil && idleCtx.Err() == nil {
			logger.Warn("wait_for_idle failed", "step", stepIndex, "err", idleErr)
		}
		idleCancel()
	}

	if pendingPostScreenshot {
		captureScreenshot(ctx, options, logger, pendingPostScreenshotStep, true)
	}

	summary.EndTime = time.Now()
	return summary, nil
}

func validate(options Options) error {
	if options.Driver == nil {
		return errors.New("runner: Driver is required")
	}
	if options.Verifier == nil {
		return errors.New("runner: Verifier is required")
	}
	if options.TraceWriter == nil {
		return errors.New("runner: TraceWriter is required")
	}
	if options.Duration <= 0 {
		return errors.New("runner: Duration must be positive")
	}
	if options.IdleTimeout <= 0 {
		options.IdleTimeout = 2 * time.Second
	}
	return nil
}

func violationNames(verdicts map[string]ltl.Verdict) []string {
	var names []string
	for name, verdict := range verdicts {
		if verdict == ltl.VerdictViolated {
			names = append(names, name)
		}
	}
	return names
}

func applyAction(ctx context.Context, drv driver.DeviceDriver, action verifier.Action, tree *hierarchy.Tree) error {
	switch action.Kind {
	case verifier.ActionKindTap:
		x, y, ok := resolveCoordinates(action, tree)
		if !ok {
			if action.On == "" {
				return nil
			}
			return drv.TapSelector(ctx, action.On)
		}
		return drv.Tap(ctx, x, y)
	case verifier.ActionKindInputText:
		if x, y, ok := resolveCoordinates(action, tree); ok {
			if err := drv.Tap(ctx, x, y); err != nil {
				return err
			}
		} else if action.On != "" {
			if err := drv.TapSelector(ctx, action.On); err != nil {
				return err
			}
		}
		return drv.InputText(ctx, action.Text)
	case verifier.ActionKindSwipe:
		duration := time.Duration(action.DurationMillis) * time.Millisecond
		if duration <= 0 {
			duration = 250 * time.Millisecond
		}
		return drv.Swipe(ctx, action.FromX, action.FromY, action.ToX, action.ToY, duration)
	case verifier.ActionKindPressKey:
		if action.Key == "" {
			return nil
		}
		return drv.PressKey(ctx, action.Key)
	case verifier.ActionKindWait:
		duration := time.Duration(action.DurationMillis) * time.Millisecond
		if duration <= 0 {
			return nil
		}
		timer := time.NewTimer(duration)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	default:
		return fmt.Errorf("unknown action kind %q", action.Kind)
	}
}

// collectLogs pulls recent error-level log entries from the driver since the
// previous fetch. A failure is warned-on but not fatal: log capture is a
// best-effort observability channel, not a correctness dependency.
func collectLogs(ctx context.Context, drv driver.DeviceDriver, since time.Time) []verifier.LogEntry {
	entries, err := drv.RecentLogs(ctx, since, "E")
	if err != nil {
		return nil
	}
	result := make([]verifier.LogEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, verifier.LogEntry{
			UnixMillis: entry.UnixMillis,
			Level:      entry.Level,
			Tag:        entry.Tag,
			Message:    entry.Message,
		})
	}
	return result
}

func resolveCoordinates(action verifier.Action, tree *hierarchy.Tree) (int, int, bool) {
	if action.X > 0 && action.Y > 0 {
		return action.X, action.Y, true
	}
	if tree != nil && action.On != "" {
		if element := tree.Find(action.On); element != nil {
			x, y := element.Bounds.Center()
			if x > 0 && y > 0 {
				return x, y, true
			}
		}
	}
	return 0, 0, false
}

func fetchHierarchy(ctx context.Context, drv driver.DeviceDriver) (*hierarchy.Tree, error) {
	xmlText, err := drv.Hierarchy(ctx)
	if err != nil {
		return nil, err
	}
	return hierarchy.Parse(xmlText)
}

func traceActionFor(action verifier.Action, tree *hierarchy.Tree) *trace.Action {
	traceAction := &trace.Action{Kind: string(action.Kind), X: action.X, Y: action.Y}
	switch action.Kind {
	case verifier.ActionKindTap:
		traceAction.Selector = action.On
		stampSelectorTarget(traceAction, action, tree)
	case verifier.ActionKindInputText:
		traceAction.Text = action.Text
		traceAction.Selector = action.On
		stampSelectorTarget(traceAction, action, tree)
	case verifier.ActionKindSwipe:
		traceAction.FromX = action.FromX
		traceAction.FromY = action.FromY
		traceAction.ToX = action.ToX
		traceAction.ToY = action.ToY
		traceAction.DurationMillis = action.DurationMillis
		traceAction.X = 0
		traceAction.Y = 0
	case verifier.ActionKindPressKey:
		traceAction.Key = action.Key
	case verifier.ActionKindWait:
		traceAction.DurationMillis = action.DurationMillis
	}
	return traceAction
}

// stampSelectorTarget mirrors applyAction's coordinate-resolution rule so the
// trace records the same point the runner taps.
func stampSelectorTarget(traceAction *trace.Action, action verifier.Action, tree *hierarchy.Tree) {
	if action.X > 0 && action.Y > 0 {
		traceAction.TapPoint = &trace.PointRecord{X: action.X, Y: action.Y}
		return
	}
	if tree == nil || action.On == "" {
		return
	}
	element := tree.Find(action.On)
	if element == nil {
		return
	}
	bounds := element.Bounds
	traceAction.ResolvedBounds = &trace.BoundsRecord{
		X:      bounds.Left,
		Y:      bounds.Top,
		Width:  bounds.Width(),
		Height: bounds.Height(),
	}
	x, y := bounds.Center()
	if x > 0 && y > 0 {
		traceAction.TapPoint = &trace.PointRecord{X: x, Y: y}
	}
}

func captureMetrics(ctx context.Context, options Options, logger *slog.Logger, stepIndex int) *trace.Metrics {
	if options.BundleID == "" {
		return nil
	}
	sample, err := options.Driver.Metrics(ctx, options.BundleID)
	if err != nil {
		logger.Warn("metrics capture failed", "step", stepIndex, "err", err)
		return nil
	}
	if sample.CPUPercent == 0 && sample.HeapBytes == 0 && sample.TotalMemoryBytes == 0 {
		return nil
	}
	return &trace.Metrics{
		CPUPercent:       sample.CPUPercent,
		HeapBytes:        sample.HeapBytes,
		TotalMemoryBytes: sample.TotalMemoryBytes,
	}
}

// nextActionFromV8 invokes the V8-side action generator and decodes the
// resulting JSON into a verifier.Action. ErrNoAction is returned when the
// generator declined to act this tick.
func nextActionFromV8(ctx context.Context, web driver.WebDriver) (verifier.Action, error) {
	raw, err := web.NextActionFromV8(ctx)
	if err != nil {
		return verifier.Action{}, fmt.Errorf("v8 next action: %w", err)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return verifier.Action{}, verifier.ErrNoAction
	}
	var decoded struct {
		Kind           string `json:"kind"`
		X              int    `json:"x"`
		Y              int    `json:"y"`
		FromX          int    `json:"from_x"`
		FromY          int    `json:"from_y"`
		ToX            int    `json:"to_x"`
		ToY            int    `json:"to_y"`
		Key            string `json:"key"`
		Text           string `json:"text"`
		DurationMillis int    `json:"duration_millis"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return verifier.Action{}, fmt.Errorf("decode v8 action: %w", err)
	}
	switch decoded.Kind {
	case "Tap":
		return verifier.Action{Kind: verifier.ActionKindTap, X: decoded.X, Y: decoded.Y}, nil
	case "InputText":
		return verifier.Action{
			Kind: verifier.ActionKindInputText,
			X:    decoded.X, Y: decoded.Y,
			Text: decoded.Text,
		}, nil
	case "Swipe":
		return verifier.Action{
			Kind:           verifier.ActionKindSwipe,
			FromX:          decoded.FromX,
			FromY:          decoded.FromY,
			ToX:            decoded.ToX,
			ToY:            decoded.ToY,
			DurationMillis: decoded.DurationMillis,
		}, nil
	case "PressKey":
		return verifier.Action{Kind: verifier.ActionKindPressKey, Key: decoded.Key}, nil
	case "Wait":
		return verifier.Action{Kind: verifier.ActionKindWait, DurationMillis: decoded.DurationMillis}, nil
	default:
		return verifier.Action{}, verifier.ErrNoAction
	}
}

func captureScreenshot(ctx context.Context, options Options, logger *slog.Logger, stepIndex int, after bool) {
	image, err := options.Driver.Screenshot(ctx)
	if err != nil {
		logger.Warn("screenshot capture failed", "step", stepIndex, "after", after, "err", err)
		return
	}
	if len(image.PNG) == 0 {
		return
	}
	var writeErr error
	if after {
		writeErr = options.TraceWriter.WriteScreenshotAfter(stepIndex, image.PNG)
	} else {
		writeErr = options.TraceWriter.WriteScreenshot(stepIndex, image.PNG)
	}
	if writeErr != nil {
		logger.Warn("screenshot write failed", "step", stepIndex, "after", after, "err", writeErr)
	}
}

func encodeResiduals(residuals map[string]ltl.Formula) (map[string]json.RawMessage, error) {
	if len(residuals) == 0 {
		return nil, nil
	}
	encoded := make(map[string]json.RawMessage, len(residuals))
	var firstErr error
	for name, formula := range residuals {
		body, err := json.Marshal(formula)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		encoded[name] = body
	}
	return encoded, firstErr
}

func isWDADrop(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "ConnectException") ||
		(strings.Contains(msg, "code = Internal") && strings.Contains(msg, "SocketException"))
}
