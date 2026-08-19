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
	// LabelSource selects how candidates are named to the model picker
	// (verifier.LabelSourceVisibleText or verifier.LabelSourceResourceID). The
	// seeded picker selects by index and never reads a label, so this reaches
	// the model picker only.
	LabelSource string
}

type Summary struct {
	StartTime  time.Time
	EndTime    time.Time
	Steps      int
	Violations []ViolationRecord
	// SkippedVerification counts the steps whose tree was still moving when it
	// was read, so no property judged them. A green run that skipped most of
	// its steps checked almost nothing, and nothing else in the output would
	// say so.
	SkippedVerification int
	// UnsupportedVerbs lists verbs the picker requested that the platform
	// could not dispatch, deduped, so the report can flag a spec exercising
	// gestures this target does not support.
	UnsupportedVerbs []string
	// SkippedActions counts, by reason, the actions a step chose that never
	// reached the app. Without it a run that dropped most of what it generated
	// reads exactly like one that exercised it: the reasons reach the trace and
	// a warn line, and nothing else.
	SkippedActions map[string]int
	// FailedObservations counts the steps whose device read produced no tree at
	// all. Such a step verifies nothing, so a run that failed every observation
	// finishes with no violations and reads as a clean one.
	FailedObservations int
	// DispatchedActions counts the steps whose chosen action reached the driver.
	// A run at zero never touched the app, whatever its step count says, so its
	// empty violation list is the reading of an instrument that measured nothing.
	DispatchedActions int
	// GeneratorActions counts the dispatched actions the generator chose. The
	// spec's setup drives the app into its starting position before the
	// generator is consulted, so a run at zero here explored nothing however
	// many actions its login fired. Both generators separate the two the same
	// way, by the producer each action names on the trace.
	GeneratorActions int
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
	//
	// A run that cannot get its app on screen has no preconditions to explore
	// from: every step after this would observe some other app and every
	// property would judge it, so the run ends here and the trace records why.
	if !waitForForeground(ctx, options, logger) {
		if err := recordPreconditionFailure(options); err != nil {
			return Summary{}, err
		}
		return Summary{}, ForegroundNotReachedError{
			BundleID: options.BundleID,
			Waited:   foregroundReadyBudget,
		}
	}

	// Pick the action and extractor sources once from the driver's
	// capabilities so the step loop runs one uniform path with no per-step
	// driver type assertion.
	actionSource, extractorSource, err := pickSources(options)
	if err != nil {
		return Summary{}, err
	}
	_, pageExtractors := extractorSource.(webSource)
	exceptionReporter, _ := options.Driver.(driver.ExceptionReporter)
	navigationReporter, _ := options.Driver.(driver.NavigationReporter)
	rereadHierarchy := driverIsAndroid(ctx, options, logger)

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
		//
		// What the guard did is reported to the spec on the action it followed,
		// because dropping that action says "nothing ran between these two
		// readings" and the runner has no business saying that: the action ran,
		// and a property told otherwise convicts the app of an effect with no
		// cause. See foreground_guard_last_action_test.go.
		guard, inScope := ensureForeground(ctx, options, logger, stepIndex)
		if lastAction != nil {
			switch guard {
			case foregroundRelaunched:
				lastAction.Relaunched = true
			case foregroundOverlayDismissed:
				// A system window owned the focused window, so whether the app
				// itself ever received this action is exactly the unknown
				// Applied already has a state for.
				lastAction.Applied = false
			}
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
		// one blocked on a hung device, and to observationTimeout so a read
		// that never answers ends the step instead of the run.
		observeCtx, observeCancel := context.WithTimeout(ctx, observationTimeout)
		g, gctx := errgroup.WithContext(observeCtx)
		si := stepIndex
		// fetchSyncedState issues a single Snapshot RPC so hierarchy and
		// screenshot describe the same frame, then re-fetches the pair
		// while the tree still looks transitional.
		g.Go(func() error {
			tree, screenshotPNG, transitional, hierarchyErr = fetchSyncedState(
				gctx, options, logger, si, rereadHierarchy)
			return nil
		})
		g.Go(func() error {
			metrics = captureMetrics(gctx, options, logger, si)
			return nil
		})
		logSince := lastLogTime
		g.Go(func() error {
			logs = collectLogs(gctx, options.Driver, logger, si, logSince)
			return nil
		})
		// All goroutines write to local variables and return nil, so the Wait
		// error is always nil; ignored intentionally.
		_ = g.Wait()
		observeCancel()

		navigations := collectNavigations(ctx, navigationReporter, logger, stepIndex)

		observationError := ""
		if hierarchyErr != nil {
			if isWDADrop(hierarchyErr) {
				return summary, fmt.Errorf("WDA connection permanently lost at step %d - re-run the test: %w", stepIndex, hierarchyErr)
			}
			logger.Warn("hierarchy fetch failed", "step", stepIndex, "err", hierarchyErr)
			observationError = hierarchyErr.Error()
			summary.FailedObservations++
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

		screen := tree.ScreenName()

		// A transitional tree is one nothing can vouch for: a NavHost mid
		// cross-fade, a screen that changed shape between two reads, or a
		// hierarchy that came back empty. Pushing one would poison the
		// verifier's previous/current extractor
		// advance, so the next clean step would compare against this
		// transient state and emit false-positive violations. We still
		// record the step (hierarchy + screenshot) for replay-side
		// debugging, but skip the verifier entirely and pick the next
		// action against the unchanged prior state to keep the loop
		// progressing.
		var violations []string
		var extractorChanges map[string]trace.ExtractorChange
		var witnesses map[string]trace.Witness
		var exceptions []verifier.Exception
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
			// lastAction and logs are the same values PushSnapshot hands the
			// goja state below: the two engines evaluate this step against one
			// action and one set of log entries.
			overridesCtx, overridesCancel := context.WithTimeout(ctx, observationTimeout)
			v8Overrides, overridesErr := extractorSource.ExtractorOverrides(overridesCtx, lastAction, logs)
			overridesCancel()
			if overridesErr != nil {
				// Not a warning. Without the page's values this step's
				// extractors keep goja's dump-derived readings while the
				// previous step holds the page's, and a delta property then
				// compares two producers and fires on an app that did nothing
				// wrong.
				return summary, fmt.Errorf("step %d extractor overrides: %w", stepIndex, overridesErr)
			}
			exceptions = collectExceptions(
				ctx,
				exceptionReporter,
				logger,
				stepIndex,
			)
			if err := options.Verifier.PushSnapshot(verifier.SnapshotInput{
				Tree:          tree,
				ScreenshotPNG: screenshotPNG,
				LastAction:    lastAction,
				StepTime:      stepStart,
				StepIndex:     stepIndex,
				RunStart:      summary.StartTime,
				Logs:          logs,
				Exceptions:    exceptions,
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
			summary.SkippedVerification++
			logger.Warn("unsettled tree; skipping verifier",
				"step", stepIndex, "screen", screen, "nodes", treeSize)
		}
		// A frame the verifier would not look at is not one to act on either.
		// #75 is the fuzzer tapping into a screen that is still filling in, and
		// holding the action back is also what keeps the spec's view of the run
		// continuous: the action a step applies is reported on the NEXT step the
		// verifier accepts, so acting here would leave the action applied last
		// step unreported for good, and a property counting actions against
		// their effects would then see an effect whose cause the runner
		// swallowed. See TestRunner_ASkippedStepDoesNotSwallowTheActionBeforeIt.
		//
		// Unbounded, because lastAction holds exactly one action: any bound that
		// let the runner act again while the verifier was still being skipped
		// would overwrite the action the hold was carrying, and that is the same
		// swallow arriving one step later. A screen that keeps moving therefore
		// costs the run its actions rather than its soundness, and a run that
		// verified nothing says so in its outcome (internal/testrun).
		held := skippedVerification
		if held {
			logger.Warn("screen still moving; holding this step's action back",
				"step", stepIndex)
		}

		var nextAction verifier.Action
		nextErr := verifier.ErrNoAction
		var traceAction *trace.Action
		if !held {
			nextAction, nextErr = actionSource.NextAction(ctx, stepIndex)
			if nextErr == nil {
				traceAction = traceActionFor(nextAction, tree)
				stampActionSource(traceAction, actionSource)
			} else if !errors.Is(nextErr, verifier.ErrNoAction) {
				return summary, fmt.Errorf("step %d next action: %w", stepIndex, nextErr)
			}
		}

		residuals, residualErr := encodeResiduals(options.Verifier.Residuals())
		if residualErr != nil {
			logger.Warn("residual encode failed", "step", stepIndex, "err", residualErr)
		}

		applySkipped := held
		var actionSkipped actionSkipReason
		if nextErr != nil && !held {
			// The source was asked and handed nothing back. Recorded like every
			// other non-action, because unrecorded it is the one that survives a
			// whole run: a picker declining on all 200 steps leaves a trace, a
			// summary and an exit status a run that exercised all 200 produces.
			actionSkipped = actionSkippedNoActionProduced
			lastAction = nil
		} else if nextErr == nil && !appIsForeground(ctx, options) {
			// The app left the foreground between observe and apply (a prior
			// action's gesture settling late, or an async navigation). The
			// chosen action's coordinates reference a tree that no longer
			// applies, so firing it would act on whatever screen is now up.
			// Skip it and record the escape; the next step's guard relaunches.
			logger.Warn("app not in foreground at action time; skipping (relaunch next step)",
				"step", stepIndex, "action", nextAction.Kind)
			applySkipped = true
			actionSkipped = actionSkippedForeground
			lastAction = nil
		} else if nextErr == nil {
			applyCtx, applyCancel := context.WithTimeout(ctx, applyBound(nextAction))
			notDispatched, err := applyAction(applyCtx, options.Driver, nextAction, tree)
			applyCancel()
			if errors.Is(err, driver.ErrGestureUndelivered) {
				// The gesture reached no element, so the app cannot have
				// responded to it and the screen is still the one already
				// verified. The device is healthy, so the step is neither
				// transitional nor part of the apply-failure streak; it records
				// that the action landed on nothing, which is what separates it
				// from an action the app received and ignored.
				logger.Warn("gesture reached no element",
					"step", stepIndex, "action", nextAction.Kind, "err", err)
				applySkipped = true
				actionSkipped = actionSkippedGestureUndelivered
				lastAction = nil
			} else if errors.Is(err, driver.ErrSelectorMatchedNothing) {
				// The selector named no element, so no point was resolved and
				// nothing was dispatched. The screen is the one already
				// verified and the device is healthy, so this is the same
				// non-action the runner records when it cannot resolve a
				// selector itself, not a device fault worth a failure streak.
				logger.Warn("selector matched no element",
					"step", stepIndex, "action", nextAction.Kind, "err", err)
				applySkipped = true
				actionSkipped = actionSkippedUnresolvedSelector
				lastAction = nil
			} else if err != nil {
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
				actionSkipped = actionSkippedApplyError
				if errors.Is(applyCtx.Err(), context.DeadlineExceeded) {
					actionSkipped = actionSkippedApplyTimeout
				}
				logger.Warn("apply error; marking step transitional",
					"step", stepIndex, "reason", actionSkipped, "err", err)
				transitional = true
				applySkipped = true
				// The error says the call failed, not that the gesture never
				// reached the app: a deadline that fires after dispatch leaves
				// the effect committed. Reporting no action here would let a
				// property convict the app for an effect with no cause, so the
				// action is reported with its fate unknown instead.
				unconfirmed := verifier.RecordedAction(nextAction, tree)
				lastAction = &unconfirmed
			} else if notDispatched != "" {
				// The action was chosen but nothing reached the driver, so the
				// screen is exactly the one already verified: the step stays
				// non-transitional and only records why it acted on nothing.
				// The apply-failure streak is left alone; a step that never
				// reached the device says nothing about the device's health.
				logger.Warn("action not dispatched",
					"step", stepIndex, "action", nextAction.Kind, "reason", notDispatched)
				applySkipped = true
				actionSkipped = notDispatched
				lastAction = nil
			} else {
				consecutiveApplyFailures = 0
				applied := verifier.RecordedAction(nextAction, tree)
				applied.Applied = true
				lastAction = &applied
			}
		}
		// A held step leaves lastAction alone on purpose: nothing ran here, and
		// the action it points at is still the one the next verified step has to
		// be told about.

		logStep(logger, stepIndex, screen, treeSize, nextAction, nextErr, actionSkipped, tree)

		step := trace.Step{
			Index:               stepIndex,
			Timestamp:           stepStart,
			Screen:              screen,
			NextAction:          traceAction,
			Logs:                traceLogs(logs),
			Exceptions:          traceExceptions(exceptions),
			Navigations:         navigations,
			Violations:          violations,
			Hierarchy:           tree,
			Residuals:           residuals,
			Metrics:             metrics,
			ExtractorChanges:    extractorChanges,
			Transitional:        transitional,
			ObservationError:    observationError,
			ActionSkipped:       string(actionSkipped),
			SkippedVerification: skippedVerification,
			Witnesses:           witnesses,
			PreconditionFailure: preconditionFailure(inScope),
		}
		if err := options.TraceWriter.WriteStep(step); err != nil {
			return summary, fmt.Errorf("step %d trace: %w", stepIndex, err)
		}
		if actionSkipped != "" {
			if summary.SkippedActions == nil {
				summary.SkippedActions = map[string]int{}
			}
			summary.SkippedActions[string(actionSkipped)]++
		}
		if nextErr == nil && !applySkipped {
			summary.DispatchedActions++
			if generatorChoseAction(traceAction) {
				summary.GeneratorActions++
			}
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
		//
		// A held step settles too, and it is the only case here that waits with
		// nothing applied. The reread that held it takes its two reads a round
		// trip apart, which is a tighter window than the one the detector was
		// measured over (an action and a settle); looping straight back into it
		// would compare two reads of a composing screen closer together still,
		// so the screen that most needs to settle is the one given least room.
		mutated := nextErr == nil && !applySkipped &&
			nextAction.Kind != verifier.ActionKindWait
		if held || mutated {
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
	fmt.Fprintf(w, "\nrun complete: %d steps, %d driven by the generator\n",
		summary.Steps, summary.GeneratorActions)
	if len(summary.Violations) == 0 {
		fmt.Fprintln(w, "no violations.")
	} else {
		fmt.Fprintf(w, "%d violation record(s):\n", len(summary.Violations))
		for _, violation := range summary.Violations {
			fmt.Fprintf(w, "  step %d: %v\n", violation.StepIndex, violation.Properties)
		}
	}
	if len(summary.SkippedActions) > 0 {
		total := 0
		byReason := make([]string, 0, len(summary.SkippedActions))
		for _, reason := range slices.Sorted(maps.Keys(summary.SkippedActions)) {
			total += summary.SkippedActions[reason]
			byReason = append(byReason,
				fmt.Sprintf("%s %d", reason, summary.SkippedActions[reason]))
		}
		fmt.Fprintf(w, "%d action(s) never reached the app: %s\n",
			total, strings.Join(byReason, ", "))
	}
	if summary.FailedObservations > 0 {
		fmt.Fprintf(w, "%d step(s) observed nothing: the device state could not be read\n",
			summary.FailedObservations)
	}
	if summary.SkippedVerification > 0 {
		fmt.Fprintf(w, "%d step(s) judged by nothing: the screen was still moving when it was read\n",
			summary.SkippedVerification)
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

// foregroundGuard is what ensureForeground had to do to put the app back in
// front. The two interventions are separate values because they are separate
// facts about the action they follow: a relaunch leaves it confirmed but
// straddling a restart, while a system window holding the focus leaves it
// dispatched with no way to tell whether the app received it.
type foregroundGuard int

const (
	foregroundIntact foregroundGuard = iota
	foregroundOverlayDismissed
	foregroundRelaunched
)

// ensureForeground keeps the app under test in the foreground. When the driver
// can report the foreground app and it no longer matches the bundle under test,
// the app is relaunched. Reports what it did so the caller can pass that on to
// the spec through the previous action, and whether the app is in front at all:
// a false there is a step whose observation is not of the app under test, which
// the step records so a trace cannot pass it off as exploration. Drivers without
// ForegroundChecker (web, iOS) are a no-op.
func ensureForeground(
	ctx context.Context,
	options Options,
	logger *slog.Logger,
	stepIndex int,
) (foregroundGuard, bool) {
	checker, ok := options.Driver.(driver.ForegroundChecker)
	if !ok || options.BundleID == "" {
		return foregroundIntact, true
	}
	foreground, err := checker.ForegroundApp(ctx)
	if err != nil {
		logger.Warn("foreground check failed", "step", stepIndex, "err", err)
		return foregroundIntact, true
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
		return foregroundRelaunched, awaitForeground(ctx, options, logger, stepIndex)
	}
	// The app is the resumed activity, but a system overlay can still own the
	// focused window while the app stays resumed: a fuzzer swipe starting in the
	// status bar pulls the notification shade over the app. The resumed-activity
	// signal misses this, so observing or acting would land on the shade.
	// Dismiss it with back (which collapses the shade) so the next observe sees
	// the app again.
	focusChecker, hasFocus := options.Driver.(driver.FocusedWindowChecker)
	if !hasFocus {
		return foregroundIntact, true
	}
	focused, err := focusChecker.FocusedWindowApp(ctx)
	if err != nil {
		logger.Warn("focus check failed", "step", stepIndex, "err", err)
		return foregroundIntact, true
	}
	if focused == "" || focused == options.BundleID {
		return foregroundIntact, true
	}
	logger.Warn("system window obscuring app; dismissing",
		"step", stepIndex, "focused", focused, "want", options.BundleID)
	if err := options.Driver.PressKey(ctx, "back"); err != nil {
		logger.Warn("dismiss overlay failed", "step", stepIndex, "err", err)
	}
	settleForForeground(ctx, options)
	return foregroundOverlayDismissed, true
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

// foregroundReadyBudget bounds how long awaitForeground waits for the app to be
// on screen, so a stuck system dialog can never hang the run. It is wall-clock
// time and not a count of polls because a poll costs whatever the driver's idle
// wait happens to take, and that is a property of the device: on an API 34
// emulator settleForForeground returned in ~100ms, so eight polls gave up 1.2s
// into a launch whose window drew at ~1.9s, while on API 36 the same eight polls
// spanned 3s and cleared the same launch. Counted in polls, the gate's verdict
// describes the device it ran on rather than the app it was watching.
var foregroundReadyBudget = 15 * time.Second

// foregroundPollInterval floors how often the gate re-reads the device.
// settleForForeground returns the moment the device reports idle, which during a
// launch animation is immediately, so without a floor the gate would spin on adb
// for the whole budget.
var foregroundPollInterval = 250 * time.Millisecond

// preconditionAppNotForeground is the reason a trace step carries when the app
// under test was not in front of it: the startup gate's verdict on step 0, and
// the scope guard's on any later step it could not bring the app back for. It is
// a fixed token so a campaign counts these by decoding the trace rather than by
// grepping a log line.
const preconditionAppNotForeground = "app_not_in_foreground"

// ForegroundNotReachedError reports that the app under test never came to the
// foreground within foregroundReadyBudget, so the run never started. A run that
// ends this way has judged nothing, and reporting it as a clean run would count
// a harness failure as evidence about the app.
type ForegroundNotReachedError struct {
	BundleID string
	Waited   time.Duration
}

func (e ForegroundNotReachedError) Error() string {
	return fmt.Sprintf(
		"%s never reached the foreground within %s: nothing was observed and no property "+
			"judged anything, so this run holds no verdict about the app",
		e.BundleID, e.Waited)
}

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
func waitForForeground(ctx context.Context, options Options, logger *slog.Logger) bool {
	return awaitForeground(ctx, options, logger, 0)
}

// awaitForeground brings the app under test forward when it is not already
// resumed and blocks until its window is actually drawn, bounded by
// foregroundReadyBudget so a stuck system dialog can never hang the run. It
// re-checks the foreground each iteration and only presses back + relaunches
// while the app is genuinely absent, so once the app is resumed it polls the
// focused-window signal instead of mashing back (which would re-exit the app
// from its root screen). Shared by the pre-run startup gate (stepIndex 0) and
// the per-step scope guard so neither lets an observe or action land outside
// the app. Drivers without ForegroundChecker (web) and an unknown foreground
// both skip the gate.
//
// It reports whether the app is on screen. False means the budget ran out with
// the app confirmed absent, which is a precondition the caller has to record:
// every signal that cannot answer the question (no capability, a read error, an
// unknown foreground, a cancelled run) reports true rather than manufacture a
// failure out of a gate that never got to judge.
func awaitForeground(ctx context.Context, options Options, logger *slog.Logger, stepIndex int) bool {
	checker, ok := options.Driver.(driver.ForegroundChecker)
	if !ok || options.BundleID == "" {
		return true
	}
	focusChecker, hasFocus := options.Driver.(driver.FocusedWindowChecker)
	deadline := time.Now().Add(foregroundReadyBudget)
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return true // the run is ending on its own; the gate holds no verdict
		}
		foreground, err := checker.ForegroundApp(ctx)
		if err != nil {
			logger.Warn("foreground check failed", "step", stepIndex, "err", err)
			return true
		}
		if foreground == "" {
			return true // foreground unknowable (e.g. iOS); don't block the run
		}
		if foreground == options.BundleID {
			if !hasFocus {
				return true // resumed is the app and no finer signal exists
			}
			focused, err := focusChecker.FocusedWindowApp(ctx)
			if err != nil {
				logger.Warn("focus check failed", "step", stepIndex, "err", err)
				return true
			}
			if focused == options.BundleID {
				return true // window is drawn; safe to observe
			}
			if !time.Now().Before(deadline) {
				break
			}
			logger.Warn("app resumed but window not yet drawn; waiting",
				"step", stepIndex, "focused", focused, "want", options.BundleID, "attempt", attempt)
			awaitNextForegroundPoll(ctx, options)
			continue
		}
		if !time.Now().Before(deadline) {
			break
		}
		logger.Warn("app not in foreground; bringing it forward",
			"step", stepIndex, "foreground", foreground, "want", options.BundleID, "attempt", attempt)
		bringToForeground(ctx, options, logger, stepIndex)
		awaitNextForegroundPoll(ctx, options)
	}
	logger.Warn("app never reached foreground", "step", stepIndex,
		"want", options.BundleID, "waited", foregroundReadyBudget)
	return false
}

// awaitNextForegroundPoll waits out one poll interval before the gate re-reads
// the device: one settle, then whatever is left of foregroundPollInterval, so a
// driver whose idle wait returns immediately cannot turn the gate into a spin.
func awaitNextForegroundPoll(ctx context.Context, options Options) {
	start := time.Now()
	settleForForeground(ctx, options)
	remaining := foregroundPollInterval - time.Since(start)
	if remaining <= 0 {
		return
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// bringToForeground returns the app under test to the foreground. It first
// presses BACK to dismiss any modal system dialog (a relaunch alone does not
// close one), then relaunches. The caller waits out the poll interval before
// looking again, so a relaunch that fails outright cannot spin the gate.
func bringToForeground(ctx context.Context, options Options, logger *slog.Logger, stepIndex int) {
	if err := options.Driver.PressKey(ctx, "back"); err != nil {
		logger.Warn("dismiss key before relaunch failed", "step", stepIndex, "err", err)
	}
	if err := options.Driver.Launch(ctx, options.BundleID, false, nil); err != nil {
		logger.Warn("relaunch failed", "step", stepIndex, "err", err)
	}
}

// preconditionFailure names what a step could not assume, empty when it could.
func preconditionFailure(inScope bool) string {
	if inScope {
		return ""
	}
	return preconditionAppNotForeground
}

// recordPreconditionFailure writes the startup gate's verdict to the trace as
// step 0, the step index no observation ever uses, so a run that never started
// is countable off the trace instead of off a warn line in a log.
func recordPreconditionFailure(options Options) error {
	step := trace.Step{
		Index:               0,
		Timestamp:           time.Now(),
		PreconditionFailure: preconditionAppNotForeground,
	}
	if err := options.TraceWriter.WriteStep(step); err != nil {
		return fmt.Errorf("write precondition failure to trace: %w", err)
	}
	return nil
}

// settleForForeground waits one idle window for the UI to settle, bounding the
// wait by the driver's idle timeout.
func settleForForeground(ctx context.Context, options Options) {
	idleCtx, cancel := context.WithTimeout(ctx, options.IdleTimeout)
	_ = options.Driver.WaitForIdle(idleCtx, options.IdleTimeout)
	cancel()
}

// applyAction dispatches one chosen action to the driver. The returned reason is
// empty exactly when the driver was called; a non-empty reason means nothing was
// dispatched and names why, so the step can record that it acted on nothing
// instead of showing a next_action that looks executed.
func applyAction(ctx context.Context, drv driver.DeviceDriver, action verifier.Action, tree *hierarchy.Tree) (actionSkipReason, error) {
	switch action.Kind {
	case verifier.ActionKindTap:
		x, y, ok := resolveCoordinates(action, tree)
		if !ok {
			return "", drv.TapSelector(ctx, action.On)
		}
		return "", drv.Tap(ctx, x, y)
	case verifier.ActionKindDoubleTap:
		x, y, ok := resolveCoordinates(action, tree)
		if !ok {
			return "", drv.DoubleTapSelector(ctx, action.On)
		}
		return "", drv.DoubleTap(ctx, x, y)
	case verifier.ActionKindLongPress:
		x, y, ok := resolveCoordinates(action, tree)
		if !ok {
			// No long-press-by-selector RPC exists, so a selector that resolves
			// to no coordinates is nothing we can dispatch.
			return actionSkippedUnresolvedSelector, nil
		}
		return "", drv.LongPress(ctx, x, y)
	case verifier.ActionKindScroll:
		fromX, fromY, toX, toY := scrollEndpoints(action, tree)
		fromX, fromY, toX, toY = clampGestureToSafeArea(fromX, fromY, toX, toY, screenBounds(tree))
		duration := time.Duration(action.DurationMillis) * time.Millisecond
		if duration <= 0 {
			duration = 300 * time.Millisecond
		}
		if scroller, ok := drv.(driver.Scroller); ok {
			return "", scroller.Scroll(ctx, fromX, fromY, toX, toY, duration)
		}
		return "", drv.Swipe(ctx, fromX, fromY, toX, toY, duration)
	case verifier.ActionKindInputText:
		tapped := false
		if x, y, ok := resolveCoordinates(action, tree); ok {
			if err := drv.Tap(ctx, x, y); err != nil {
				return "", err
			}
			tapped = true
		} else if action.On != "" {
			if err := drv.TapSelector(ctx, action.On); err != nil {
				return "", err
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
				return "", ctx.Err()
			case <-timer.C:
			}
			if err := confirmFocus(ctx, drv, action.On, tree); err != nil {
				return "", err
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
					return "", err
				}
			}
		}
		return "", drv.InputText(ctx, action.Text)
	case verifier.ActionKindSwipe:
		duration := time.Duration(action.DurationMillis) * time.Millisecond
		if duration <= 0 {
			duration = 250 * time.Millisecond
		}
		fromX, fromY, toX, toY := clampGestureToSafeArea(action.FromX, action.FromY, action.ToX, action.ToY, screenBounds(tree))
		return "", drv.Swipe(ctx, fromX, fromY, toX, toY, duration)
	case verifier.ActionKindPressKey:
		if action.Key == "" {
			return actionSkippedMissingKey, nil
		}
		return "", drv.PressKey(ctx, action.Key)
	case verifier.ActionKindWait:
		duration := time.Duration(action.DurationMillis) * time.Millisecond
		if duration <= 0 {
			return actionSkippedZeroDurationWait, nil
		}
		timer := time.NewTimer(duration)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timer.C:
			return "", nil
		}
	default:
		return "", fmt.Errorf("unknown action kind %q", action.Kind)
	}
}

// collectLogs pulls recent error-level log entries from the driver since the
// previous fetch. A failure is warned-on but not fatal: one unreadable fetch on
// a flaky device should not end a run. It is not free either. This fetch is the
// whole evidence base for state.logs, so a step that could not make it leaves
// every log property (the default noLogcatErrors included) holding on an empty
// slice, and that has to be visible in the run's output rather than read as the
// app having logged nothing.
func collectLogs(
	ctx context.Context,
	drv driver.DeviceDriver,
	logger *slog.Logger,
	step int,
	since time.Time,
) []verifier.LogEntry {
	entries, err := drv.RecentLogs(ctx, since, "E")
	if err != nil {
		logger.Warn("log fetch failed; log properties hold vacuously this step",
			"step", step, "err", err)
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

// collectExceptions reads the app's captured uncaught errors. Like log
// capture it is best-effort: a failed read is warned on rather than ending
// the run, and a driver that cannot report them yields none.
func collectExceptions(
	ctx context.Context,
	reporter driver.ExceptionReporter,
	logger *slog.Logger,
	stepIndex int,
) []verifier.Exception {
	if reporter == nil {
		return nil
	}
	captured, err := reporter.Exceptions(ctx)
	if err != nil {
		logger.Warn("exception fetch failed", "step", stepIndex, "err", err)
		return nil
	}
	result := make([]verifier.Exception, 0, len(captured))
	for _, entry := range captured {
		result = append(result, verifier.Exception{
			Class:      entry.Class,
			Message:    entry.Message,
			StackTrace: entry.StackTrace,
			UnixMillis: entry.UnixMillis,
		})
	}
	return result
}

func collectNavigations(
	ctx context.Context,
	reporter driver.NavigationReporter,
	logger *slog.Logger,
	stepIndex int,
) []trace.Navigation {
	if reporter == nil {
		return nil
	}
	observed, err := reporter.Navigations(ctx)
	if err != nil {
		logger.Warn("navigation fetch failed", "step", stepIndex, "err", err)
		return nil
	}
	records := make([]trace.Navigation, 0, len(observed))
	for _, entry := range observed {
		records = append(records, trace.Navigation{URL: entry.URL, UnixMillis: entry.UnixMillis})
	}
	if len(records) == 0 {
		return nil
	}
	return records
}

func traceLogs(entries []verifier.LogEntry) []trace.LogEntry {
	if len(entries) == 0 {
		return nil
	}
	result := make([]trace.LogEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, trace.LogEntry(entry))
	}
	return result
}

func traceExceptions(entries []verifier.Exception) []trace.Exception {
	if len(entries) == 0 {
		return nil
	}
	result := make([]trace.Exception, 0, len(entries))
	for _, entry := range entries {
		result = append(result, trace.Exception(entry))
	}
	return result
}

// inputReplacesText reports whether the driver's InputText replaces existing
// content, making the runner's pre-erase redundant.
func inputReplacesText(drv driver.DeviceDriver) bool {
	replacer, ok := drv.(driver.TextReplacer)
	return ok && replacer.ReplacesTextOnInput()
}

// confirmFocus fails the action when the device reports focus on an element
// other than the one the focus tap aimed at. Typing is a blind write to
// whatever holds focus, so a tap the target never received (a keyboard overlay
// window covering it, a target that cannot take focus) would stream the
// characters into a different field, corrupting it and every property that
// reads it. Only a field that already holds focus can receive that text, so
// the confirming read is charged only when the pre-tap hierarchy shows focus
// somewhere other than the target: a target that already holds focus, a screen
// with nothing focused, and platforms that never report focus (iOS) all skip
// it and keep the round-trip.
func confirmFocus(
	ctx context.Context,
	drv driver.DeviceDriver,
	selector string,
	tree *hierarchy.Tree,
) error {
	if selector == "" || !otherElementHoldsFocus(tree, selector) {
		return nil
	}
	dump, err := drv.Hierarchy(ctx)
	if err != nil {
		return fmt.Errorf("focus check for %s: %w", selector, err)
	}
	current, err := hierarchy.Parse(dump)
	if err != nil {
		return fmt.Errorf("focus check for %s: %w", selector, err)
	}
	if !otherElementHoldsFocus(current, selector) {
		return nil
	}
	return fmt.Errorf(
		"focus tap on %s did not focus it: %s holds focus, so the text would land there",
		selector, elementName(focusedElement(current)),
	)
}

// otherElementHoldsFocus reports whether the hierarchy shows focus on
// something outside the selector's subtree, which is the state that sends
// typed text to the wrong field. A selector the hierarchy cannot resolve
// answers false: not knowing where the target is says nothing about where the
// text would land, and failing on it turns every step the dump has no node for
// into an apply error, which is an aborted run three steps later.
func otherElementHoldsFocus(tree *hierarchy.Tree, selector string) bool {
	if tree == nil || focusedElement(tree) == nil {
		return false
	}
	target := tree.FindNode(selector)
	return target != nil && !holdsFocus(target)
}

func focusedElement(tree *hierarchy.Tree) *hierarchy.Element {
	for _, element := range tree.Elements {
		if element.Focused {
			return element
		}
	}
	return nil
}

// holdsFocus accepts focus anywhere in the target's subtree: a selector often
// names the field wrapper while the platform reports focus on the inner
// editable node.
func holdsFocus(node *hierarchy.Node) bool {
	if node.Focused {
		return true
	}
	for _, child := range node.Children {
		if holdsFocus(child) {
			return true
		}
	}
	return false
}

func elementName(element *hierarchy.Element) string {
	switch {
	case element.ResourceID != "":
		return element.ResourceID
	case element.Description != "":
		return element.Description
	case element.Class != "":
		return element.Class
	default:
		return "an unnamed element"
	}
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
	// even at (0,0)). A point outside the viewport is off screen, not absent:
	// only the driver knows whether it can scroll that point back into reach,
	// so the judgement belongs there and not here. When On is set, prefer the
	// tree lookup so stale coords don't leak from earlier ticks.
	if action.On == "" {
		return action.X, action.Y, true
	}
	if tree != nil {
		// An ambiguous selector names several elements while the action's own
		// coordinates name one, so the coordinates win. Attribute values match
		// by substring, so a selector unique where the candidate was built can
		// be ambiguous in the tree it resolves against. A bare-string target
		// carries no coordinates, and there the name is all there is.
		matches := tree.FindAll(action.On)
		hasCoordinates := action.X > 0 && action.Y > 0
		if len(matches) > 0 && (len(matches) == 1 || !hasCoordinates) {
			x, y := matches[0].Bounds.Center()
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
// on a still-transitional tree, or (when reread is set) whether a second
// hierarchy read disagreed with the first. Callers use it to skip the verifier
// for that step so the previous/current extractor advance does not absorb
// transient state.
func fetchSyncedState(
	ctx context.Context,
	options Options,
	logger *slog.Logger,
	stepIndex int,
	reread bool,
) (tree *hierarchy.Tree, png []byte, transitional bool, err error) {
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
	if reread && err == nil && !transitional && changedOnReread(ctx, options, logger, stepIndex, tree) {
		transitional = true
	}
	if len(pngBytes) > 0 {
		if writeErr := options.TraceWriter.WriteScreenshot(stepIndex, pngBytes); writeErr != nil {
			logger.Warn("screenshot write failed", "step", stepIndex, "err", writeErr)
		}
	}
	return tree, pngBytes, transitional, err
}

// changedOnReread reads the hierarchy once more and reports whether the screen
// changed shape while we were looking at it. A Compose route can settle before
// its content composes (a lazy list mounts over several frames, a query lands a
// frame late), and a tree read in that window describes a screen that is still
// filling in. Two reads a read apart are the cheapest thing that can see it
// happening: the round trip IS the interval, so there is no sleep here.
//
// The comparison only means anything because the Hierarchy RPC serves the tree
// the snapshot's own read produces (see snapshotTree in the sidecar). Off the
// bare device read it does not: with an IME standing open, the snapshot answers
// with 134 nodes and the bare read with 489, and the pair then differs over
// whether the sidecar closed a keyboard between them rather than over anything
// the app did.
//
// Waiting for the change to stop was measured on an API 34 device and refused:
// a 750ms-quiet poll capped at 2s cost a median 1434ms against 76ms for one
// read, hit its cap on every frame it fired for, and still handed back a frame
// that might be filling. Detecting is what the runner can act on, because a
// step it declines to verify is at worst a missed conviction, never a false
// one.
//
// A read that fails reports no change. Nothing about a dropped RPC says the
// screen was moving, and skipping verification on it would quietly spend the
// run's evidence on a flaky link.
func changedOnReread(
	ctx context.Context,
	options Options,
	logger *slog.Logger,
	stepIndex int,
	first *hierarchy.Tree,
) bool {
	// An empty tree is skipped by the caller anyway, so the read buys nothing.
	if first == nil || len(first.Elements) == 0 {
		return false
	}
	hierarchyJSON, err := options.Driver.Hierarchy(ctx)
	if err != nil {
		logger.Warn("second hierarchy read failed", "step", stepIndex, "err", err)
		return false
	}
	second, err := hierarchy.Parse(hierarchyJSON)
	if err != nil || second == nil {
		logger.Warn("second hierarchy parse failed", "step", stepIndex, "err", err)
		return false
	}
	if structuralShape(first) == structuralShape(second) {
		return false
	}
	logger.Warn("screen changed between two reads; skipping verifier",
		"step", stepIndex, "nodes", len(first.Elements), "then", len(second.Elements))
	return true
}

// structuralShape renders what is on screen as its nodes' identities in tree
// order: how many there are, and which ids and classes they carry.
//
// Text and bounds are deliberately absent. A measure pass that moves pixels is
// not a screen still composing, and neither is a value arriving into a node
// that already exists, which this cannot tell apart from a clock ticking. This
// decides whether a property gets to judge at all, so it reads only what a
// change in what is on screen can move: a detector that fires on every step of
// a screen with a timer on it would leave the run green and vacuous, which is
// worse than the composition it set out to catch. The trade is measured rather
// than assumed: over 100 folio steps on an API 35 emulator, text moved under
// an unchanged shape on 1 step, and the shape itself moved on 1 other.
//
// TestRunner_OnlyAChangeOfShapeCostsAStepItsVerdict is what holds the line:
// adding either field back to the shape turns one of its cases red.
func structuralShape(tree *hierarchy.Tree) string {
	var shape strings.Builder
	for _, element := range tree.Elements {
		shape.WriteString(element.ResourceID)
		shape.WriteByte(0x1f)
		shape.WriteString(element.Class)
		shape.WriteByte(0x1e)
	}
	return shape.String()
}

// driverIsAndroid asks the driver what it is, once per run, so the step loop
// never repeats the RPC. It gates the reread: #75 is about Compose composition,
// and web and iOS have their own settle paths and no measurement saying an
// extra hierarchy read there is cheap. An unreadable answer is not android.
func driverIsAndroid(ctx context.Context, options Options, logger *slog.Logger) bool {
	health, err := options.Driver.Health(ctx)
	if err != nil {
		logger.Warn("health read failed; not rereading the hierarchy", "err", err)
		return false
	}
	return health.Platform == "android"
}

// logStep prints the one line a run emits per step: what screen it saw and what
// it did there. The typed value goes through the same redaction the trace and
// the prompt use, so the console cannot publish a credential the records
// withhold.
func logStep(
	logger *slog.Logger,
	stepIndex int,
	screen string,
	treeSize int,
	action verifier.Action,
	actionErr error,
	skipped actionSkipReason,
	tree *hierarchy.Tree,
) {
	attrs := []any{"index", stepIndex, "screen", screen, "nodes", treeSize}
	if actionErr != nil {
		attrs = append(attrs, "action", "none")
	} else {
		attrs = append(attrs, "action", string(action.Kind), "target", actionTarget(action))
		if action.Kind == verifier.ActionKindInputText {
			attrs = append(attrs, "text", verifier.RecordedActionText(action, tree))
		}
	}
	if skipped != "" {
		attrs = append(attrs, "skipped", string(skipped))
	}
	logger.Info("step", attrs...)
}

func traceActionFor(action verifier.Action, tree *hierarchy.Tree) *trace.Action {
	traceAction := &trace.Action{
		Kind:   string(action.Kind),
		X:      action.X,
		Y:      action.Y,
		Source: action.Source,
	}
	switch action.Kind {
	case verifier.ActionKindTap, verifier.ActionKindDoubleTap, verifier.ActionKindLongPress:
		traceAction.Selector = action.On
		stampSelectorTarget(traceAction, action, tree)
	case verifier.ActionKindInputText:
		traceAction.Text = verifier.RecordedActionText(action, tree)
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

// actionSkipReason names why a chosen action was never dispatched. It is
// recorded on the step so a count of executed actions is not inflated by the
// next_action of a step that acted on nothing. Empty means the action ran.
type actionSkipReason string

const (
	actionSkippedForeground actionSkipReason = "app_left_foreground"
	actionSkippedApplyError actionSkipReason = "apply_error"
	// The action was dispatched and the driver never came back inside the
	// step's bound. Distinct from apply_error, which is a call that answered
	// and said no, and from the reasons below, which are actions that never
	// reached the device at all.
	actionSkippedApplyTimeout actionSkipReason = "apply_timeout"
	// The action named a selector that resolved to no on-screen coordinates,
	// either because its verb has no by-selector dispatch to fall back to or
	// because the driver's own lookup found nothing to tap.
	actionSkippedUnresolvedSelector actionSkipReason = "unresolved_selector"
	actionSkippedMissingKey         actionSkipReason = "missing_key"
	actionSkippedZeroDurationWait   actionSkipReason = "zero_duration_wait"
	// The driver resolved the action's point and found no element there, so
	// the gesture was never dispatched. Recorded rather than counted as a
	// device fault: a run that acts on nothing has to say so.
	actionSkippedGestureUndelivered actionSkipReason = "gesture_undelivered"
	// The step asked the action source for an action and was handed none: a
	// generator with no candidate for this screen, or a model call that failed.
	// The step then drives nothing, which is the one non-action a run can take
	// on every one of its steps and still finish reporting a full step count.
	actionSkippedNoActionProduced actionSkipReason = "no_action_produced"
)

// maxConsecutiveApplyFailures bounds how many transient apply failures in a
// row the run tolerates before aborting. One or two absorb a runner restart;
// an unbroken streak means the device is wedged and the rest of the budget
// would be spent doing nothing.
const maxConsecutiveApplyFailures = 3

// applyTimeout bounds one dispatched action and observationTimeout the device
// reads a step opens with. Options.Duration is a loop condition checked between
// steps and the drivers add no deadline of their own, so without these a call
// that never returns holds the run for as long as the process lives. Both are
// far above any healthy call and far below the timeout a campaign runner puts
// on a whole run. Variables so the timeout tests can shrink them.
var (
	applyTimeout       = 60 * time.Second
	observationTimeout = 60 * time.Second
)

// applyBound is how long one dispatched action may take. An action that names
// its own duration carries it on top: the bound exists to end a call that
// stopped answering, not to cut a gesture the spec asked for.
func applyBound(action verifier.Action) time.Duration {
	return applyTimeout + time.Duration(action.DurationMillis)*time.Millisecond
}

// isWDADrop reports that the sidecar could not restart the iOS XCTest
// runner: the channel is gone for good and the run must abort. Transient
// drops are classified by the sidecar itself (it reconnects and surfaces
// UNAVAILABLE), so matching on raw exception text like "ConnectException"
// here would kill runs the sidecar already recovered.
func isWDADrop(err error) bool {
	return strings.Contains(err.Error(), "WDA reconnect failed")
}
