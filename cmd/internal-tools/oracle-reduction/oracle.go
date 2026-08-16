package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/priyanshujain/sanderling/internal/ltl"
	"github.com/priyanshujain/sanderling/internal/trace"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

// foregroundLossReason is the skip reason internal/runner records when the app
// was no longer the foreground process at action time. It is the only
// foreground signal a stored trace carries, and it is what the crash-only
// oracle reads for "the application is no longer the foreground process".
const foregroundLossReason = "app_left_foreground"

// refutation is one oracle's finding for one property on one trace. A reduced
// oracle has three of them and not two: CannotExpress says its rewrite no
// longer states the property, which is neither a refutation nor a clean bill.
type refutation struct {
	Refuted       bool `json:"refuted"`
	CannotExpress bool `json:"cannot_express,omitempty"`
	// Step is the observation whose evaluation produced the violation;
	// OriginStep is the observation whose obligation failed, which for a
	// deferred one is earlier.
	Step       int    `json:"step,omitempty"`
	OriginStep int    `json:"origin_step,omitempty"`
	Reason     string `json:"reason,omitempty"`
	IsError    bool   `json:"is_error,omitempty"`
}

type propertyReport struct {
	Property    string     `json:"property"`
	Class       string     `json:"class"`
	TopLevel    string     `json:"top_level"`
	Engine      refutation `json:"engine"`
	SingleState refutation `json:"single_state"`
	SingleStep  refutation `json:"single_step"`
	// SingleStepTruncatesWindow marks a property whose window the triple had to
	// shorten to two observations, so what its single-step column refutes is a
	// stronger property than the one the author wrote.
	SingleStepTruncatesWindow bool `json:"single_step_truncates_window,omitempty"`
	// Weakest names the weakest oracle that refutes this property on this
	// trace, and is empty unless the engine refuted it. "temporal-only" means
	// no reduced oracle did.
	Weakest string `json:"weakest_refuting_oracle,omitempty"`
}

type crashReport struct {
	Fired               bool  `json:"fired"`
	FirstStep           int   `json:"first_step,omitempty"`
	ExceptionSteps      []int `json:"exception_steps,omitempty"`
	ForegroundLossSteps []int `json:"foreground_loss_steps,omitempty"`
	ErrorLogSteps       []int `json:"error_log_steps,omitempty"`
}

// mismatch is one disagreement between the offline engine and the verdicts the
// run recorded. Any of these blocks the trace's result.
type mismatch struct {
	Property string `json:"property"`
	Field    string `json:"field"`
	Online   string `json:"online"`
	Offline  string `json:"offline"`
}

type runReport struct {
	Run       string `json:"run"`
	Seed      int64  `json:"seed"`
	Platform  string `json:"platform"`
	Arm       string `json:"arm,omitempty"`
	Generator string `json:"generator,omitempty"`
	// ReplayMode says where the extractor values under replay came from:
	// "page-extractor-values" reuses what the page computed in V8 and the run
	// evaluated against, "host-extractors" re-runs the spec's getters over the
	// stored hierarchy.
	ReplayMode    string     `json:"replay_mode"`
	StepsObserved int        `json:"steps_observed"`
	StepsSkipped  int        `json:"steps_skipped"`
	Valid         bool       `json:"valid"`
	Mismatches    []mismatch `json:"mismatches,omitempty"`
	// ResidualMismatches counts every step whose replayed residual formula
	// differed from the recorded one, of which Mismatches carries the first
	// few. The recorded residual is the engine's whole pending state, so
	// agreement on it is a stronger claim than agreement on the verdicts.
	ResidualMismatches int              `json:"residual_mismatches"`
	CrashOnly          crashReport      `json:"crash_only"`
	Properties         []propertyReport `json:"properties"`
	ActionsUnbounded   int              `json:"actions_without_resolved_bounds"`
	ScrollActions      int              `json:"scroll_last_actions"`
}

// witnessRecord is one violation as either side reports it: the step it was
// recorded at, plus the witness the engine attached.
type witnessRecord struct {
	RecordedStep int
	OriginStep   int
	DetectedStep int
	Reason       string
	IsError      bool
}

// replay re-evaluates one run offline under all four oracles. The engine's
// offline verdicts are compared against the ones the run recorded, and the
// report is marked invalid on any disagreement: a mismatch is a bug here or a
// gap in the trace, not a finding.
func replay(
	run loadedRun,
	bundleJavaScript string,
	hostExtractors bool,
) (runReport, error) {
	engine, err := verifier.New(
		verifier.WithSeed(uint64(run.Meta.Seed)),
		verifier.WithPlatform(run.Meta.Platform),
		verifier.WithAppPackage(run.Meta.BundleID),
	)
	if err != nil {
		return runReport{}, fmt.Errorf("verifier: %w", err)
	}
	if err := engine.Load(bundleJavaScript); err != nil {
		return runReport{}, fmt.Errorf("load spec: %w", err)
	}
	formulas, err := engine.PropertyFormulas()
	if err != nil {
		return runReport{}, err
	}

	singleState := map[string]*ltl.Evaluator{}
	singleStep := map[string]*ltl.Evaluator{}
	for name, formula := range formulas {
		singleState[name] = ltl.NewEvaluator(singleStateFormula(formula))
		singleStep[name] = ltl.NewEvaluator(singleStepFormula(formula))
	}

	usePageValues := run.Meta.Platform == "web" && !hostExtractors
	report := runReport{
		Run:        run.Directory,
		Seed:       run.Meta.Seed,
		Platform:   run.Meta.Platform,
		Arm:        run.Meta.Arm,
		Generator:  run.Meta.Generator,
		ReplayMode: "host-extractors",
	}
	var folded []map[int]json.RawMessage
	if usePageValues {
		report.ReplayMode = "page-extractor-values"
		folded, err = extractorFold(run.Steps, engine.ExtractorNames())
		if err != nil {
			return runReport{}, err
		}
	}

	offline := map[string]witnessRecord{}
	stateFired := map[string]int{}
	stepFired := map[string]int{}
	var residualMismatches []mismatch
	var lastAction *verifier.Action
	for position, step := range run.Steps {
		if step.NextAction != nil && step.NextAction.Selector != "" &&
			step.NextAction.ResolvedBounds == nil {
			report.ActionsUnbounded++
		}
		if step.SkippedVerification {
			report.StepsSkipped++
			residualMismatches = append(
				residualMismatches,
				compareResiduals(step, engine.Residuals())...)
			lastAction = lastActionFor(step)
			continue
		}
		if lastAction != nil && lastAction.Kind == verifier.ActionKindScroll {
			report.ScrollActions++
		}
		if err := engine.PushSnapshot(verifier.SnapshotInput{
			Tree:       step.Hierarchy,
			LastAction: lastAction,
			StepTime:   step.Timestamp,
			StepIndex:  step.Index,
			RunStart:   run.Meta.StartedAt,
			Logs:       traceLogs(step.Logs),
			Exceptions: traceExceptions(step.Exceptions),
		}); err != nil {
			return runReport{}, fmt.Errorf("step %d push: %w", step.Index, err)
		}
		if usePageValues {
			skipped, overrideErr := engine.OverrideExtractorValues(
				folded[position],
			)
			if overrideErr != nil {
				return runReport{}, fmt.Errorf(
					"step %d override: %w",
					step.Index,
					overrideErr,
				)
			}
			if skipped > 0 {
				return runReport{}, fmt.Errorf(
					"step %d: %d recorded extractor values fell outside the spec's extractor list",
					step.Index,
					skipped,
				)
			}
		}
		engine.EvaluateProperties()
		for _, name := range engine.NewlyViolatedProperties() {
			offline[name] = witnessFrom(engine.Witness(name), step.Index)
		}
		residualMismatches = append(
			residualMismatches,
			compareResiduals(step, engine.Residuals())...)
		for name := range formulas {
			recordFiring(
				stateFired,
				name,
				singleState[name].ObserveAtStep(step.Timestamp, step.Index),
				step.Index,
			)
			recordFiring(
				stepFired,
				name,
				singleStep[name].ObserveAtStep(step.Timestamp, step.Index),
				step.Index,
			)
		}
		report.StepsObserved++
		lastAction = lastActionFor(step)
	}

	finalizeIndex := run.finalizeIndex()
	for _, name := range engine.Finalize() {
		offline[name] = witnessFrom(engine.Witness(name), finalizeIndex)
	}
	for name := range formulas {
		recordFiring(
			stateFired,
			name,
			singleState[name].Finalize(),
			finalizeIndex,
		)
		recordFiring(
			stepFired,
			name,
			singleStep[name].Finalize(),
			finalizeIndex,
		)
	}

	report.CrashOnly = crashOnly(run)
	report.Mismatches = compareVerdicts(onlineVerdicts(run), offline)
	report.ResidualMismatches = len(residualMismatches)
	if len(residualMismatches) > reportedResiduals {
		residualMismatches = residualMismatches[:reportedResiduals]
	}
	report.Mismatches = append(report.Mismatches, residualMismatches...)
	report.Valid = len(report.Mismatches) == 0

	names := make([]string, 0, len(formulas))
	for name := range formulas {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		property := propertyReport{
			Property: name,
			Class:    propertyClass(formulas[name]),
			TopLevel: topLevelForm(formulas[name]),
			Engine:   engineRefutation(offline, name),
			SingleState: reducedRefutation(
				singleStateExpresses(formulas[name]),
				singleState[name],
				stateFired[name],
			),
			SingleStep: reducedRefutation(
				singleStepExpresses(formulas[name]),
				singleStep[name],
				stepFired[name],
			),

			SingleStepTruncatesWindow: truncatesWindow(formulas[name]),
		}
		property.Weakest = weakestOracle(property, report.CrashOnly)
		report.Properties = append(report.Properties, property)
	}
	return report, nil
}

// reportedResiduals caps how many residual differences one trace lists. The
// count of all of them is reported alongside; the first few are what says
// where the two engines parted.
const reportedResiduals = 5

// compareResiduals checks the replayed pending state against the one the step
// recorded. A step the verifier skipped still recorded the residual it was
// holding, so those steps assert that a skipped step advanced nothing.
func compareResiduals(
	step trace.Step,
	replayed map[string]ltl.Formula,
) []mismatch {
	if len(step.Residuals) == 0 {
		return nil
	}
	var mismatches []mismatch
	names := make([]string, 0, len(step.Residuals))
	for name := range step.Residuals {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		formula, ok := replayed[name]
		if !ok {
			mismatches = append(mismatches, mismatch{
				Property: name,
				Field:    fmt.Sprintf("residual at step %d", step.Index),
				Online:   string(step.Residuals[name]),
				Offline:  "property not registered",
			})
			continue
		}
		encoded, err := json.Marshal(formula)
		if err != nil {
			encoded = []byte(fmt.Sprintf("%q", err.Error()))
		}
		if !bytes.Equal(encoded, step.Residuals[name]) {
			mismatches = append(mismatches, mismatch{
				Property: name,
				Field:    fmt.Sprintf("residual at step %d", step.Index),
				Online:   string(step.Residuals[name]),
				Offline:  string(encoded),
			})
		}
	}
	return mismatches
}

func witnessFrom(witness *verifier.Witness, recordedStep int) witnessRecord {
	record := witnessRecord{RecordedStep: recordedStep}
	if witness == nil {
		return record
	}
	record.OriginStep = witness.Step
	record.DetectedStep = witness.DetectedStep
	record.Reason = witness.Reason
	record.IsError = witness.IsError
	return record
}

// onlineVerdicts reads the violations the run recorded, including the
// end-of-run record a finalized liveness obligation is written to.
func onlineVerdicts(run loadedRun) map[string]witnessRecord {
	recorded := map[string]witnessRecord{}
	steps := run.Steps
	if run.Finalize != nil {
		steps = append(append([]trace.Step(nil), steps...), *run.Finalize)
	}
	for _, step := range steps {
		for _, name := range step.Violations {
			record := witnessRecord{RecordedStep: step.Index}
			if witness, ok := step.Witnesses[name]; ok {
				record.OriginStep = witness.Step
				record.DetectedStep = witness.DetectedStep
				record.Reason = witness.Reason
				record.IsError = witness.IsError
			}
			recorded[name] = record
		}
	}
	return recorded
}

func compareVerdicts(online, offline map[string]witnessRecord) []mismatch {
	var mismatches []mismatch
	names := map[string]bool{}
	for name := range online {
		names[name] = true
	}
	for name := range offline {
		names[name] = true
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)

	for _, name := range ordered {
		recorded, wasRecorded := online[name]
		replayed, wasReplayed := offline[name]
		switch {
		case wasRecorded && !wasReplayed:
			mismatches = append(mismatches, mismatch{
				Property: name, Field: "violated",
				Online: fmt.Sprintf(
					"violated at step %d",
					recorded.RecordedStep,
				),
				Offline: "not violated",
			})
			continue
		case !wasRecorded && wasReplayed:
			mismatches = append(mismatches, mismatch{
				Property: name, Field: "violated",
				Online: "not violated",
				Offline: fmt.Sprintf(
					"violated at step %d",
					replayed.RecordedStep,
				),
			})
			continue
		case !wasRecorded:
			continue
		}
		for _, field := range []struct {
			name    string
			online  string
			offline string
		}{
			{"step", fmt.Sprint(recorded.RecordedStep), fmt.Sprint(replayed.RecordedStep)},
			{"origin_step", fmt.Sprint(recorded.OriginStep), fmt.Sprint(replayed.OriginStep)},
			{"detected_step", fmt.Sprint(recorded.DetectedStep), fmt.Sprint(replayed.DetectedStep)},
			{"reason", recorded.Reason, replayed.Reason},
		} {
			if field.online != field.offline {
				mismatches = append(mismatches, mismatch{
					Property: name, Field: field.name,
					Online: field.online, Offline: field.offline,
				})
			}
		}
	}
	return mismatches
}

// crashOnly fires where the application left the foreground or an error
// surface was recorded. Error-level log lines are reported alongside rather
// than folded in: a console error is not a crash, and an analysis that wants
// the looser detector can read the steps from here.
func crashOnly(run loadedRun) crashReport {
	report := crashReport{}
	for _, step := range run.Steps {
		if len(step.Exceptions) > 0 {
			report.ExceptionSteps = append(report.ExceptionSteps, step.Index)
		}
		if step.ActionSkipped == foregroundLossReason {
			report.ForegroundLossSteps = append(
				report.ForegroundLossSteps,
				step.Index,
			)
		}
		for _, entry := range step.Logs {
			if entry.Level == "E" || entry.Level == "F" {
				report.ErrorLogSteps = append(report.ErrorLogSteps, step.Index)
				break
			}
		}
	}
	first := 0
	for _, step := range append(append([]int(nil), report.ExceptionSteps...), report.ForegroundLossSteps...) {
		if first == 0 || step < first {
			first = step
		}
	}
	report.Fired = first != 0
	report.FirstStep = first
	return report
}

func engineRefutation(
	offline map[string]witnessRecord,
	name string,
) refutation {
	record, ok := offline[name]
	if !ok {
		return refutation{}
	}
	return refutation{
		Refuted:    true,
		Step:       record.RecordedStep,
		OriginStep: record.OriginStep,
		Reason:     record.Reason,
		IsError:    record.IsError,
	}
}

// reducedRefutation reports a reduced oracle's finding, or its inability to
// state the property at all. The evaluator is driven either way so that the
// verdict a reduction would have reached is never what decides whether it was
// entitled to reach one.
func reducedRefutation(
	expresses bool,
	evaluator *ltl.Evaluator,
	firedAt int,
) refutation {
	if !expresses {
		return refutation{CannotExpress: true}
	}
	return evaluatorRefutation(evaluator, firedAt)
}

func evaluatorRefutation(evaluator *ltl.Evaluator, firedAt int) refutation {
	violation := evaluator.Violation()
	if violation == nil {
		return refutation{}
	}
	return refutation{
		Refuted:    true,
		Step:       firedAt,
		OriginStep: violation.Step,
		Reason:     violation.Reason,
		IsError:    violation.IsError,
	}
}

// recordFiring keeps the first observation at which a reduced oracle latched,
// which the evaluator itself does not carry.
func recordFiring(
	fired map[string]int,
	name string,
	verdict ltl.Verdict,
	step int,
) {
	if verdict != ltl.VerdictViolated {
		return
	}
	if _, ok := fired[name]; ok {
		return
	}
	fired[name] = step
}

// weakestOracle names the weakest oracle that refutes a defect the engine
// refuted, in the order a crash detector, a single-state check and a
// single-step triple are weaker than the engine.
func weakestOracle(property propertyReport, crash crashReport) string {
	if !property.Engine.Refuted {
		return ""
	}
	switch {
	case crash.Fired:
		return "crash-only"
	case property.SingleState.Refuted:
		return "single-state"
	case property.SingleStep.Refuted:
		return "single-step"
	default:
		return "temporal-only"
	}
}
