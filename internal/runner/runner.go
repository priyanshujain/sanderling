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

	"github.com/priyanshujain/sanderling/internal/agent"
	"github.com/priyanshujain/sanderling/internal/driver"
	"github.com/priyanshujain/sanderling/internal/hierarchy"
	"github.com/priyanshujain/sanderling/internal/ltl"
	"github.com/priyanshujain/sanderling/internal/trace"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

type Options struct {
	Duration        time.Duration
	SnapshotTimeout time.Duration
	IdleTimeout     time.Duration

	BundleID    string
	Connection  *agent.Conn
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

// Run drives the snapshot/evaluate/release/act loop until the duration
// elapses or the context is canceled. The caller is responsible for
// launching the app and connecting the SDK before Run is called, and for
// terminating the app afterwards.
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

		// Hierarchy, metrics, and logs are independent device reads. Run
		// them concurrently so metrics+logs hide behind the hierarchy
		// fetch (~2s). All three must finish before snapshotStep pauses
		// the SDK.
		var tree *hierarchy.Tree
		var hierarchyErr error
		var metrics *trace.Metrics
		var logs []verifier.LogEntry

		g, _ := errgroup.WithContext(ctx)
		g.Go(func() error {
			tree, hierarchyErr = fetchHierarchy(ctx, options.Driver)
			return nil
		})
		si := stepIndex
		g.Go(func() error {
			metrics = captureMetrics(ctx, options, logger, si)
			return nil
		})
		logSince := lastLogTime
		g.Go(func() error {
			logs = collectLogs(ctx, options.Driver, logSince)
			return nil
		})
		if pendingPostScreenshot {
			postStep := pendingPostScreenshotStep
			g.Go(func() error {
				captureScreenshot(ctx, options, logger, postStep, true)
				return nil
			})
			pendingPostScreenshot = false
		}
		g.Wait()

		if hierarchyErr != nil {
			logger.Warn("hierarchy fetch failed", "step", stepIndex, "err", hierarchyErr)
		}
		treeSize := 0
		if tree != nil {
			treeSize = len(tree.Elements)
		}

		snapshot, err := snapshotStep(ctx, options)
		if err != nil {
			return summary, fmt.Errorf("step %d snapshot: %w", stepIndex, err)
		}
		lastLogTime = stepStart

		exceptions := decodeExceptions(snapshot)

		if err := options.Verifier.PushSnapshot(verifier.SnapshotInput{
			Snapshots:  verifier.Snapshots(snapshot.Snapshots),
			Tree:       tree,
			LastAction: lastAction,
			StepTime:   stepStart,
			RunStart:   summary.StartTime,
			Logs:       logs,
			Exceptions: exceptions,
		}); err != nil {
			return summary, fmt.Errorf("step %d push: %w", stepIndex, err)
		}
		screen, screenErr := screenFromSnapshot(snapshot.Snapshots)
		if screenErr != nil {
			logger.Warn("screen snapshot decode failed", "step", stepIndex, "err", screenErr)
		}
		if screen == "" && tree != nil && len(tree.Elements) > 0 {
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

		nextAction, nextErr := options.Verifier.NextAction()
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
			Snapshots:  snapshot.Snapshots,
			Action:     traceAction,
			Exceptions: traceExceptions(exceptions),
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

		if options.Connection != nil {
			if err := options.Connection.Release(ctx); err != nil {
				return summary, fmt.Errorf("step %d release: %w", stepIndex, err)
			}
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
	if options.SnapshotTimeout <= 0 {
		options.SnapshotTimeout = 5 * time.Second
	}
	if options.IdleTimeout <= 0 {
		options.IdleTimeout = 2 * time.Second
	}
	return nil
}

func snapshotStep(ctx context.Context, options Options) (agent.Message, error) {
	if options.Connection == nil {
		return agent.Message{}, nil
	}
	snapshotTimeout := options.SnapshotTimeout
	if snapshotTimeout <= 0 {
		snapshotTimeout = 5 * time.Second
	}
	snapshotCtx, snapshotCancel := context.WithTimeout(ctx, snapshotTimeout)
	defer snapshotCancel()
	return options.Connection.Snapshot(snapshotCtx)
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

func screenFromSnapshot(snapshots map[string]json.RawMessage) (string, error) {
	raw, ok := snapshots["screen"]
	if !ok {
		return "", nil
	}
	var screen string
	if err := json.Unmarshal(raw, &screen); err != nil {
		return "", err
	}
	return screen, nil
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

func decodeExceptions(snapshot agent.Message) []verifier.Exception {
	if len(snapshot.Exceptions) == 0 {
		return nil
	}
	result := make([]verifier.Exception, 0, len(snapshot.Exceptions))
	for _, e := range snapshot.Exceptions {
		result = append(result, verifier.Exception{
			Class:      e.Class,
			Message:    e.Message,
			StackTrace: e.StackTrace,
			UnixMillis: e.UnixMillis,
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
// trace records the same point the runner taps. When the spec passed an ax
// element directly, action.X/Y are already populated and we use them; when the
// spec passed a string selector, we resolve it against the captured hierarchy.
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

func traceExceptions(exceptions []verifier.Exception) []trace.Exception {
	if len(exceptions) == 0 {
		return nil
	}
	result := make([]trace.Exception, 0, len(exceptions))
	for _, e := range exceptions {
		result = append(result, trace.Exception{
			Class:      e.Class,
			Message:    e.Message,
			StackTrace: e.StackTrace,
			UnixMillis: e.UnixMillis,
		})
	}
	return result
}
