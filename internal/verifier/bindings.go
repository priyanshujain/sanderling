package verifier

import (
	"fmt"
	"time"

	"github.com/dop251/goja"
)

type ActionKind string

const (
	ActionKindTap       ActionKind = "Tap"
	ActionKindInputText ActionKind = "InputText"
	ActionKindSwipe     ActionKind = "Swipe"
	ActionKindPressKey  ActionKind = "PressKey"
	ActionKindWait      ActionKind = "Wait"
)

type Action struct {
	Kind ActionKind
	On   string
	Text string
	// X, Y hold the element center when the spec passed an ax element to
	// Tap/InputText. Zero means the runner must resolve On against the
	// current hierarchy.
	X, Y int
	// Swipe coordinates (raw px). Used only for ActionKindSwipe.
	FromX, FromY int
	ToX, ToY     int
	// DurationMillis is the Swipe gesture duration or the Wait duration.
	DurationMillis int
	// Key is the logical key name for ActionKindPressKey.
	Key string
}

type extractorState struct {
	getter goja.Callable
	handle *goja.Object
}

type formulaState struct {
	predicate goja.Callable
}

type specKind int

const (
	specKindPure specKind = iota
	specKindThunk
	specKindNow
	specKindNext
	specKindEventually
	specKindImplies
	specKindOr
	specKindAnd
	specKindNot
	specKindAlways
)

// formulaSpec is the Go-side registry entry that mirrors a chainable JS
// formula handle. Handles reference specs by index; chaining creates new
// specs that reference their operands' indices.
type formulaSpec struct {
	kind specKind

	pureValue      bool
	predicateIndex int

	childA int
	childB int

	stepBound    int
	hasStepBound bool
	duration     time.Duration
}

const (
	tagFormula          = "__uatuFormula"
	tagFormulaSpecIndex = "__uatuFormulaSpec"
	tagActionGenerator  = "__uatuActionGenerator"
	tagInternalKind     = "__uatuKind"
	tagSelector         = "__uatuSelector"

	internalKindActions         = "actions"
	internalKindWeighted        = "weighted"
	internalKindBuiltinTaps     = "taps"
	internalKindBuiltinSwipes   = "swipes"
	internalKindBuiltinWaitOnce = "waitOnce"
	internalKindBuiltinPressKey = "pressKey"
)

// installRuntimeBindings exposes globalThis.__uatu__ to the loaded spec.
func (v *Verifier) installRuntimeBindings() error {
	uatu := v.runtime.NewObject()

	if err := uatu.Set("extract", v.bindExtract); err != nil {
		return err
	}
	if err := uatu.Set("always", v.bindAlways); err != nil {
		return err
	}
	if err := uatu.Set("now", v.bindNow); err != nil {
		return err
	}
	if err := uatu.Set("next", v.bindNext); err != nil {
		return err
	}
	if err := uatu.Set("eventually", v.bindEventually); err != nil {
		return err
	}
	if err := uatu.Set("actions", v.bindActions); err != nil {
		return err
	}
	if err := uatu.Set("weighted", v.bindWeighted); err != nil {
		return err
	}
	if err := uatu.Set("from", v.bindFrom); err != nil {
		return err
	}
	if err := uatu.Set("tap", v.bindTap); err != nil {
		return err
	}
	if err := uatu.Set("inputText", v.bindInputText); err != nil {
		return err
	}
	if err := uatu.Set("swipe", v.bindSwipe); err != nil {
		return err
	}
	if err := uatu.Set("pressKey", v.bindPressKey); err != nil {
		return err
	}
	if err := uatu.Set("wait", v.bindWait); err != nil {
		return err
	}
	if err := uatu.Set("taps", v.builtinGenerator(internalKindBuiltinTaps)); err != nil {
		return err
	}
	if err := uatu.Set("swipes", v.builtinGenerator(internalKindBuiltinSwipes)); err != nil {
		return err
	}
	if err := uatu.Set("waitOnce", v.builtinGenerator(internalKindBuiltinWaitOnce)); err != nil {
		return err
	}
	if err := uatu.Set("pressKeys", v.builtinGenerator(internalKindBuiltinPressKey)); err != nil {
		return err
	}

	return v.runtime.GlobalObject().Set("__uatu__", uatu)
}

func (v *Verifier) bindExtract(call goja.FunctionCall) goja.Value {
	if len(call.Arguments) != 1 {
		panic(v.runtime.NewTypeError("extract requires exactly one argument"))
	}
	getter, ok := goja.AssertFunction(call.Arguments[0])
	if !ok {
		panic(v.runtime.NewTypeError("extract argument must be a function"))
	}

	handle := v.runtime.NewObject()
	_ = handle.Set("current", goja.Undefined())
	_ = handle.Set("previous", goja.Undefined())

	v.extractors = append(v.extractors, &extractorState{getter: getter, handle: handle})
	return handle
}

// bindAlways accepts either a predicate function (legacy shape) or a formula
// handle (new shape). Both produce a formula handle tagged with
// __uatuFormulaSpec.
func (v *Verifier) bindAlways(call goja.FunctionCall) goja.Value {
	if len(call.Arguments) != 1 {
		panic(v.runtime.NewTypeError("always requires exactly one argument"))
	}
	arg := call.Arguments[0]
	if predicate, ok := goja.AssertFunction(arg); ok {
		thunkIndex := v.registerThunk(predicate)
		return v.makeFormulaHandle(specKindAlways, formulaSpec{
			kind:   specKindAlways,
			childA: thunkIndex,
		})
	}
	childIndex, ok := v.extractSpecIndex(arg)
	if !ok {
		panic(v.runtime.NewTypeError("always argument must be a predicate or formula"))
	}
	return v.makeFormulaHandle(specKindAlways, formulaSpec{
		kind:   specKindAlways,
		childA: childIndex,
	})
}

func (v *Verifier) bindNow(call goja.FunctionCall) goja.Value {
	thunkIndex := v.requirePredicate(call, "now")
	return v.makeFormulaHandle(specKindNow, formulaSpec{
		kind:   specKindNow,
		childA: thunkIndex,
	})
}

func (v *Verifier) bindNext(call goja.FunctionCall) goja.Value {
	thunkIndex := v.requirePredicate(call, "next")
	return v.makeFormulaHandle(specKindNext, formulaSpec{
		kind:   specKindNext,
		childA: thunkIndex,
	})
}

func (v *Verifier) bindEventually(call goja.FunctionCall) goja.Value {
	thunkIndex := v.requirePredicate(call, "eventually")
	return v.makeFormulaHandle(specKindEventually, formulaSpec{
		kind:   specKindEventually,
		childA: thunkIndex,
	})
}

func (v *Verifier) requirePredicate(call goja.FunctionCall, name string) int {
	if len(call.Arguments) != 1 {
		panic(v.runtime.NewTypeError(name + " requires exactly one argument"))
	}
	predicate, ok := goja.AssertFunction(call.Arguments[0])
	if !ok {
		panic(v.runtime.NewTypeError(name + " argument must be a function"))
	}
	return v.registerThunk(predicate)
}

// registerThunk stores a predicate in v.formulas and returns its index, which
// reduce can later invoke via formulaThunk.
func (v *Verifier) registerThunk(predicate goja.Callable) int {
	spec := formulaSpec{kind: specKindThunk}
	// predicateIndex points into v.formulas, which is a parallel slice.
	spec.predicateIndex = len(v.formulas)
	v.formulas = append(v.formulas, &formulaState{predicate: predicate})
	v.formulaSpecs = append(v.formulaSpecs, spec)
	return len(v.formulaSpecs) - 1
}

// registerSpec appends a spec and returns its index.
func (v *Verifier) registerSpec(spec formulaSpec) int {
	v.formulaSpecs = append(v.formulaSpecs, spec)
	return len(v.formulaSpecs) - 1
}

// makeFormulaHandle registers the spec and returns a JS handle exposing
// chainable combinators. Eventually handles additionally expose .within.
func (v *Verifier) makeFormulaHandle(kind specKind, spec formulaSpec) *goja.Object {
	index := v.registerSpec(spec)
	return v.formulaHandle(kind, index)
}

func (v *Verifier) formulaHandle(kind specKind, index int) *goja.Object {
	handle := v.runtime.NewObject()
	_ = handle.Set(tagFormula, true)
	_ = handle.Set(tagFormulaSpecIndex, index)
	// Keep __uatuIndex as an alias so older property shapes that read it keep
	// working during backward-compat transitions.
	_ = handle.Set("__uatuIndex", index)

	_ = handle.Set("implies", v.binaryChain(index, specKindImplies))
	_ = handle.Set("or", v.binaryChain(index, specKindOr))
	_ = handle.Set("and", v.binaryChain(index, specKindAnd))
	_ = handle.Set("not", v.unaryChain(index, specKindNot))

	if kind == specKindEventually {
		_ = handle.Set("within", v.eventuallyWithin(index))
	}

	return handle
}

func (v *Verifier) binaryChain(selfIndex int, kind specKind) func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) != 1 {
			panic(v.runtime.NewTypeError("operator requires exactly one argument"))
		}
		otherIndex, ok := v.extractSpecIndex(call.Arguments[0])
		if !ok {
			panic(v.runtime.NewTypeError("operator argument must be a formula"))
		}
		return v.makeFormulaHandle(kind, formulaSpec{
			kind:   kind,
			childA: selfIndex,
			childB: otherIndex,
		})
	}
}

func (v *Verifier) unaryChain(selfIndex int, kind specKind) func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		return v.makeFormulaHandle(kind, formulaSpec{
			kind:   kind,
			childA: selfIndex,
		})
	}
}

func (v *Verifier) eventuallyWithin(selfIndex int) func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) != 2 {
			panic(v.runtime.NewTypeError("within requires (amount, unit)"))
		}
		amount := call.Argument(0).ToInteger()
		unit := call.Argument(1).String()
		base := v.formulaSpecs[selfIndex]
		if base.kind != specKindEventually {
			panic(v.runtime.NewTypeError("within only applies to eventually"))
		}
		switch unit {
		case "steps":
			base.stepBound = int(amount)
			base.hasStepBound = true
		case "milliseconds":
			base.duration = time.Duration(amount) * time.Millisecond
		case "seconds":
			base.duration = time.Duration(amount) * time.Second
		default:
			panic(v.runtime.NewTypeError("within unit must be 'milliseconds', 'seconds', or 'steps'"))
		}
		return v.makeFormulaHandle(specKindEventually, base)
	}
}

// extractSpecIndex reads __uatuFormulaSpec from a JS formula handle.
func (v *Verifier) extractSpecIndex(value goja.Value) (int, bool) {
	if value == nil || goja.IsNull(value) || goja.IsUndefined(value) {
		return 0, false
	}
	object := value.ToObject(v.runtime)
	if object == nil {
		return 0, false
	}
	indexValue := object.Get(tagFormulaSpecIndex)
	if indexValue == nil || goja.IsUndefined(indexValue) {
		return 0, false
	}
	return int(indexValue.ToInteger()), true
}

func (v *Verifier) bindActions(call goja.FunctionCall) goja.Value {
	if len(call.Arguments) != 1 {
		panic(v.runtime.NewTypeError("actions requires a single generator argument"))
	}
	if _, ok := goja.AssertFunction(call.Arguments[0]); !ok {
		panic(v.runtime.NewTypeError("actions argument must be a function"))
	}
	handle := v.runtime.NewObject()
	_ = handle.Set(tagActionGenerator, true)
	_ = handle.Set(tagInternalKind, internalKindActions)
	_ = handle.Set("generate", call.Arguments[0])
	return handle
}

func (v *Verifier) bindWeighted(call goja.FunctionCall) goja.Value {
	entries := v.runtime.NewArray()
	for index, argument := range call.Arguments {
		object := argument.ToObject(v.runtime)
		if object == nil {
			panic(v.runtime.NewTypeError(fmt.Sprintf("weighted entry %d must be a [number, generator] tuple", index)))
		}
		if err := entries.Set(fmt.Sprintf("%d", index), object); err != nil {
			panic(v.runtime.NewGoError(err))
		}
	}
	handle := v.runtime.NewObject()
	_ = handle.Set(tagActionGenerator, true)
	_ = handle.Set(tagInternalKind, internalKindWeighted)
	_ = handle.Set("entries", entries)
	return handle
}

// bindFrom returns a `{ generate }` that picks uniformly at random from the
// provided items using the verifier's seeded rng.
func (v *Verifier) bindFrom(call goja.FunctionCall) goja.Value {
	if len(call.Arguments) != 1 {
		panic(v.runtime.NewTypeError("from requires an array argument"))
	}
	itemsValue := call.Arguments[0]
	itemsObject := itemsValue.ToObject(v.runtime)
	if itemsObject == nil {
		panic(v.runtime.NewTypeError("from argument must be an array"))
	}
	lengthValue := itemsObject.Get("length")
	if lengthValue == nil {
		panic(v.runtime.NewTypeError("from argument must be array-like"))
	}
	length := int(lengthValue.ToInteger())

	handle := v.runtime.NewObject()
	_ = handle.Set("generate", func(goja.FunctionCall) goja.Value {
		if length == 0 {
			return goja.Undefined()
		}
		index := v.rng.IntN(length)
		return itemsObject.Get(fmt.Sprintf("%d", index))
	})
	return handle
}

func (v *Verifier) bindTap(call goja.FunctionCall) goja.Value {
	parameters := call.Argument(0).ToObject(v.runtime)
	if parameters == nil {
		panic(v.runtime.NewTypeError("Tap requires {on}"))
	}
	handle := v.runtime.NewObject()
	_ = handle.Set("kind", "Tap")
	_ = handle.Set("on", parameters.Get("on"))
	return handle
}

func (v *Verifier) bindInputText(call goja.FunctionCall) goja.Value {
	parameters := call.Argument(0).ToObject(v.runtime)
	if parameters == nil {
		panic(v.runtime.NewTypeError("InputText requires {into, text}"))
	}
	handle := v.runtime.NewObject()
	_ = handle.Set("kind", "InputText")
	_ = handle.Set("into", parameters.Get("into"))
	_ = handle.Set("text", parameters.Get("text"))
	return handle
}

func (v *Verifier) bindSwipe(call goja.FunctionCall) goja.Value {
	parameters := call.Argument(0).ToObject(v.runtime)
	if parameters == nil {
		panic(v.runtime.NewTypeError("Swipe requires {from, to}"))
	}
	handle := v.runtime.NewObject()
	_ = handle.Set("kind", "Swipe")
	_ = handle.Set("from", parameters.Get("from"))
	_ = handle.Set("to", parameters.Get("to"))
	if duration := parameters.Get("durationMillis"); duration != nil && !goja.IsUndefined(duration) {
		_ = handle.Set("durationMillis", duration)
	}
	return handle
}

func (v *Verifier) bindPressKey(call goja.FunctionCall) goja.Value {
	parameters := call.Argument(0).ToObject(v.runtime)
	if parameters == nil {
		panic(v.runtime.NewTypeError("PressKey requires {key}"))
	}
	handle := v.runtime.NewObject()
	_ = handle.Set("kind", "PressKey")
	_ = handle.Set("key", parameters.Get("key"))
	return handle
}

func (v *Verifier) bindWait(call goja.FunctionCall) goja.Value {
	parameters := call.Argument(0).ToObject(v.runtime)
	if parameters == nil {
		panic(v.runtime.NewTypeError("Wait requires {durationMillis}"))
	}
	handle := v.runtime.NewObject()
	_ = handle.Set("kind", "Wait")
	_ = handle.Set("durationMillis", parameters.Get("durationMillis"))
	return handle
}

func (v *Verifier) builtinGenerator(kind string) *goja.Object {
	handle := v.runtime.NewObject()
	_ = handle.Set(tagActionGenerator, true)
	_ = handle.Set(tagInternalKind, kind)
	return handle
}
