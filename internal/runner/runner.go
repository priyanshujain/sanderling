// Package runner drives the observe-decide-act loop that steps a spec against a device.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
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

	// MaxSteps caps the run at a fixed number of steps for reproducible
	// bounded runs. 0 means unbounded (the duration deadline governs); a
	// positive value stops the loop once that many steps have run.
	MaxSteps int

	// StopOnViolation ends the step loop as soon as a step records a
	// violation, so a run that exists to find one bug stops at the evidence
	// instead of spending the rest of its budget past it.
	StopOnViolation bool

	BundleID    string
	Driver      driver.DeviceDriver
	Verifier    *verifier.Verifier
	TraceWriter *trace.Writer
	Logger      *slog.Logger
	// Generator selects the action picker: "llm" drives selection with the
	// spec's generator = llm({...}) config; anything else (the default) uses the
	// seeded weighted picker. Both draw from the same actionsRoot candidate set.
	Generator string
}

type Summary struct {
	StartTime  time.Time
	EndTime    time.Time
	Steps      int
	Violations []ViolationRecord
	// UnsupportedVerbs lists verbs the picker requested that the platform
	// could not dispatch, deduped, so the report can flag a spec exercising
	// gestures this target does not support.
	UnsupportedVerbs []string
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
	options.IdleTimeout = resolveIdleTimeout(options)

	// Gate on the app actually being on top before acting, so the first
	// action never fires against a leftover screen or a system dialog. Done
	// before the deadline is set so the settle time does not eat the run.
	waitForForeground(ctx, options, logger)

	// Pick the action and extractor sources once from the driver's
	// capabilities so the step loop runs one uniform path with no per-step
	// driver type assertion.
	actionSource, extractorSource, err := pickSources(options)
	if err != nil {
		return Summary{}, err
	}
	_, pageExtractors := extractorSource.(webSource)

	summary := Summary{StartTime: time.Now()}
	deadline := summary.StartTime.Add(options.Duration)
	stepIndex := 0
	consecutiveApplyFailures := 0
	var lastAction *verifier.Action
	var lastLogTime time.Time
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			break
		}
		if options.MaxSteps > 0 && stepIndex >= options.MaxSteps {
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

		// Hierarchy, metrics, and logs are independent device reads. Run
		// them concurrently so metrics+logs hide behind the hierarchy fetch.
		var tree *hierarchy.Tree
		var hierarchyErr error
		var transitional bool
		var screenshotPNG []byte
		var metrics *trace.Metrics
		var logs []verifier.LogEntry

		// gctx is bound to the errgroup so a returned error (or outer
		// cancellation) propagates to every sibling read rather than leaving
		// one blocked on a hung device.
		g, gctx := errgroup.WithContext(ctx)
		si := stepIndex
		// fetchSyncedState issues a single Snapshot RPC so hierarchy and
		// screenshot describe the same frame, then re-fetches the pair
		// while the tree still looks transitional.
		g.Go(func() error {
			tree, screenshotPNG, transitional, hierarchyErr = fetchSyncedState(gctx, options, logger, si)
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
		// A nil or empty tree means the sidecar's hierarchy fetch failed or
		// returned nothing (e.g. transient device-side timeout). Pushing it
		// would let spec extractors call findAll() and chain .map() on a null
		// result; treat it like a transitional capture so the verifier is
		// skipped, the step is still recorded, and the loop progresses.
		if treeSize == 0 {
			transitional = true
		}
		lastLogTime = stepStart

		screen := ""
		if tree != nil && len(tree.Elements) > 0 {
			screen = tree.Elements[0].Screen
		}

		// Transitional trees describe a NavHost mid cross-fade. Pushing
		// one would poison the verifier's previous/current extractor
		// advance, so the next clean step would compare against this
		// transient state and emit false-positive violations. We still
		// record the step (hierarchy + screenshot) for replay-side
		// debugging, but skip the verifier entirely and pick the next
		// action against the unchanged prior state to keep the loop
		// progressing.
		var violations []string
		var extractorChanges map[string]trace.ExtractorChange
		var witnesses map[string]trace.Witness
		skippedVerification := false
		if !transitional {
			// The page-side extractors evaluate only on steps the verifier will
			// accept, which is why this read waits for the tree instead of
			// racing it. A spec's extractor getters carry state across steps
			// (folio's last-seen Home total, its submit counters) and that state
			// advances every time they run: evaluating them on a step whose
			// values are then thrown away leaves the page one window ahead of
			// the verifier, so the next accepted pair brackets two committed
			// transactions while having counted one submit, and the property
			// convicts a healthy app. It costs the latency the read used to hide
			// behind the hierarchy fetch; the fetch is what decides whether this
			// step counts at all, so it has to go first.
			//
			// lastAction is the same value PushSnapshot hands the goja state
			// below: the two engines evaluate this step against one action.
			v8Overrides, overridesErr := extractorSource.ExtractorOverrides(ctx, lastAction)
			if overridesErr != nil {
				// Not a warning. Without the page's values this step's
				// extractors keep goja's dump-derived readings while the
				// previous step holds the page's, and a delta property then
				// compares two producers and fires on an app that did nothing
				// wrong.
				return summary, fmt.Errorf("step %d extractor overrides: %w", stepIndex, overridesErr)
			}
			if err := options.Verifier.PushSnapshot(verifier.SnapshotInput{
				Tree:          tree,
				ScreenshotPNG: screenshotPNG,
				LastAction:    lastAction,
				StepTime:      stepStart,
				StepIndex:     stepIndex,
				RunStart:      summary.StartTime,
				Logs:          logs,
			}); err != nil {
				return summary, fmt.Errorf("step %d push: %w", stepIndex, err)
			}
			// Every failure below leaves some extractors holding the page's
			// value and the rest holding goja's reading of the dump, and a
			// property comparing previous to current across that split fires
			// on a healthy app. Each also means the two engines loaded
			// different bundles, which nothing downstream can reconcile.
			if pageExtractors && len(v8Overrides) != options.Verifier.ExtractorCount() {
				return summary, fmt.Errorf(
					"step %d: the page reported values for %d of the spec's %d extractors; "+
						"the page and the host are running different bundles",
					stepIndex, len(v8Overrides), options.Verifier.ExtractorCount())
			}
			skipped, overrideErr := options.Verifier.OverrideExtractorValues(v8Overrides)
			if overrideErr != nil {
				return summary, fmt.Errorf("step %d apply extractor overrides: %w", stepIndex, overrideErr)
			}
			if skipped > 0 {
				return summary, fmt.Errorf(
					"step %d: %d of %d extractor overrides fell outside the spec's extractor list; "+
						"the page and the host are running different bundles",
					stepIndex, skipped, len(v8Overrides))
			}
			options.Verifier.EvaluateProperties()
			violations = options.Verifier.NewlyViolatedProperties()
			witnesses = collectWitnesses(options.Verifier, violations, logger, stepIndex)
			extractorChanges = encodeExtractorChanges(options.Verifier.ChangedExtractors())
		} else {
			skippedVerification = true
			logger.Warn("transitional tree after retry budget; skipping verifier",
				"step", stepIndex, "screen", screen, "nodes", treeSize)
		}
		logger.Info("step", "index", stepIndex, "screen", screen, "nodes", treeSize)

		nextAction, nextErr := actionSource.NextAction(ctx)
		var traceAction *trace.Action
		if nextErr == nil {
			traceAction = traceActionFor(nextAction, tree)
			stampActionSource(traceAction, actionSource)
		} else if !errors.Is(nextErr, verifier.ErrNoAction) {
			return summary, fmt.Errorf("step %d next action: %w", stepIndex, nextErr)
		}

		residuals, residualErr := encodeResiduals(options.Verifier.Residuals())
		if residualErr != nil {
			logger.Warn("residual encode failed", "step", stepIndex, "err", residualErr)
		}

		applySkipped := false
		if nextErr == nil && !appIsForeground(ctx, options) {
			// The app left the foreground between observe and apply (a prior
			// action's gesture settling late, or an async navigation). The
			// chosen action's coordinates reference a tree that no longer
			// applies, so firing it would act on whatever screen is now up.
			// Skip it and record the escape; the next step's guard relaunches.
			logger.Warn("app not in foreground at action time; skipping (relaunch next step)",
				"step", stepIndex, "action", nextAction.Kind)
			applySkipped = true
			lastAction = nil
		} else if nextErr == nil {
			if err := applyAction(ctx, options.Driver, nextAction, tree); err != nil {
				if isWDADrop(err) {
					return summary, fmt.Errorf("step %d: the iOS XCTest runner could not be restarted - re-run the test: %w", stepIndex, err)
				}
				if ctx.Err() != nil {
					return summary, fmt.Errorf("step %d apply: %w", stepIndex, err)
				}
				// Every apply error is a device-side condition (a dropped
				// gesture, a typing request the runner's input handler choked
				// on, an RPC deadline). None of them individually justify
				// killing a fuzz run; what does is an unbroken streak, which
				// means the device is wedged. The step is marked transitional
				// so the verifier never sees a state the action did not reach.
				consecutiveApplyFailures++
				if consecutiveApplyFailures >= maxConsecutiveApplyFailures {
					return summary, fmt.Errorf("step %d apply: %d consecutive failures; the device is not recovering: %w", stepIndex, consecutiveApplyFailures, err)
				}
				logger.Warn("apply error; marking step transitional", "step", stepIndex, "err", err)
				transitional = true
				applySkipped = true
				lastAction = nil
			} else {
				consecutiveApplyFailures = 0
				actionCopy := nextAction
				lastAction = &actionCopy
			}
		} else {
			lastAction = nil
		}

		step := trace.Step{
			Index:               stepIndex,
			Timestamp:           stepStart,
			Screen:              screen,
			NextAction:          traceAction,
			Violations:          violations,
			Hierarchy:           tree,
			Residuals:           residuals,
			Metrics:             metrics,
			ExtractorChanges:    extractorChanges,
			Transitional:        transitional,
			SkippedVerification: skippedVerification,
			Witnesses:           witnesses,
		}
		if err := options.TraceWriter.WriteStep(step); err != nil {
			return summary, fmt.Errorf("step %d trace: %w", stepIndex, err)
		}
		summary.Steps = stepIndex
		if len(violations) > 0 {
			summary.Violations = append(summary.Violations, violationRecords(violations, witnesses, stepIndex)...)
			// The step is already written, so the trace ends on the state that
			// produced the violation. Finalize below still runs, so pending
			// liveness obligations are reported alongside it.
			if options.StopOnViolation {
				break
			}
		}
		// Wait actions are themselves a settling: skip the idle poll. Actions
		// that mutate the UI fall through to WaitForIdle so the next step's
		// concurrent fetches observe a stable post-action state. A transient
		// apply error means nothing landed, so the idle poll has nothing to
		// settle and may itself hang on the same device condition.
		if nextErr == nil && !applySkipped && nextAction.Kind != verifier.ActionKindWait {
			idleCtx, idleCancel := context.WithTimeout(ctx, options.IdleTimeout)
			idleErr := options.Driver.WaitForIdle(idleCtx, options.IdleTimeout)
			if idleErr != nil && idleCtx.Err() == nil {
				logger.Warn("wait_for_idle failed", "step", stepIndex, "err", idleErr)
			}
			idleCancel()
		}
	}

	// Finalize each evaluator once the loop ends so liveness obligations that
	// never discharged (an eventually that never fired) are reported as
	// violations rather than silently left pending. Properties already
	// violated mid-run are not re-reported. The synthetic record gets its own
	// step index so no two trace lines share one; witnesses still attribute
	// the violation to the step that spawned the obligation.
	if ended := options.Verifier.Finalize(); len(ended) > 0 {
		finalIndex := stepIndex + 1
		witnesses := collectWitnesses(options.Verifier, ended, logger, finalIndex)
		summary.Violations = append(summary.Violations, violationRecords(ended, witnesses, finalIndex)...)
		finalStep := trace.Step{
			Index:      finalIndex,
			Timestamp:  time.Now(),
			Violations: ended,
			Witnesses:  witnesses,
		}
		if err := options.TraceWriter.WriteStep(finalStep); err != nil {
			return summary, fmt.Errorf("finalize trace: %w", err)
		}
	}

	summary.UnsupportedVerbs = options.Verifier.UnsupportedVerbs()
	summary.EndTime = time.Now()
	return summary, nil
}

// RenderSummary writes the human-facing run summary: step count, each violation
// record, and any unsupported verbs. The wall-clock duration is excluded so the
// output is deterministic and snapshot-testable; the CLI prints it separately.
func RenderSummary(w io.Writer, summary Summary, platform string) {
	fmt.Fprintf(w, "\nrun complete: %d steps\n", summary.Steps)
	if len(summary.Violations) == 0 {
		fmt.Fprintln(w, "no violations.")
	} else {
		fmt.Fprintf(w, "%d violation record(s):\n", len(summary.Violations))
		for _, violation := range summary.Violations {
			fmt.Fprintf(w, "  step %d: %v\n", violation.StepIndex, violation.Properties)
		}
	}
	if len(summary.UnsupportedVerbs) > 0 {
		fmt.Fprintf(w, "unsupported on %s: %s\n",
			platform, strings.Join(summary.UnsupportedVerbs, ", "))
	}
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
	return nil
}

// defaultIdleTimeout is the settle budget a caller that names none gets.
const defaultIdleTimeout = 2 * time.Second

// idleTimeoutFloor is a driver that knows how long its own settle can take.
// Declared here rather than in the driver package (like lastActionInstaller in
// source.go) so the mobile drivers stay untouched.
type idleTimeoutFloor interface {
	MinIdleTimeout() time.Duration
}

// resolveIdleTimeout settles the per-step settle budget: the caller's value,
// defaulted when unset, and raised to whatever the driver says its own settle
// needs. The chrome driver's settle waits for the DOM to go quiet and only then
// opens its route-transition window; handed less than their sum it is cut off
// mid-transition, and the step samples the screen the app is leaving. A driver
// that reports no floor keeps the caller's value exactly.
func resolveIdleTimeout(options Options) time.Duration {
	timeout := options.IdleTimeout
	if timeout <= 0 {
		timeout = defaultIdleTimeout
	}
	if floor, ok := options.Driver.(idleTimeoutFloor); ok {
		timeout = max(timeout, floor.MinIdleTimeout())
	}
	return timeout
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
	if foreground != "" && foreground != options.BundleID {
		logger.Warn("app left foreground; relaunching",
			"step", stepIndex, "foreground", foreground, "want", options.BundleID)
		// Relaunch and confirm the app is genuinely back on screen before the
		// step observes or acts. A single relaunch returns before the window
		// draws on a slow physical device, which would let the observe and the
		// next action land on the launcher (its type-to-search swallows
		// InputText). awaitForeground re-checks the foreground and focused
		// window, so it never acts outside the app no matter how slow the
		// relaunch settles.
		awaitForeground(ctx, options, logger, stepIndex)
		return true
	}
	// The app is the resumed activity, but a system overlay can still own the
	// focused window while the app stays resumed: a fuzzer swipe starting in the
	// status bar pulls the notification shade over the app. The resumed-activity
	// signal misses this, so observing or acting would land on the shade.
	// Dismiss it with back (which collapses the shade) so the next observe sees
	// the app again.
	focusChecker, hasFocus := options.Driver.(driver.FocusedWindowChecker)
	if !hasFocus {
		return false
	}
	focused, err := focusChecker.FocusedWindowApp(ctx)
	if err != nil {
		logger.Warn("focus check failed", "step", stepIndex, "err", err)
		return false
	}
	if focused == "" || focused == options.BundleID {
		return false
	}
	logger.Warn("system window obscuring app; dismissing",
		"step", stepIndex, "focused", focused, "want", options.BundleID)
	if err := options.Driver.PressKey(ctx, "back"); err != nil {
		logger.Warn("dismiss overlay failed", "step", stepIndex, "err", err)
	}
	settleForForeground(ctx, options)
	return true
}

// appIsForeground reports whether the app under test currently owns the
// foreground. It is the apply-time half of the scope guard: ensureForeground
// runs before observe, but the app can leave between observe and apply (a prior
// gesture settling late, an async navigation), and swipes/keys carry stale
// coordinates with no selector to re-resolve. An absent capability or an unknown
// foreground returns true so the run is never blocked where the signal is
// unavailable (web, iOS, a transient read).
func appIsForeground(ctx context.Context, options Options) bool {
	checker, ok := options.Driver.(driver.ForegroundChecker)
	if !ok || options.BundleID == "" {
		return true
	}
	foreground, err := checker.ForegroundApp(ctx)
	if err != nil || foreground == "" {
		return true
	}
	if foreground != options.BundleID {
		return false
	}
	// A system overlay can own the focused window while the app stays resumed,
	// so mirror ensureForeground's focus check rather than act on the overlay.
	focusChecker, ok := options.Driver.(driver.FocusedWindowChecker)
	if !ok {
		return true
	}
	focused, err := focusChecker.FocusedWindowApp(ctx)
	if err != nil || focused == "" {
		return true
	}
	return focused == options.BundleID
}

// foregroundReadyAttempts bounds how many times waitForForeground tries to
// bring the app forward before the first step, so a stuck system dialog can
// never hang the run.
const foregroundReadyAttempts = 8

// focusTapSettle is the pause after tapping a field to focus it, before typing.
// Long enough for focus to land, short enough to avoid the ~500ms-1s full
// settle the keyboard's open animation would otherwise cost every InputText
// step on a physical device.
var focusTapSettle = 250 * time.Millisecond

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
	awaitForeground(ctx, options, logger, 0)
}

// awaitForeground brings the app under test forward when it is not already
// resumed and blocks until its window is actually drawn, bounded by
// foregroundReadyAttempts so a stuck system dialog can never hang the run. It
// re-checks the foreground each iteration and only presses back + relaunches
// while the app is genuinely absent, so once the app is resumed it polls the
// focused-window signal instead of mashing back (which would re-exit the app
// from its root screen). Shared by the pre-run startup gate (stepIndex 0) and
// the per-step scope guard so neither lets an observe or action land outside
// the app. Drivers without ForegroundChecker (web) and an unknown foreground
// both skip the gate.
func awaitForeground(ctx context.Context, options Options, logger *slog.Logger, stepIndex int) {
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
			logger.Warn("foreground check failed", "step", stepIndex, "err", err)
			return
		}
		if foreground == "" {
			return // foreground unknowable (e.g. iOS); don't block the run
		}
		if foreground != options.BundleID {
			logger.Warn("app not in foreground; bringing it forward",
				"step", stepIndex, "foreground", foreground, "want", options.BundleID, "attempt", attempt)
			bringToForeground(ctx, options, logger, stepIndex)
			continue
		}
		if !hasFocus {
			return // resumed is the app and no finer signal exists
		}
		focused, err := focusChecker.FocusedWindowApp(ctx)
		if err != nil {
			logger.Warn("focus check failed", "step", stepIndex, "err", err)
			return
		}
		if focused == options.BundleID {
			return // window is drawn; safe to observe
		}
		logger.Warn("app resumed but window not yet drawn; waiting",
			"step", stepIndex, "focused", focused, "want", options.BundleID, "attempt", attempt)
		settleForForeground(ctx, options)
	}
	logger.Warn("app never reached foreground; proceeding anyway",
		"step", stepIndex, "want", options.BundleID)
}

// bringToForeground returns the app under test to the foreground. It first
// presses BACK to dismiss any modal system dialog (a relaunch alone does not
// close one), then relaunches and waits for the UI to settle.
func bringToForeground(ctx context.Context, options Options, logger *slog.Logger, stepIndex int) {
	if err := options.Driver.PressKey(ctx, "back"); err != nil {
		logger.Warn("dismiss key before relaunch failed", "step", stepIndex, "err", err)
	}
	if err := options.Driver.Launch(ctx, options.BundleID, false, nil); err != nil {
		logger.Warn("relaunch failed", "step", stepIndex, "err", err)
		return
	}
	settleForForeground(ctx, options)
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
		if !ok {
			if action.On == "" {
				return nil
			}
			return drv.DoubleTapSelector(ctx, action.On)
		}
		return drv.DoubleTap(ctx, x, y)
	case verifier.ActionKindLongPress:
		x, y, ok := resolveCoordinates(action, tree)
		if !ok {
			// No long-press-by-selector RPC exists, so an unresolved target is
			// nothing we can dispatch; skip rather than error.
			return nil
		}
		return drv.LongPress(ctx, x, y)
	case verifier.ActionKindScroll:
		fromX, fromY, toX, toY := scrollEndpoints(action, tree)
		fromX, fromY, toX, toY = clampGestureToSafeArea(fromX, fromY, toX, toY, screenBounds(tree))
		duration := time.Duration(action.DurationMillis) * time.Millisecond
		if duration <= 0 {
			duration = 300 * time.Millisecond
		}
		return drv.Swipe(ctx, fromX, fromY, toX, toY, duration)
	case verifier.ActionKindInputText:
		tapped := false
		if x, y, ok := resolveCoordinates(action, tree); ok {
			if err := drv.Tap(ctx, x, y); err != nil {
				return err
			}
			tapped = true
		} else if action.On != "" {
			if err := drv.TapSelector(ctx, action.On); err != nil {
				return err
			}
			tapped = true
		}
		// The focus tap raises the keyboard. The tap registers focus
		// immediately and the text is injected into the focused view (not typed
		// on the visible keyboard), so a brief pause is enough for focus to land
		// rather than a full settle, which costs ~500ms-1s per InputText step on
		// a physical device while the keyboard animates in.
		if tapped {
			timer := time.NewTimer(focusTapSettle)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		// InputText replaces the field's content: erase what the target
		// holds before typing. Appending instead lets repeated draws grow
		// the field without bound (e.g. into a max-length validation error
		// the fuzzer can never escape) and makes retried typing land twice.
		// Drivers whose InputText already replaces skip the erase entirely.
		if !inputReplacesText(drv) {
			if count := existingTextLength(action, tree); count > 0 {
				if err := drv.EraseText(ctx, count); err != nil {
					return err
				}
			}
		}
		return drv.InputText(ctx, action.Text)
	case verifier.ActionKindSwipe:
		duration := time.Duration(action.DurationMillis) * time.Millisecond
		if duration <= 0 {
			duration = 250 * time.Millisecond
		}
		fromX, fromY, toX, toY := clampGestureToSafeArea(action.FromX, action.FromY, action.ToX, action.ToY, screenBounds(tree))
		return drv.Swipe(ctx, fromX, fromY, toX, toY, duration)
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

// inputReplacesText reports whether the driver's InputText replaces existing
// content, making the runner's pre-erase redundant.
func inputReplacesText(drv driver.DeviceDriver) bool {
	replacer, ok := drv.(driver.TextReplacer)
	return ok && replacer.ReplacesTextOnInput()
}

// existingTextLength returns the character count of the InputText target's
// current text, so the runner can erase it before typing. Zero when the
// target cannot be resolved or holds no text.
func existingTextLength(action verifier.Action, tree *hierarchy.Tree) int {
	if action.On == "" || tree == nil {
		return 0
	}
	element := tree.Find(action.On)
	if element == nil {
		return 0
	}
	return len([]rune(element.Text))
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

// scrollEndpoints lowers a Scroll to a swipe's from/to points. Pre-computed
// endpoints (from the generator) win. Otherwise it derives them from the
// container bounds: the named node when On resolves, else the whole screen.
func scrollEndpoints(action verifier.Action, tree *hierarchy.Tree) (fromX, fromY, toX, toY int) {
	if action.FromX != 0 || action.FromY != 0 || action.ToX != 0 || action.ToY != 0 {
		return action.FromX, action.FromY, action.ToX, action.ToY
	}
	bounds := scrollBounds(action, tree)
	cx, cy := bounds.Center()
	width := bounds.Width()
	height := bounds.Height()
	toX, toY = cx, cy
	// Scroll direction names content motion; the gesture swipes the opposite
	// way. Revealing lower content ("down") drags the finger up, so toY drops.
	switch action.Direction {
	case "down":
		toY = cy - (4*height)/10
	case "up":
		toY = cy + (4*height)/10
	case "left":
		toX = cx + (4*width)/10
	case "right":
		toX = cx - (4*width)/10
	}
	if toX < 0 {
		toX = 0
	}
	if toY < 0 {
		toY = 0
	}
	return cx, cy, toX, toY
}

// screenBounds returns the device screen rectangle as the maximum extent across
// all elements. The hierarchy root often reports zero bounds on Android, so the
// extent (driven by full-screen containers and the navigation bar) is the
// reliable screen size. Returns a zero rectangle when unknown.
func screenBounds(tree *hierarchy.Tree) hierarchy.Bounds {
	if tree == nil {
		return hierarchy.Bounds{}
	}
	var bounds hierarchy.Bounds
	for _, element := range tree.Elements {
		if element.Bounds.Right > bounds.Right {
			bounds.Right = element.Bounds.Right
		}
		if element.Bounds.Bottom > bounds.Bottom {
			bounds.Bottom = element.Bounds.Bottom
		}
	}
	return bounds
}

// clampGestureToSafeArea keeps a swipe's origin below the top status strip,
// where a downward drag pulls the notification shade over the app. Runs force
// 3-button navigation (ForceThreeButtonNav), which disables the side back and
// bottom home gestures at the OS level; on-device probing confirmed side and
// bottom origins then no longer drift, so the shade is the only edge gesture a
// swipe can still trigger. Origin and destination are otherwise only kept on
// screen. With an unknown screen size the coordinates pass through unchanged.
func clampGestureToSafeArea(fromX, fromY, toX, toY int, screen hierarchy.Bounds) (int, int, int, int) {
	width, height := screen.Width(), screen.Height()
	if width <= 0 || height <= 0 {
		return fromX, fromY, toX, toY
	}
	// Translate the whole segment when the origin is in the top margin, rather
	// than clamping the origin alone, which could push it past the destination
	// and reverse a near-top scroll.
	marginY := height / 12
	if shortfall := (screen.Top + marginY) - fromY; shortfall > 0 {
		fromY += shortfall
		toY += shortfall
	}
	clamp := func(value, low, high int) int {
		if value < low {
			return low
		}
		if value > high {
			return high
		}
		return value
	}
	fromX = clamp(fromX, screen.Left, screen.Right)
	fromY = clamp(fromY, screen.Top, screen.Bottom)
	toX = clamp(toX, screen.Left, screen.Right)
	toY = clamp(toY, screen.Top, screen.Bottom)
	return fromX, fromY, toX, toY
}

// scrollBounds returns the container bounds for an authored Scroll: the node
// named by On when it resolves, otherwise the root (whole-screen) bounds.
func scrollBounds(action verifier.Action, tree *hierarchy.Tree) hierarchy.Bounds {
	if tree == nil {
		return hierarchy.Bounds{}
	}
	if action.On != "" {
		if element := tree.Find(action.On); element != nil {
			return element.Bounds
		}
	}
	if tree.Root != nil {
		return tree.Root.Bounds
	}
	return hierarchy.Bounds{}
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
//
// The driver's Snapshot RPC captures both reads under a backend-side mutex
// so they describe the same on-device frame; the retry exists for the
// orthogonal case where the frame itself is transitional.
//
// The transitional return reports whether the retry budget was exhausted
// on a still-transitional tree. Callers use it to skip the verifier for
// that step so the previous/current extractor advance does not absorb
// transient state.
func fetchSyncedState(ctx context.Context, options Options, logger *slog.Logger, stepIndex int) (tree *hierarchy.Tree, png []byte, transitional bool, err error) {
	var pngBytes []byte
	var previousJSON string
retryLoop:
	for attempt := range transitionalRetryAttempts {
		hierarchyJSON, image, snapshotErr := options.Driver.Snapshot(ctx)
		if snapshotErr != nil {
			err = snapshotErr
			tree = nil
		} else {
			tree, err = hierarchy.Parse(hierarchyJSON)
			pngBytes = image.PNG
		}
		if err != nil || !tree.Transitional() {
			break
		}
		// A tree unchanged since the previous attempt is a settled state
		// that merely matches the heuristic (persistent overlay, both route
		// ids alive at rest), not a cross-fade in flight: verify it instead
		// of burning the retry budget and skipping the verifier forever.
		if attempt > 0 && hierarchyJSON == previousJSON {
			break
		}
		previousJSON = hierarchyJSON
		if attempt == transitionalRetryAttempts-1 {
			transitional = true
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
	return tree, pngBytes, transitional, err
}

func traceActionFor(action verifier.Action, tree *hierarchy.Tree) *trace.Action {
	traceAction := &trace.Action{Kind: string(action.Kind), X: action.X, Y: action.Y}
	switch action.Kind {
	case verifier.ActionKindTap, verifier.ActionKindDoubleTap, verifier.ActionKindLongPress:
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
	case verifier.ActionKindScroll:
		fromX, fromY, toX, toY := scrollEndpoints(action, tree)
		traceAction.FromX = fromX
		traceAction.FromY = fromY
		traceAction.ToX = toX
		traceAction.ToY = toY
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

// stampSelectorTarget records the element bounds the selector resolved to and
// derives the tap point through resolveCoordinates, the same rule applyAction
// dispatches with, so the trace can never record a different point than the
// one tapped.
func stampSelectorTarget(traceAction *trace.Action, action verifier.Action, tree *hierarchy.Tree) {
	if action.On != "" && tree != nil {
		if element := tree.Find(action.On); element != nil {
			bounds := element.Bounds
			traceAction.ResolvedBounds = &trace.BoundsRecord{
				X:      bounds.Left,
				Y:      bounds.Top,
				Width:  bounds.Width(),
				Height: bounds.Height(),
			}
		}
	}
	if x, y, ok := resolveCoordinates(action, tree); ok {
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

// violationRecords groups newly-violated properties by the step their witness
// attributes the violation to (the causing step), falling back to the
// detection step for properties without a witness. Records are ordered by
// step; properties keep the sorted order NewlyViolatedProperties produced.
func violationRecords(properties []string, witnesses map[string]trace.Witness, detectionStep int) []ViolationRecord {
	byStep := map[int][]string{}
	for _, name := range properties {
		step := detectionStep
		if witness, ok := witnesses[name]; ok && witness.Step > 0 {
			step = witness.Step
		}
		byStep[step] = append(byStep[step], name)
	}
	records := make([]ViolationRecord, 0, len(byStep))
	for _, step := range slices.Sorted(maps.Keys(byStep)) {
		records = append(records, ViolationRecord{StepIndex: step, Properties: byStep[step]})
	}
	return records
}

// collectWitnesses gathers the violation witness for each newly-violated
// property, logs its cause, and returns them keyed by property name for the
// trace. Properties without a captured witness are skipped. stepIndex is the
// trace line the witness lands on, and stands in as the detection step for a
// verifier that observed no labeled step (a run-end finalize).
func collectWitnesses(verifierInstance *verifier.Verifier, properties []string, logger *slog.Logger, stepIndex int) map[string]trace.Witness {
	if len(properties) == 0 {
		return nil
	}
	witnesses := map[string]trace.Witness{}
	for _, name := range properties {
		witness := verifierInstance.Witness(name)
		if witness == nil {
			continue
		}
		detectedStep := witness.DetectedStep
		if detectedStep == 0 {
			detectedStep = stepIndex
		}
		logger.Warn("property violated",
			"step", witness.Step, "detected_step", detectedStep,
			"property", name, "reason", witness.Reason, "error", witness.IsError)
		witnesses[name] = trace.Witness{
			Reason:       witness.Reason,
			IsError:      witness.IsError,
			Step:         witness.Step,
			DetectedStep: detectedStep,
			Extractors:   witness.Extractors,
		}
	}
	if len(witnesses) == 0 {
		return nil
	}
	return witnesses
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

// maxConsecutiveApplyFailures bounds how many transient apply failures in a
// row the run tolerates before aborting. One or two absorb a runner restart;
// an unbroken streak means the device is wedged and the rest of the budget
// would be spent doing nothing.
const maxConsecutiveApplyFailures = 3

// isWDADrop reports that the sidecar could not restart the iOS XCTest
// runner: the channel is gone for good and the run must abort. Transient
// drops are classified by the sidecar itself (it reconnects and surfaces
// UNAVAILABLE), so matching on raw exception text like "ConnectException"
// here would kill runs the sidecar already recovered.
func isWDADrop(err error) bool {
	return strings.Contains(err.Error(), "WDA reconnect failed")
}
