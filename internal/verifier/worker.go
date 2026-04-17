package verifier

import (
	"errors"
	"fmt"
	"math/rand/v2"

	"github.com/dop251/goja"

	"github.com/priyanshujain/uatu/internal/ltl"
)

type Verifier struct {
	runtime    *goja.Runtime
	extractors []*extractorState
	formulas   []*formulaState

	properties      map[string]int // property name -> formula index
	actionGenerator goja.Value

	evaluators map[string]*ltl.Evaluator

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
			indexValue := handle.Get("__uatuIndex")
			if indexValue == nil {
				return fmt.Errorf("property %q was not produced by always()", name)
			}
			index := int(indexValue.ToInteger())
			v.properties[name] = index
			v.evaluators[name] = ltl.NewEvaluator(ltl.Always(ltl.Thunk(v.formulaThunk(index))))
		}
	}

	if actionsValue := v.runtime.GlobalObject().Get("actions"); actionsValue != nil && !goja.IsUndefined(actionsValue) && !goja.IsNull(actionsValue) {
		v.actionGenerator = actionsValue
	}

	return nil
}

// PushSnapshot updates the JS-side state and refreshes every extractor's
// current/previous values in registration order.
func (v *Verifier) PushSnapshot(snapshots Snapshots) error {
	state, err := stateObject(v.runtime, snapshots)
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

// EvaluateProperties returns each registered property's running verdict
// after the most recent PushSnapshot.
func (v *Verifier) EvaluateProperties() map[string]ltl.Verdict {
	verdicts := map[string]ltl.Verdict{}
	for name, evaluator := range v.evaluators {
		verdicts[name] = evaluator.Observe()
	}
	return verdicts
}

// NextAction resolves the root action generator into a single Action.
// Returns ErrNoAction when the generator yields nothing actionable.
func (v *Verifier) NextAction() (Action, error) {
	if v.actionGenerator == nil {
		return Action{}, ErrNoAction
	}
	return v.resolveGenerator(v.actionGenerator)
}

var ErrNoAction = errors.New("verifier: no action available")

func (v *Verifier) formulaThunk(index int) func() bool {
	return func() bool {
		formula := v.formulas[index]
		result, err := formula.predicate(goja.Undefined())
		if err != nil {
			panic(fmt.Errorf("predicate panic: %w", err))
		}
		return result.ToBoolean()
	}
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
	case internalKindBuiltinTaps, internalKindBuiltinSwipes:
		return Action{}, ErrNoAction
	default:
		return Action{}, fmt.Errorf("unknown generator kind %q", kindValue.String())
	}
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
