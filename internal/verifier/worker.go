package verifier

import (
	"errors"
	"fmt"
	"math/rand/v2"
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

	properties      map[string]int // property name -> formula-spec index
	actionGenerator goja.Value

	evaluators map[string]*ltl.Evaluator

	lastTree       *hierarchy.Tree
	lastAction     *Action
	lastLogs       []LogEntry
	lastExceptions []Exception
	stepTime       time.Time
	runStart       time.Time

	rng *rand.Rand
}

type Option func(*Verifier)

func WithRand(rng *rand.Rand) Option {
	return func(v *Verifier) { v.rng = rng }
}

func New(options ...Option) (*Verifier, error) {
	verifier := &Verifier{
		runtime:    goja.New(),
		properties: map[string]int{},
		evaluators: map[string]*ltl.Evaluator{},
		rng:        rand.New(rand.NewPCG(0, 0)),
	}
	for _, option := range options {
		option(verifier)
	}
	if err := verifier.installRuntimeBindings(); err != nil {
		return nil, fmt.Errorf("install bindings: %w", err)
	}
	return verifier, nil
}

// Load executes the bundled spec source. The spec is expected to assign its
// property formulas to globalThis.properties and its root action generator
// to globalThis.actions.
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

	if actionsValue := v.runtime.GlobalObject().Get("actions"); actionsValue != nil && !goja.IsUndefined(actionsValue) && !goja.IsNull(actionsValue) {
		v.actionGenerator = actionsValue
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
		return ltl.Thunk(v.formulaThunk(spec.predicateIndex)), nil
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
	for index, extractor := range v.extractors {
		previous := extractor.handle.Get("current")
		_ = extractor.handle.Set("previous", previous)
		newValue, err := extractor.getter(goja.Undefined(), state)
		if err != nil {
			return fmt.Errorf("extractor %d: %w", index, err)
		}
		_ = extractor.handle.Set("current", newValue)
	}
	return nil
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
func (v *Verifier) EvaluateProperties() map[string]ltl.Verdict {
	verdicts := map[string]ltl.Verdict{}
	stepTime := v.stepTime
	if stepTime.IsZero() {
		stepTime = time.Now()
	}
	for name, evaluator := range v.evaluators {
		verdicts[name] = evaluator.ObserveAt(stepTime)
	}
	return verdicts
}

// Residuals returns the residual formula for each registered property after
// the most recent EvaluateProperties call. Properties that errored during
// predicate evaluation surface as ErrorFormula so the inspect UI can render
// "predicate threw" inline.
func (v *Verifier) Residuals() map[string]ltl.Formula {
	residuals := map[string]ltl.Formula{}
	for name, evaluator := range v.evaluators {
		if predicateErr := v.PredicateError(name); predicateErr != nil {
			residuals[name] = ltl.ErrorFormula{Message: predicateErr.Error()}
			continue
		}
		residuals[name] = evaluator.Residual()
	}
	return residuals
}

// NextAction resolves the root action generator into a single Action.
// Returns ErrNoAction when no branch of the generator produces one after a
// small number of retries. Retrying avoids wedging when most branches of a
// weighted generator produce no action on the current screen (e.g. a gated
// login-phone generator when the app is already past login).
func (v *Verifier) NextAction() (Action, error) {
	if v.actionGenerator == nil {
		return Action{}, ErrNoAction
	}
	const maxRetries = 16
	for range maxRetries {
		action, err := v.resolveGenerator(v.actionGenerator)
		if err == nil {
			return action, nil
		}
		if !errors.Is(err, ErrNoAction) {
			return Action{}, err
		}
	}
	return Action{}, ErrNoAction
}

var ErrNoAction = errors.New("verifier: no action available")

func (v *Verifier) formulaThunk(index int) func() bool {
	return func() bool {
		formula := v.formulas[index]
		result, err := formula.predicate(goja.Undefined())
		if err != nil {
			if formula.err == nil {
				formula.err = err
			}
			return false
		}
		return result.ToBoolean()
	}
}

// PredicateError returns the first goja error raised by any thunk in the
// named property's formula tree, or nil if none fired. Callers typically
// consult this after EvaluateProperties reports a violation to distinguish
// a genuine predicate-false verdict from a malformed spec.
func (v *Verifier) PredicateError(name string) error {
	rootIndex, ok := v.properties[name]
	if !ok {
		return nil
	}
	return v.firstThunkError(rootIndex)
}

func (v *Verifier) firstThunkError(index int) error {
	if index < 0 || index >= len(v.formulaSpecs) {
		return nil
	}
	spec := v.formulaSpecs[index]
	switch spec.kind {
	case specKindThunk:
		return v.formulas[spec.predicateIndex].err
	case specKindImplies, specKindOr, specKindAnd:
		if err := v.firstThunkError(spec.childA); err != nil {
			return err
		}
		return v.firstThunkError(spec.childB)
	case specKindNow, specKindNext, specKindEventually, specKindNot, specKindAlways:
		return v.firstThunkError(spec.childA)
	}
	return nil
}

func (v *Verifier) resolveGenerator(generator goja.Value) (Action, error) {
	object := generator.ToObject(v.runtime)
	if object == nil {
		return Action{}, fmt.Errorf("generator is not an object")
	}
	kindValue := object.Get(tagInternalKind)
	if kindValue == nil {
		return Action{}, fmt.Errorf("generator missing internal kind tag")
	}
	switch kindValue.String() {
	case internalKindActions:
		generateValue := object.Get("generate")
		generate, ok := goja.AssertFunction(generateValue)
		if !ok {
			return Action{}, fmt.Errorf("actions handle missing generate function")
		}
		result, err := generate(goja.Undefined())
		if err != nil {
			return Action{}, fmt.Errorf("generate: %w", err)
		}
		return v.pickFromResult(result)
	case internalKindWeighted:
		entries := object.Get("entries").ToObject(v.runtime)
		if entries == nil {
			return Action{}, fmt.Errorf("weighted handle missing entries")
		}
		picked, err := v.pickWeighted(entries)
		if err != nil {
			return Action{}, err
		}
		return v.resolveGenerator(picked)
	case internalKindBuiltinTaps:
		return v.generateRandomTap()
	case internalKindBuiltinSwipes:
		return v.generateRandomSwipe()
	case internalKindBuiltinWaitOnce:
		return Action{Kind: ActionKindWait, DurationMillis: 500}, nil
	case internalKindBuiltinPressKey:
		return v.generateRandomPressKey()
	default:
		return Action{}, fmt.Errorf("unknown generator kind %q", kindValue.String())
	}
}

// generateRandomTap picks a visible, tappable element from the last
// hierarchy snapshot and returns a Tap action targeting its center.
func (v *Verifier) generateRandomTap() (Action, error) {
	if v.lastTree == nil {
		return Action{}, ErrNoAction
	}
	candidates := make([]*hierarchy.Element, 0, len(v.lastTree.Elements))
	for _, element := range v.lastTree.Elements {
		if !element.Clickable || !element.Enabled {
			continue
		}
		if element.Bounds.Right-element.Bounds.Left <= 0 || element.Bounds.Bottom-element.Bounds.Top <= 0 {
			continue
		}
		candidates = append(candidates, element)
	}
	if len(candidates) == 0 {
		return Action{}, ErrNoAction
	}
	picked := candidates[v.rng.IntN(len(candidates))]
	x, y := picked.Bounds.Center()
	return Action{Kind: ActionKindTap, X: x, Y: y}, nil
}

// generateRandomSwipe emits a swipe over a random enabled element or the
// whole screen, in a random direction. Returns ErrNoAction only when we have
// no tree to size a gesture off of.
func (v *Verifier) generateRandomSwipe() (Action, error) {
	if v.lastTree == nil || len(v.lastTree.Elements) == 0 {
		return Action{}, ErrNoAction
	}
	element := v.lastTree.Elements[v.rng.IntN(len(v.lastTree.Elements))]
	cx, cy := element.Bounds.Center()
	if cx <= 0 || cy <= 0 {
		return Action{}, ErrNoAction
	}
	// Pick a direction: 0=up 1=down 2=left 3=right; magnitude 200-600 px.
	magnitude := 200 + v.rng.IntN(401)
	toX, toY := cx, cy
	switch v.rng.IntN(4) {
	case 0:
		toY = cy - magnitude
	case 1:
		toY = cy + magnitude
	case 2:
		toX = cx - magnitude
	case 3:
		toX = cx + magnitude
	}
	if toX < 0 {
		toX = 0
	}
	if toY < 0 {
		toY = 0
	}
	return Action{
		Kind:           ActionKindSwipe,
		FromX:          cx,
		FromY:          cy,
		ToX:            toX,
		ToY:            toY,
		DurationMillis: 250,
	}, nil
}

func (v *Verifier) generateRandomPressKey() (Action, error) {
	// Keep exploration gentle: only "back" for now. Home/menu would navigate
	// away from the app under test.
	return Action{Kind: ActionKindPressKey, Key: "back"}, nil
}

func (v *Verifier) pickFromResult(result goja.Value) (Action, error) {
	if result == nil || goja.IsUndefined(result) || goja.IsNull(result) {
		return Action{}, ErrNoAction
	}
	object := result.ToObject(v.runtime)
	if object == nil {
		return Action{}, ErrNoAction
	}
	lengthValue := object.Get("length")
	if lengthValue == nil {
		return jsValueToAction(v.runtime, result)
	}
	length := int(lengthValue.ToInteger())
	if length == 0 {
		return Action{}, ErrNoAction
	}
	pick := v.rng.IntN(length)
	return jsValueToAction(v.runtime, object.Get(fmt.Sprintf("%d", pick)))
}

func (v *Verifier) pickWeighted(entries *goja.Object) (goja.Value, error) {
	lengthValue := entries.Get("length")
	if lengthValue == nil {
		return nil, fmt.Errorf("weighted entries missing length")
	}
	length := int(lengthValue.ToInteger())
	if length == 0 {
		return nil, ErrNoAction
	}

	weights := make([]float64, length)
	generators := make([]goja.Value, length)
	totalWeight := 0.0
	for index := range length {
		entry := entries.Get(fmt.Sprintf("%d", index)).ToObject(v.runtime)
		if entry == nil {
			return nil, fmt.Errorf("weighted entry %d not an array", index)
		}
		weight := entry.Get("0").ToFloat()
		generator := entry.Get("1")
		if weight < 0 {
			weight = 0
		}
		weights[index] = weight
		generators[index] = generator
		totalWeight += weight
	}
	if totalWeight == 0 {
		return nil, ErrNoAction
	}
	pick := v.rng.Float64() * totalWeight
	cumulative := 0.0
	for index := range length {
		cumulative += weights[index]
		if pick < cumulative {
			return generators[index], nil
		}
	}
	return generators[length-1], nil
}
