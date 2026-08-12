// Package verifier runs the spec's extractors and property formulas against each observed step.
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

	// setupActionFn is the bundle-installed __sanderlingSetupAction__, which
	// walks ONLY the setup generator. The LLM action generator runs it for setup
	// precedence (e.g. login) without triggering the seeded action root.
	setupActionFn goja.Callable

	// sampleInputFn is the bundle-installed __sanderlingSampleInput__, which
	// draws one value from the shared INPUT_CORPUS. The LLM action backend uses
	// it to fill InputText values, reusing the exact corpus draw rather than
	// reimplementing the corpus on the Go side.
	sampleInputFn goja.Callable

	// enumerateBuiltinFn is the bundle-installed __sanderlingEnumerateBuiltin__,
	// which lists every action a builtin verb can yield right now. It is the same
	// enumeration the seeded picker draws from, so the LLM action backend selects
	// over the picker's action space rather than one of its own.
	enumerateBuiltinFn goja.Callable

	evaluators map[string]*ltl.Evaluator

	priorVerdicts map[string]ltl.Verdict
	newlyViolated []string
	witnesses     map[string]Witness

	lastTree        *hierarchy.Tree
	lastScreenshot  []byte
	scopeCache      map[*hierarchy.Element]bool
	scopeCacheTree  *hierarchy.Tree
	targetCache     []targetElement
	targetCacheTree *hierarchy.Tree
	lastAction      *Action
	lastLogs        []LogEntry
	lastExceptions  []Exception
	stepTime        time.Time
	stepIndex       int
	runStart        time.Time

	appPackage string
	platform   string
	seed       uint64

	// unsupported collects verbs the picker requested but the platform cannot
	// dispatch (reportUnsupported host callback), deduped and in first-seen
	// order, so the runner can surface them in the run report.
	unsupported     []string
	unsupportedSeen map[string]bool

	// extracting is true only while an extractor getter is running. The handle's
	// current/previous accessors consult it so a getter that reaches into
	// another extractor's handle throws instead of reading a stale value.
	extracting bool
}

// UnsupportedVerbs returns the verbs the picker requested that this platform
// cannot dispatch, deduped and in first-seen order.
func (v *Verifier) UnsupportedVerbs() []string {
	return v.unsupported
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
		runtime:         goja.New(),
		properties:      map[string]int{},
		evaluators:      map[string]*ltl.Evaluator{},
		priorVerdicts:   map[string]ltl.Verdict{},
		witnesses:       map[string]Witness{},
		platform:        "android",
		unsupportedSeen: map[string]bool{},
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
	if fn := v.runtime.GlobalObject().Get("__sanderlingSetupAction__"); fn != nil {
		if callable, ok := goja.AssertFunction(fn); ok {
			v.setupActionFn = callable
		}
	}

	// __sanderlingSampleInput__ draws an InputText value from the shared corpus.
	// The LLM action backend uses it; a raw-JS fixture without the runtime entry
	// leaves it nil and SampleInput reports an error.
	if fn := v.runtime.GlobalObject().Get("__sanderlingSampleInput__"); fn != nil {
		if callable, ok := goja.AssertFunction(fn); ok {
			v.sampleInputFn = callable
		}
	}

	if fn := v.runtime.GlobalObject().Get("__sanderlingEnumerateBuiltin__"); fn != nil {
		if callable, ok := goja.AssertFunction(fn); ok {
			v.enumerateBuiltinFn = callable
		}
	}

	return nil
}

// buildFormula walks the formula-spec registry and produces a Go ltl.Formula
// tree rooted at the given spec index.
//
// A top-level spec that is not already a temporal obligation is wrapped in
// Always, which is what an author writing a bare predicate or a combinator
// means. An always is left alone, and so is an eventually: wrapping
// `eventually(p).within(5, "minutes")` would turn one reachability goal into
// "within five minutes of every step", a different and far stronger property.
func (v *Verifier) buildFormula(rootIndex int) (ltl.Formula, error) {
	inner, err := v.buildFormulaNode(rootIndex)
	if err != nil {
		return nil, err
	}
	switch inner.(type) {
	case ltl.AlwaysFormula, ltl.EventuallyFormula:
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
	v.lastScreenshot = input.ScreenshotPNG
	v.scopeCache = nil
	v.lastAction = input.LastAction
	v.lastLogs = input.Logs
	v.lastExceptions = input.Exceptions
	v.stepTime = input.StepTime
	v.stepIndex = input.StepIndex
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
		extractor.previousValue = extractor.currentValue
		newValue, err := v.runExtractor(extractor, state)
		if err != nil {
			return fmt.Errorf("extractor %d: %w", index, err)
		}
		extractor.currentValue = newValue
		extractor.prev = extractor.curr
		extractor.curr = encodeExtractorValue(newValue)
	}
	return nil
}

// runExtractor invokes an extractor's getter with the extracting flag set, so a
// getter that reads another extractor's current/previous throws. The flag is
// cleared even if the getter panics.
func (v *Verifier) runExtractor(extractor *extractorState, state goja.Value) (goja.Value, error) {
	v.extracting = true
	defer func() { v.extracting = false }()
	return extractor.getter(goja.Undefined(), state)
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
// name; unnamed extractors (extractor_N fallback) are included so the replay
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
// The JSON snapshot `curr` is replaced alongside the value, so the diffs in
// ChangedExtractors and the witness recorded by captureWitness describe the
// state the verdict was computed from. Recording the goja value while a
// predicate read the V8 one makes a witness explain a violation with a state
// that never reached the property.
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
		v.extractors[index].currentValue = value
		v.extractors[index].curr = encodeExtractorValue(value)
	}
	return skipped, nil
}

// SnapshotInput bundles everything a step feeds into the verifier. Fields
// other than Snapshots are optional; callers that only have snapshots can
// populate Snapshots alone and leave the rest zero.
type SnapshotInput struct {
	Snapshots Snapshots
	Tree      *hierarchy.Tree
	// ScreenshotPNG is the step's screenshot, captured alongside Tree. The LLM
	// action backend reads it via Screenshot() to select a candidate; other
	// callers may leave it nil.
	ScreenshotPNG []byte
	LastAction    *Action
	StepTime      time.Time
	// StepIndex is the runner's step number for this snapshot. Evaluators label
	// observations with it so violation witnesses carry runner step numbers even
	// when transitional steps were skipped. Zero means unlabeled; evaluators
	// then fall back to their internal counter.
	StepIndex  int
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
		if v.stepIndex > 0 {
			verdicts[name] = evaluator.ObserveAtStep(stepTime, v.stepIndex)
		} else {
			verdicts[name] = evaluator.ObserveAt(stepTime)
		}
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
// (a predicate's thrown-error text, "predicate false", or a liveness failure)
// and the two step indices a deferred obligation spans.
//
// Step is the origin: the step whose observation armed the obligation that
// failed. DetectedStep is the observation whose reduction produced the
// violation, which for a next or an eventually is later. Extractors is that
// observation's state, so it belongs to DetectedStep and not to Step; the two
// were previously conflated under one index.
type Witness struct {
	Property     string
	Reason       string
	Step         int
	DetectedStep int
	IsError      bool
	Extractors   map[string]json.RawMessage
}

// captureWitness records the witness for a property that just transitioned to
// violated, snapshotting the current extractor values so the cause is visible
// after the run. The snapshot is the state of the observation being reduced,
// which the witness records as its detection step.
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
		Property:     name,
		Reason:       violation.Reason,
		Step:         violation.Step,
		DetectedStep: v.stepIndex,
		IsError:      violation.IsError,
		Extractors:   v.extractorSnapshot(),
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
// captured witness, so the replay UI can render "predicate threw" inline.
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

// SetupAction walks ONLY the setup generator (globalThis.setup), returning its
// action or ErrNoAction. The LLM action generator runs this for setup
// precedence (e.g. login) without triggering the seeded action root, which it
// replaces entirely. Mirrors NextAction's decode.
func (v *Verifier) SetupAction() (Action, error) {
	if v.setupActionFn == nil {
		return Action{}, ErrNoAction
	}
	value, err := v.setupActionFn(goja.Undefined())
	if err != nil {
		return Action{}, fmt.Errorf("setup action: %w", err)
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

// frameworkPackage is the AOSP framework package. Both the app's own window
// (android:id/content) and system chrome carry it, so it is treated as neutral
// (transparent) when deciding which window owns a node, rather than as a foreign
// package that would put the app's content out of scope.
const frameworkPackage = "android"

// scopedElements returns the set of elements that belong to the app under test.
// It walks the window tree propagating each node's owning package: a node's
// owner is the nearest ancestor-or-self with a concrete package (empty and the
// neutral android framework package are transparent). A node is in scope when no
// concrete foreign package owns it -- the app's own window carries no package on
// Compose apps -- or the owner is the app package itself. This drops whole
// foreign windows (the soft keyboard, system UI, the launcher) AND their
// empty-package child wrappers, e.g. a keyboard's "Settings" key, which a
// per-element package check admits because the wrapper itself has no package.
//
// With no app package configured (iOS/web, or an unscoped run) every node is in
// scope, preserving prior behavior.
func (v *Verifier) scopedElements() map[*hierarchy.Element]bool {
	if v.scopeCacheTree == v.lastTree && v.scopeCache != nil {
		return v.scopeCache
	}
	scope := make(map[*hierarchy.Element]bool, len(v.lastTree.Elements))
	unscoped := v.appPackage == ""
	if v.lastTree.Root == nil {
		for _, element := range v.lastTree.Elements {
			scope[element] = true
		}
	} else {
		var walk func(node *hierarchy.Node, owner string)
		walk = func(node *hierarchy.Node, owner string) {
			if pkg := node.Element.Package; pkg != "" && pkg != frameworkPackage {
				owner = pkg
			}
			if unscoped || owner == "" || owner == v.appPackage {
				scope[&node.Element] = true
			}
			for _, child := range node.Children {
				walk(child, owner)
			}
		}
		walk(v.lastTree.Root, "")
	}
	v.scopeCache = scope
	v.scopeCacheTree = v.lastTree
	return scope
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

// targets enumerates every element this host can offer, in v.lastTree.Elements
// ORDER (the order is part of the picker's parity contract). It is the native
// half of the single candidate producer: the picker reads it through
// __sanderlingHost__.queryTargets, applies the SHARED per-verb eligibility rule
// (pkg/spec/src/targets.ts), and expands what survives into concrete actions for
// both policies. Which verb may act on which element is decided there, once, so
// this host and the web host cannot mean different things by the same verb.
//
// This host's job is the facts: clickable/enabled/editable come off the
// accessibility node, scrollable off its attribute, and the geometry off its
// bounds. Every target carries the resolving selector so the runner can re-route
// by id/text. Out-of-scope nodes (the soft keyboard, system UI, the launcher)
// are dropped by scopedElements.
//
// A cross-fade frame yields nothing at all. Its layout is mid-animation, often
// in a collapsed coordinate space, so acting on it lands on garbage; skipping it
// here rather than in one policy means both policies re-observe a settled frame
// instead of one of them acting on the animation.
func (v *Verifier) targets() []targetElement {
	if v.lastTree == nil || v.lastTree.Transitional() {
		return nil
	}
	if v.targetCacheTree == v.lastTree {
		return v.targetCache
	}
	scope := v.scopedElements()
	result := make([]targetElement, 0, len(v.lastTree.Elements))
	for _, element := range v.lastTree.Elements {
		if !scope[element] {
			continue
		}
		x, y := element.Bounds.Center()
		result = append(result, targetElement{
			element:    element,
			x:          x,
			y:          y,
			width:      element.Bounds.Width(),
			height:     element.Bounds.Height(),
			selector:   selectorForElement(v.lastTree, element),
			clickable:  element.Clickable,
			enabled:    element.Enabled,
			editable:   element.Editable,
			scrollable: element.Attributes["scrollable"] == "true",
		})
	}
	v.targetCache = result
	v.targetCacheTree = v.lastTree
	return result
}

// targetElement is one host-enumerated element with the facts the shared
// eligibility rule reads. The host reports facts; it does not filter by verb.
type targetElement struct {
	element       *hierarchy.Element
	x, y          int
	width, height int
	selector      string
	clickable     bool
	enabled       bool
	editable      bool
	scrollable    bool
}
