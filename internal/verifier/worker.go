package verifier

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"time"

	"github.com/dop251/goja"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
	"github.com/priyanshujain/sanderling/internal/ltl"
)

type Verifier struct {
	runtime      *goja.Runtime
	extractors   []*extractorState
	formulas     []*formulaState
	formulaSpecs []formulaSpec

	properties map[string]int // property name -> formula-spec index

	// nextActionFn is the bundle-installed __sanderlingNextAction__, which runs
	// the shared picker (pick.ts) over the shared Pcg.
	nextActionFn goja.Callable

	evaluators map[string]*ltl.Evaluator

	priorVerdicts map[string]ltl.Verdict
	newlyViolated []string
	witnesses     map[string]Witness

	lastTree       *hierarchy.Tree
	lastAction     *Action
	lastLogs       []LogEntry
	lastExceptions []Exception
	stepTime       time.Time
	runStart       time.Time

	appPackage string
	platform   string
	seed       uint64
}

type Option func(*Verifier)

// WithSeed sets the 64-bit seed the JS picker constructs its Pcg from
// (new Pcg(seed, 0), matching the web bundle's SANDERLING_SEED). The verifier
// exposes it to the bundle via the __sanderlingHost__.seedHi/seedLo binds.
func WithSeed(seed uint64) Option {
	return func(v *Verifier) { v.seed = seed }
}

// WithPlatform names the platform the host reports to the picker
// ("android"/"ios"/"web"); it drives the verb-support matrix and the press-key
// pool. Empty defaults to "android".
func WithPlatform(platform string) Option {
	return func(v *Verifier) { v.platform = platform }
}

// WithAppPackage scopes random-action target selection to the app under test.
// Nodes belonging to another package (the soft keyboard, system UI, permission
// dialogs) are excluded so exploration never spends steps fuzzing the IME or
// inserting keyboard glyphs into fields. Empty package keeps current behavior.
func WithAppPackage(appPackage string) Option {
	return func(v *Verifier) { v.appPackage = appPackage }
}

func New(options ...Option) (*Verifier, error) {
	verifier := &Verifier{
		runtime:       goja.New(),
		properties:    map[string]int{},
		evaluators:    map[string]*ltl.Evaluator{},
		priorVerdicts: map[string]ltl.Verdict{},
		witnesses:     map[string]Witness{},
		platform:      "android",
	}
	for _, option := range options {
		option(verifier)
	}
	if verifier.platform == "" {
		verifier.platform = "android"
	}
	if err := verifier.installRuntimeBindings(); err != nil {
		return nil, fmt.Errorf("install bindings: %w", err)
	}
	return verifier, nil
}

// Load executes the bundled spec source. The spec is expected to assign its
// property formulas to globalThis.properties, its root action generator to
// globalThis.actions, and optionally a setup (precondition) action generator
// to globalThis.setup.
func (v *Verifier) Load(source string) error {
	if _, err := v.runtime.RunString(source); err != nil {
		return fmt.Errorf("run spec: %w", err)
	}

	propertiesValue := v.runtime.GlobalObject().Get("properties")
	if propertiesValue != nil && !goja.IsUndefined(propertiesValue) && !goja.IsNull(propertiesValue) {
		propertiesObject := propertiesValue.ToObject(v.runtime)
		for _, name := range propertiesObject.Keys() {
			handle := propertiesObject.Get(name).ToObject(v.runtime)
			if handle == nil {
				return fmt.Errorf("property %q is not an object", name)
			}
			specIndex, ok := v.extractSpecIndex(handle)
			if !ok {
				return fmt.Errorf("property %q was not produced by always()", name)
			}
			formula, err := v.buildFormula(specIndex)
			if err != nil {
				return fmt.Errorf("property %q: %w", name, err)
			}
			v.properties[name] = specIndex
			v.evaluators[name] = ltl.NewEvaluator(formula)
		}
	}

	// The bundle's goja runtime entry installs __sanderlingNextAction__ once the
	// spec assigned globalThis.actions. Capture it; a spec bundled without the
	// runtime entry (raw-JS unit fixtures) leaves it nil and NextAction reports
	// ErrNoAction.
	if fn := v.runtime.GlobalObject().Get("__sanderlingNextAction__"); fn != nil {
		if callable, ok := goja.AssertFunction(fn); ok {
			v.nextActionFn = callable
		}
	}

	return nil
}

// buildFormula walks the formula-spec registry and produces a Go ltl.Formula
// tree rooted at the given spec index. Specs built at the top level are
// always wrapped in Always unless the top-level spec is already an Always.
func (v *Verifier) buildFormula(rootIndex int) (ltl.Formula, error) {
	inner, err := v.buildFormulaNode(rootIndex)
	if err != nil {
		return nil, err
	}
	if _, ok := inner.(ltl.AlwaysFormula); ok {
		return inner, nil
	}
	return ltl.Always(inner), nil
}

func (v *Verifier) buildFormulaNode(index int) (ltl.Formula, error) {
	if index < 0 || index >= len(v.formulaSpecs) {
		return nil, fmt.Errorf("formula spec index %d out of range", index)
	}
	spec := v.formulaSpecs[index]
	switch spec.kind {
	case specKindPure:
		return ltl.Pure(spec.pureValue), nil
	case specKindThunk:
		name := fmt.Sprintf("p%d", spec.predicateIndex)
		return ltl.ThunkNamed(name, v.formulaThunk(spec.predicateIndex)), nil
	case specKindNow:
		child, err := v.buildFormulaNode(spec.childA)
		if err != nil {
			return nil, err
		}
		return ltl.Now(child), nil
	case specKindNext:
		child, err := v.buildFormulaNode(spec.childA)
		if err != nil {
			return nil, err
		}
		return ltl.Next(child), nil
	case specKindEventually:
		child, err := v.buildFormulaNode(spec.childA)
		if err != nil {
			return nil, err
		}
		formula := ltl.EventuallyFormula{Inner: child}
		if spec.hasStepBound {
			formula.StepBound = spec.stepBound
			formula.HasStepBound = true
		}
		if spec.duration > 0 {
			formula.Duration = spec.duration
		}
		return formula, nil
	case specKindImplies:
		left, err := v.buildFormulaNode(spec.childA)
		if err != nil {
			return nil, err
		}
		right, err := v.buildFormulaNode(spec.childB)
		if err != nil {
			return nil, err
		}
		return ltl.Implies(left, right), nil
	case specKindOr:
		left, err := v.buildFormulaNode(spec.childA)
		if err != nil {
			return nil, err
		}
		right, err := v.buildFormulaNode(spec.childB)
		if err != nil {
			return nil, err
		}
		return ltl.Or(left, right), nil
	case specKindAnd:
		left, err := v.buildFormulaNode(spec.childA)
		if err != nil {
			return nil, err
		}
		right, err := v.buildFormulaNode(spec.childB)
		if err != nil {
			return nil, err
		}
		return ltl.And(left, right), nil
	case specKindNot:
		child, err := v.buildFormulaNode(spec.childA)
		if err != nil {
			return nil, err
		}
		return ltl.Not(child), nil
	case specKindAlways:
		child, err := v.buildFormulaNode(spec.childA)
		if err != nil {
			return nil, err
		}
		return ltl.Always(child), nil
	default:
		return nil, fmt.Errorf("unknown formula spec kind %d", spec.kind)
	}
}

// PushSnapshot updates the JS-side state and refreshes every extractor's
// current/previous values in registration order. Passing a nil tree is
// allowed and yields an empty ax scope.
func (v *Verifier) PushSnapshot(input SnapshotInput) error {
	v.lastTree = input.Tree
	v.lastAction = input.LastAction
	v.lastLogs = input.Logs
	v.lastExceptions = input.Exceptions
	v.stepTime = input.StepTime
	if v.runStart.IsZero() {
		v.runStart = input.RunStart
	}

	state, err := stateObject(v.runtime, stateInput{
		snapshots:  input.Snapshots,
		tree:       input.Tree,
		lastAction: input.LastAction,
		stepTime:   input.StepTime,
		runStart:   v.runStart,
		logs:       input.Logs,
		exceptions: input.Exceptions,
	})
	if err != nil {
		return fmt.Errorf("build state: %w", err)
	}
	if err := v.runtime.GlobalObject().Set("state", state); err != nil {
		return fmt.Errorf("set state: %w", err)
	}
	// Extractor previous/current advance exactly once per PushSnapshot.
	// Predicate thunks read these slots but never trigger advancement, so
	// invoking a thunk multiple times between snapshots is value-stable.
	for index, extractor := range v.extractors {
		previous := extractor.handle.Get("current")
		_ = extractor.handle.Set("previous", previous)
		newValue, err := extractor.getter(goja.Undefined(), state)
		if err != nil {
			return fmt.Errorf("extractor %d: %w", index, err)
		}
		_ = extractor.handle.Set("current", newValue)
		extractor.prev = extractor.curr
		extractor.curr = encodeExtractorValue(newValue)
	}
	return nil
}

// encodeExtractorValue produces a stable JSON encoding of an extractor's
// current value for diff comparison. goja values that don't survive Export
// (e.g. wrapped host functions) yield nil; callers treat nil as "unknown" and
// emit no diff entry.
func encodeExtractorValue(value goja.Value) []byte {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return []byte("null")
	}
	exported := value.Export()
	body, err := json.Marshal(exported)
	if err != nil {
		return nil
	}
	return body
}

// ChangedExtractors returns the named extractors whose value changed between
// the prior PushSnapshot and the current one. The map is keyed by extractor
// name; unnamed extractors (extractor_N fallback) are included so the inspect
// UI can still display them under a numeric label. The very first snapshot
// emits every non-null extractor as a change (Prev=null, Curr=current) since
// the runner can otherwise misread "no diff yet" as "nothing initialized".
func (v *Verifier) ChangedExtractors() map[string]ExtractorChange {
	changes := map[string]ExtractorChange{}
	for _, extractor := range v.extractors {
		if extractor.curr == nil {
			continue
		}
		prev := extractor.prev
		if prev == nil {
			prev = []byte("null")
		}
		if bytes.Equal(prev, extractor.curr) {
			continue
		}
		changes[extractor.name] = ExtractorChange{
			Prev: append([]byte(nil), prev...),
			Curr: append([]byte(nil), extractor.curr...),
		}
	}
	return changes
}

// OverrideExtractorValues replaces each extractor's `current` slot with a
// caller-supplied value, keyed by registration index. Used by the web tick
// path so extractor bodies that ran in V8 (against the real DOM) drive the
// goja-side LTL predicates without re-running the getter against an empty
// state.ax shim. Passing a nil/empty map is a no-op so the mobile path can
// call this unconditionally. The override must run *after* PushSnapshot
// (which advanced `previous`) and *before* EvaluateProperties.
//
// Out-of-range indices are tolerated (skipped) rather than fatal: V8 and goja
// register extractors from the same spec bundle so counts should always
// match, but a stale or partial override map should not block valid overrides
// from applying. The number of skipped entries is reported so the caller can
// surface a mismatch.
func (v *Verifier) OverrideExtractorValues(overrides map[int]json.RawMessage) (skipped int, err error) {
	if len(overrides) == 0 {
		return 0, nil
	}
	for index, raw := range overrides {
		if index < 0 || index >= len(v.extractors) {
			skipped++
			continue
		}
		value, conversionErr := jsonToJSValue(v.runtime, raw)
		if conversionErr != nil {
			return skipped, fmt.Errorf("extractor override %d: %w", index, conversionErr)
		}
		_ = v.extractors[index].handle.Set("current", value)
	}
	return skipped, nil
}

// SnapshotInput bundles everything a step feeds into the verifier. Fields
// other than Snapshots are optional; callers that only have snapshots can
// populate Snapshots alone and leave the rest zero.
type SnapshotInput struct {
	Snapshots  Snapshots
	Tree       *hierarchy.Tree
	LastAction *Action
	StepTime   time.Time
	RunStart   time.Time
	Logs       []LogEntry
	Exceptions []Exception
}

// EvaluateProperties returns each registered property's running verdict
// after the most recent PushSnapshot. The step time passed in PushSnapshot is
// forwarded to each evaluator so deadline-bound operators see the snapshot's
// wall clock rather than time.Now().
//
// As a side effect, the set of properties that newly transitioned to violated
// on this call is recorded; see NewlyViolatedProperties.
func (v *Verifier) EvaluateProperties() map[string]ltl.Verdict {
	verdicts := map[string]ltl.Verdict{}
	stepTime := v.stepTime
	if stepTime.IsZero() {
		stepTime = time.Now()
	}
	for name, evaluator := range v.evaluators {
		verdicts[name] = evaluator.ObserveAt(stepTime)
	}

	var onset []string
	for name, verdict := range verdicts {
		if verdict == ltl.VerdictViolated && v.priorVerdicts[name] != ltl.VerdictViolated {
			onset = append(onset, name)
			v.captureWitness(name)
		}
	}
	sort.Strings(onset)
	v.newlyViolated = onset

	next := make(map[string]ltl.Verdict, len(verdicts))
	maps.Copy(next, verdicts)
	v.priorVerdicts = next

	return verdicts
}

// Witness is the verifier-level record of a property violation: the LTL reason
// (a predicate's thrown-error text, "predicate false", or a liveness failure),
// the step it fired at, and a snapshot of every extractor's current value at
// that step. The snapshot lets a reader see the state that produced the
// violation without replaying the run.
type Witness struct {
	Property   string
	Reason     string
	Step       int
	IsError    bool
	Extractors map[string]json.RawMessage
}

// captureWitness records the witness for a property that just transitioned to
// violated, snapshotting the current extractor values so the cause is visible
// after the run.
func (v *Verifier) captureWitness(name string) {
	evaluator, ok := v.evaluators[name]
	if !ok {
		return
	}
	violation := evaluator.Violation()
	if violation == nil {
		return
	}
	v.witnesses[name] = Witness{
		Property:   name,
		Reason:     violation.Reason,
		Step:       violation.Step,
		IsError:    violation.IsError,
		Extractors: v.extractorSnapshot(),
	}
}

// extractorSnapshot encodes every named extractor's current value as JSON. A
// nil value (extractor never advanced or its value did not survive Export)
// is recorded as JSON null.
func (v *Verifier) extractorSnapshot() map[string]json.RawMessage {
	if len(v.extractors) == 0 {
		return nil
	}
	snapshot := make(map[string]json.RawMessage, len(v.extractors))
	for _, extractor := range v.extractors {
		value := extractor.curr
		if value == nil {
			value = []byte("null")
		}
		snapshot[extractor.name] = append(json.RawMessage(nil), value...)
	}
	return snapshot
}

// Witness returns the captured violation witness for a property, or nil if the
// property has not violated. Callers consult this after EvaluateProperties (or
// Finalize) reports a violation to surface the cause and the state at onset.
func (v *Verifier) Witness(name string) *Witness {
	witness, ok := v.witnesses[name]
	if !ok {
		return nil
	}
	return &witness
}

// Finalize drives each evaluator to its terminal verdict and returns the names
// of properties that violate only at run end (a liveness obligation that never
// discharged), capturing a witness for each. Properties already violated
// mid-run are not re-reported here.
func (v *Verifier) Finalize() []string {
	var ended []string
	for name, evaluator := range v.evaluators {
		if v.priorVerdicts[name] == ltl.VerdictViolated {
			continue
		}
		if evaluator.Finalize() == ltl.VerdictViolated {
			ended = append(ended, name)
			v.captureWitness(name)
			v.priorVerdicts[name] = ltl.VerdictViolated
		}
	}
	sort.Strings(ended)
	return ended
}

// NewlyViolatedProperties returns the names of properties whose verdict
// transitioned from non-Violated to Violated on the most recent
// EvaluateProperties call, sorted lexicographically. Returns nil if no
// transition occurred or EvaluateProperties has not been called.
//
// This is the onset set: each property name appears at most once across a
// run's traces, at the step where the violation first fired. Subsequent
// steps where the property remains violated (LTL `always` sticky semantics)
// will not list it. Use this for trace emission and summary reporting so the
// onset is the only step that surfaces the violation event; use
// EvaluateProperties for residual / current-verdict needs.
func (v *Verifier) NewlyViolatedProperties() []string {
	return append([]string(nil), v.newlyViolated...)
}

// Residuals returns the residual formula for each registered property after
// the most recent EvaluateProperties call. Properties whose violation was
// caused by a thrown predicate surface as ErrorFormula, sourced from the
// captured witness, so the inspect UI can render "predicate threw" inline.
func (v *Verifier) Residuals() map[string]ltl.Formula {
	residuals := map[string]ltl.Formula{}
	for name, evaluator := range v.evaluators {
		if witness, ok := v.witnesses[name]; ok && witness.IsError {
			residuals[name] = ltl.ErrorFormula{Message: witness.Reason}
			continue
		}
		residuals[name] = evaluator.Residual()
	}
	return residuals
}

// NextAction resolves an action for the current step by invoking the bundled
// __sanderlingNextAction__(), which runs the SHARED picker (pick.ts) over the
// shared Pcg. Setup-generator precedence and the 16-attempt retry both live in
// runtime-entry.ts now, so this is a thin call-and-decode. A null result (the
// generator declined to act) reports ErrNoAction.
func (v *Verifier) NextAction() (Action, error) {
	if v.nextActionFn == nil {
		return Action{}, ErrNoAction
	}
	value, err := v.nextActionFn(goja.Undefined())
	if err != nil {
		return Action{}, fmt.Errorf("next action: %w", err)
	}
	if value == nil || goja.IsNull(value) || goja.IsUndefined(value) {
		return Action{}, ErrNoAction
	}
	raw, err := json.Marshal(value.Export())
	if err != nil {
		return Action{}, fmt.Errorf("marshal action: %w", err)
	}
	return DecodeAction(raw)
}

var ErrNoAction = errors.New("verifier: no action available")

func (v *Verifier) formulaThunk(index int) func() (bool, error) {
	return func() (bool, error) {
		formula := v.formulas[index]
		result, err := formula.predicate(goja.Undefined())
		if err != nil {
			return false, err
		}
		return result.ToBoolean(), nil
	}
}

// inScope reports whether an element belongs to the app under test. Nodes from
// another package (the soft keyboard, system UI, permission dialogs) are out of
// scope. An unset app package or an element with no package falls through to in
// scope, preserving behavior on platforms that omit the attribute (e.g. iOS).
func (v *Verifier) inScope(element *hierarchy.Element) bool {
	if v.appPackage == "" || element.Package == "" {
		return true
	}
	return element.Package == v.appPackage
}

// selectorForElement builds a canonical "key:value" selector that resolves
// back to the given element via hierarchy.Tree.Find. Prefers resource-id (the
// testTag carrier on Android / accessibilityIdentifier on iOS), falling back
// to text and content-description so action-gated properties can still tell
// what was tapped even on legacy nodes without a testTag. Returns "" when no
// candidate selector uniquely resolves to the picked element so the runner
// keeps using the action's coordinates without re-routing to a sibling that
// shares the same id/text.
func selectorForElement(tree *hierarchy.Tree, element *hierarchy.Element) string {
	if element == nil || tree == nil {
		return ""
	}
	candidates := make([]string, 0, 4)
	if element.ResourceID != "" {
		candidates = append(candidates, "id:"+element.ResourceID)
	}
	// Some platforms surface the Compose testTag only in the attributes map
	// (the sidecar doesn't always promote it to resource-id). Try the raw
	// attribute keys before falling back to text-based selectors so an
	// element with a unique testTag still gets identified.
	for _, key := range []string{"testTag", "identifier", "accessibilityIdentifier"} {
		if value := element.Attributes[key]; value != "" {
			candidates = append(candidates, key+":"+value)
		}
	}
	if element.Text != "" {
		candidates = append(candidates, "text:"+element.Text)
	}
	if element.Description != "" {
		candidates = append(candidates, "desc:"+element.Description)
	}
	for _, selector := range candidates {
		resolved := tree.Find(selector)
		if resolved == nil || resolved != element {
			continue
		}
		return selector
	}
	return ""
}

// candidatesForVerb enumerates the host-side targets a builtin verb may draw
// from, in v.lastTree.Elements ORDER (the order is part of the picker's parity
// contract). The filters are LIFTED from the old Go picker:
//   taps/doubleTaps/longPresses: clickable + enabled + positive bounds
//   typing:                      editable + enabled + positive bounds
//   scrolls:                     scrollable attribute + positive bounds
//   swipes:                      any in-scope element
// Every candidate carries the resolving selector so the runner can re-route by
// id/text. Out-of-scope nodes (the soft keyboard, system UI) are always dropped.
func (v *Verifier) candidatesForVerb(verb string) []candidate {
	if v.lastTree == nil {
		return nil
	}
	var result []candidate
	for _, element := range v.lastTree.Elements {
		if !v.inScope(element) {
			continue
		}
		if !verbAccepts(verb, element) {
			continue
		}
		x, y := element.Bounds.Center()
		result = append(result, candidate{
			x:        x,
			y:        y,
			width:    element.Bounds.Width(),
			height:   element.Bounds.Height(),
			selector: selectorForElement(v.lastTree, element),
		})
	}
	return result
}

type candidate struct {
	x, y          int
	width, height int
	selector      string
}

// verbAccepts applies the per-verb element filter.
func verbAccepts(verb string, element *hierarchy.Element) bool {
	positiveBounds := element.Bounds.Width() > 0 && element.Bounds.Height() > 0
	switch verb {
	case "taps", "doubleTaps", "longPresses":
		return element.Clickable && element.Enabled && positiveBounds
	case "typing":
		return element.Editable && element.Enabled && positiveBounds
	case "scrolls":
		return element.Attributes["scrollable"] == "true" && positiveBounds
	case "swipes":
		return true
	default:
		return false
	}
}
