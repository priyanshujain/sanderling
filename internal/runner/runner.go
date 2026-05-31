package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/priyanshujain/sanderling/internal/driver"
	"github.com/priyanshujain/sanderling/internal/hierarchy"
	"github.com/priyanshujain/sanderling/internal/ltl"
	"github.com/priyanshujain/sanderling/internal/trace"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

// doubleTapGap is the inter-tap delay for ActionKindDoubleTap: short enough to
// land both events inside a sub-100 ms race window, long enough for adb
// `input tap` to serialize two MotionEvent streams.
const doubleTapGap = 50 * time.Millisecond

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

	// Gate on the app actually being on top before acting, so the first
	// action never fires against a leftover screen or a system dialog. Done
	// before the deadline is set so the settle time does not eat the run.
	waitForForeground(ctx, options, logger)

	summary := Summary{StartTime: time.Now()}
	deadline := summary.StartTime.Add(options.Duration)
	stepIndex := 0
	var lastAction *verifier.Action
	var lastLogTime time.Time
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			break
		}
		stepIndex++
		stepStart := time.Now()

		// Keep exploration scoped to the app under test. If a prior action
		// backed out of (or otherwise left) the app, relaunch it before we
		// observe or act, so properties never evaluate against a foreign app
		// and actions never land outside the app.
		if ensureForeground(ctx, options, logger, stepIndex) {
			lastAction = nil
		}

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
		si := stepIndex
		// fetchSyncedState pairs hierarchy and screenshot in the same goroutine
		// so when a retry happens on a transitional tree, the screenshot stays
		// aligned with the final hierarchy snapshot.
		g.Go(func() error {
			tree, hierarchyErr = fetchSyncedState(gctx, options, logger, si)
			return nil
		})
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
		options.Verifier.EvaluateProperties()
		violations := options.Verifier.NewlyViolatedProperties()
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
			Index:            stepIndex,
			Timestamp:        stepStart,
			Screen:           screen,
			Action:           traceAction,
			Violations:       violations,
			Hierarchy:        tree,
			Residuals:        residuals,
			Metrics:          metrics,
			ExtractorChanges: encodeExtractorChanges(options.Verifier.ChangedExtractors()),
		}
		if err := options.TraceWriter.WriteStep(step); err != nil {
			return summary, fmt.Errorf("step %d trace: %w", stepIndex, err)
		}
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

		// Wait actions are themselves a settling: skip the idle poll. Actions
		// that mutate the UI fall through to WaitForIdle so the next step's
		// concurrent fetches observe a stable post-action state.
		if nextErr == nil && nextAction.Kind != verifier.ActionKindWait {
			idleCtx, idleCancel := context.WithTimeout(ctx, options.IdleTimeout)
			idleErr := options.Driver.WaitForIdle(idleCtx, options.IdleTimeout)
			if idleErr != nil && idleCtx.Err() == nil {
				logger.Warn("wait_for_idle failed", "step", stepIndex, "err", idleErr)
			}
			idleCancel()
		}
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

// ensureForeground keeps the app under test in the foreground. When the driver
// can report the foreground app and it no longer matches the bundle under test,
// the app is relaunched. Returns true when a relaunch happened so the caller
// can drop the now-stale lastAction. Drivers without ForegroundChecker (web,
// iOS) are a no-op.
func ensureForeground(ctx context.Context, options Options, logger *slog.Logger, stepIndex int) bool {
	checker, ok := options.Driver.(driver.ForegroundChecker)
	if !ok || options.BundleID == "" {
		return false
	}
	foreground, err := checker.ForegroundApp(ctx)
	if err != nil {
		logger.Warn("foreground check failed", "step", stepIndex, "err", err)
		return false
	}
	if foreground == "" || foreground == options.BundleID {
		return false
	}
	logger.Warn("app left foreground; relaunching",
		"step", stepIndex, "foreground", foreground, "want", options.BundleID)
	return bringToForeground(ctx, options, logger, stepIndex)
}

// foregroundReadyAttempts bounds how many times waitForForeground tries to
// bring the app forward before the first step, so a stuck system dialog can
// never hang the run.
const foregroundReadyAttempts = 8

// waitForForeground blocks until the app under test is actually on screen, so
// the first observe never captures a leftover screen or a freshly-booted
// device's system dialog (e.g. Android's "set a screen lock" prompt). Drivers
// without ForegroundChecker (web) and an unknown foreground both skip the gate.
//
// It is not enough that the app is the resumed activity: ResumedActivity flips
// to a freshly launched app ~before its first frame draws, so gating on it
// alone lets the first observe read the outgoing app. When the driver can also
// report the focused window, the gate additionally waits for that window to
// name the app, which only happens once it is genuinely drawn.
func waitForForeground(ctx context.Context, options Options, logger *slog.Logger) {
	checker, ok := options.Driver.(driver.ForegroundChecker)
	if !ok || options.BundleID == "" {
		return
	}
	focusChecker, hasFocus := options.Driver.(driver.FocusedWindowChecker)
	for attempt := range foregroundReadyAttempts {
		if err := ctx.Err(); err != nil {
			return
		}
		foreground, err := checker.ForegroundApp(ctx)
		if err != nil {
			logger.Warn("foreground check failed before first step", "err", err)
			return
		}
		if foreground == "" {
			return // foreground unknowable (e.g. iOS); don't block the run
		}
		if foreground != options.BundleID {
			logger.Warn("app not in foreground at start; bringing it forward",
				"foreground", foreground, "want", options.BundleID, "attempt", attempt)
			bringToForeground(ctx, options, logger, 0)
			continue
		}
		if !hasFocus {
			return // resumed is the app and no finer signal exists
		}
		focused, err := focusChecker.FocusedWindowApp(ctx)
		if err != nil {
			logger.Warn("focus check failed before first step", "err", err)
			return
		}
		if focused == options.BundleID {
			return // window is drawn; safe to observe
		}
		logger.Warn("app resumed but window not yet drawn; waiting",
			"focused", focused, "want", options.BundleID, "attempt", attempt)
		settleForForeground(ctx, options)
	}
	logger.Warn("app never reached foreground before first step; proceeding anyway",
		"want", options.BundleID)
}

// bringToForeground returns the app under test to the foreground. It first
// presses BACK to dismiss any modal system dialog (a relaunch alone does not
// close one), then relaunches and waits for the UI to settle. Returns true
// when the relaunch itself succeeded.
func bringToForeground(ctx context.Context, options Options, logger *slog.Logger, stepIndex int) bool {
	if err := options.Driver.PressKey(ctx, "back"); err != nil {
		logger.Warn("dismiss key before relaunch failed", "step", stepIndex, "err", err)
	}
	if err := options.Driver.Launch(ctx, options.BundleID, false, nil); err != nil {
		logger.Warn("relaunch failed", "step", stepIndex, "err", err)
		return false
	}
	settleForForeground(ctx, options)
	return true
}

// settleForForeground waits one idle window for the UI to settle, bounding the
// wait by the driver's idle timeout.
func settleForForeground(ctx context.Context, options Options) {
	idleCtx, cancel := context.WithTimeout(ctx, options.IdleTimeout)
	_ = options.Driver.WaitForIdle(idleCtx, options.IdleTimeout)
	cancel()
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
	case verifier.ActionKindDoubleTap:
		x, y, ok := resolveCoordinates(action, tree)
		tap := func() error {
			if !ok {
				if action.On == "" {
					return nil
				}
				return drv.TapSelector(ctx, action.On)
			}
			return drv.Tap(ctx, x, y)
		}
		if err := tap(); err != nil {
			return err
		}
		timer := time.NewTimer(doubleTapGap)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
		return tap()
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
	// When On is empty, X/Y are authoritative (web V8 path emits coordinates
	// directly from getBoundingClientRect; the runtime nullifies unresolved
	// actions upstream so a non-null InputText here always has real coords,
	// even at (0,0)). When On is set, prefer the tree lookup so stale coords
	// don't leak from earlier ticks.
	if action.On == "" {
		if action.X >= 0 && action.Y >= 0 {
			return action.X, action.Y, true
		}
		return 0, 0, false
	}
	if tree != nil {
		if element := tree.Find(action.On); element != nil {
			x, y := element.Bounds.Center()
			if x > 0 && y > 0 {
				return x, y, true
			}
		}
	}
	if action.X > 0 && action.Y > 0 {
		return action.X, action.Y, true
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

// transitionalRetryAttempts caps how many times we re-fetch hierarchy when a
// tree carries more than one route-level Screen tag (NavHost cross-fade in
// flight). Each retry pauses transitionalRetrySleep before the next fetch.
const (
	transitionalRetryAttempts = 4
	transitionalRetrySleep    = 200 * time.Millisecond
)

// fetchSyncedState fetches hierarchy and screenshot together so the recorded
// pair shows the same UI moment. If the hierarchy looks like a NavHost
// cross-fade (multiple route-level *Screen tags), the function waits briefly
// and re-fetches the pair, up to transitionalRetryAttempts times. This
// handles transitions whose async work begins after the sidecar's settle
// poll has already exited.
func fetchSyncedState(ctx context.Context, options Options, logger *slog.Logger, stepIndex int) (*hierarchy.Tree, error) {
	var tree *hierarchy.Tree
	var hierarchyErr error
	var pngBytes []byte
retryLoop:
	for attempt := range transitionalRetryAttempts {
		var wg sync.WaitGroup
		var localTree *hierarchy.Tree
		var localHierErr error
		var localImg driver.Image
		var localImgErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			localTree, localHierErr = fetchHierarchy(ctx, options.Driver)
		}()
		go func() {
			defer wg.Done()
			localImg, localImgErr = options.Driver.Screenshot(ctx)
		}()
		wg.Wait()
		tree = localTree
		hierarchyErr = localHierErr
		if localImgErr == nil {
			pngBytes = localImg.PNG
		}
		if hierarchyErr != nil || !isTransitionalHierarchy(tree) {
			break
		}
		if attempt == transitionalRetryAttempts-1 {
			break
		}
		timer := time.NewTimer(transitionalRetrySleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			break retryLoop
		case <-timer.C:
		}
	}
	if len(pngBytes) > 0 {
		if writeErr := options.TraceWriter.WriteScreenshot(stepIndex, pngBytes); writeErr != nil {
			logger.Warn("screenshot write failed", "step", stepIndex, "err", writeErr)
		}
	}
	return tree, hierarchyErr
}

// isTransitionalHierarchy returns true when the tree carries more than one
// resource-id ending in "Screen" - the marker of a Compose NavHost mid
// cross-fade where both source and destination route composables are alive.
// Mirrors the sidecar's stabilitySnapshot heuristic so runner-side rejection
// stays consistent with the settle poll.
func isTransitionalHierarchy(tree *hierarchy.Tree) bool {
	if tree == nil {
		return false
	}
	screens := 0
	for _, element := range tree.Elements {
		if strings.HasSuffix(element.ResourceID, "Screen") {
			screens++
			if screens > 1 {
				return true
			}
		}
	}
	return false
}

func traceActionFor(action verifier.Action, tree *hierarchy.Tree) *trace.Action {
	traceAction := &trace.Action{Kind: string(action.Kind), X: action.X, Y: action.Y}
	switch action.Kind {
	case verifier.ActionKindTap, verifier.ActionKindDoubleTap:
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
	case "DoubleTap":
		return verifier.Action{Kind: verifier.ActionKindDoubleTap, X: decoded.X, Y: decoded.Y}, nil
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

func encodeExtractorChanges(changes map[string]verifier.ExtractorChange) map[string]trace.ExtractorChange {
	if len(changes) == 0 {
		return nil
	}
	out := make(map[string]trace.ExtractorChange, len(changes))
	for name, change := range changes {
		out[name] = trace.ExtractorChange{
			Prev: json.RawMessage(change.Prev),
			Curr: json.RawMessage(change.Curr),
		}
	}
	return out
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
