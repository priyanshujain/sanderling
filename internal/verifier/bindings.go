package verifier

import (
	"fmt"
	"math/big"
	"time"

	"github.com/dop251/goja"
)

type extractorState struct {
	getter goja.Callable
	name   string
	// currentValue/previousValue back the handle's current/previous accessors.
	currentValue  goja.Value
	previousValue goja.Value
	// prev/curr cache the JSON-encoded extractor values from the prior and
	// current PushSnapshot, used by ChangedExtractors to surface per-step
	// diffs in the trace.
	prev []byte
	curr []byte
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
	tagFormula          = "__sanderlingFormula"
	tagFormulaSpecIndex = "__sanderlingFormulaSpec"
	tagSelector         = "__sanderlingSelector"
)

// installRuntimeBindings exposes globalThis.__sanderling__ to the loaded spec.
func (v *Verifier) installRuntimeBindings() error {
	sanderling := v.runtime.NewObject()

	if err := sanderling.Set("extract", v.bindExtract); err != nil {
		return err
	}
	if err := sanderling.Set("always", v.bindAlways); err != nil {
		return err
	}
	if err := sanderling.Set("now", v.bindNow); err != nil {
		return err
	}
	if err := sanderling.Set("next", v.bindNext); err != nil {
		return err
	}
	if err := sanderling.Set("eventually", v.bindEventually); err != nil {
		return err
	}

	if err := v.runtime.GlobalObject().Set("__sanderling__", sanderling); err != nil {
		return err
	}
	return v.installHost()
}

// installHost exposes globalThis.__sanderlingHost__ for the goja runtime entry.
// The shared picker (pick.ts) draws against it: platform() drives the verb
// matrix and press-key pool; seedHi/seedLo construct its Pcg; queryCandidates
// enumerates targets over the hierarchy tree; reportUnsupported records the
// verb for the run report.
func (v *Verifier) installHost() error {
	host := v.runtime.NewObject()
	if err := host.Set("platform", func(goja.FunctionCall) goja.Value {
		return v.runtime.ToValue(v.platform)
	}); err != nil {
		return err
	}
	if err := host.Set("seedHi", func(goja.FunctionCall) goja.Value {
		return v.runtime.ToValue(new(big.Int).SetUint64(v.seed))
	}); err != nil {
		return err
	}
	if err := host.Set("seedLo", func(goja.FunctionCall) goja.Value {
		return v.runtime.ToValue(big.NewInt(0))
	}); err != nil {
		return err
	}
	if err := host.Set("queryCandidates", v.bindQueryCandidates); err != nil {
		return err
	}
	if err := host.Set("reportUnsupported", func(call goja.FunctionCall) goja.Value {
		v.recordUnsupported(call.Argument(0).String())
		return goja.Undefined()
	}); err != nil {
		return err
	}
	return v.runtime.GlobalObject().Set("__sanderlingHost__", host)
}

// recordUnsupported notes a verb the picker requested that this platform
// cannot dispatch, deduped and in first-seen order.
func (v *Verifier) recordUnsupported(verb string) {
	if verb == "" || v.unsupportedSeen[verb] {
		return
	}
	v.unsupportedSeen[verb] = true
	v.unsupported = append(v.unsupported, verb)
}

// bindQueryCandidates returns the host-enumerated targets for a verb as an
// array of {x, y, selector, width, height}, in tree order.
func (v *Verifier) bindQueryCandidates(call goja.FunctionCall) goja.Value {
	verb := call.Argument(0).String()
	candidates := v.candidatesForVerb(verb)
	array := v.runtime.NewArray()
	for index, candidate := range candidates {
		item := v.runtime.NewObject()
		_ = item.Set("x", candidate.x)
		_ = item.Set("y", candidate.y)
		_ = item.Set("width", candidate.width)
		_ = item.Set("height", candidate.height)
		if candidate.selector != "" {
			_ = item.Set("selector", candidate.selector)
		}
		_ = array.Set(fmt.Sprintf("%d", index), item)
	}
	return array
}

func (v *Verifier) bindExtract(call goja.FunctionCall) goja.Value {
	if len(call.Arguments) < 1 || len(call.Arguments) > 2 {
		panic(v.runtime.NewTypeError("extract requires (getter) or (getter, name)"))
	}
	getter, ok := goja.AssertFunction(call.Arguments[0])
	if !ok {
		panic(v.runtime.NewTypeError("extract argument must be a function"))
	}
	name := ""
	if len(call.Arguments) == 2 {
		arg := call.Arguments[1]
		if !goja.IsUndefined(arg) && !goja.IsNull(arg) {
			name = arg.String()
		}
	}
	if name == "" {
		name = fmt.Sprintf("extractor_%d", len(v.extractors))
	}

	state := &extractorState{
		getter:        getter,
		name:          name,
		currentValue:  goja.Undefined(),
		previousValue: goja.Undefined(),
	}

	handle := v.runtime.NewObject()
	_ = handle.DefineAccessorProperty("current", v.runtime.ToValue(func(goja.FunctionCall) goja.Value {
		v.checkNotExtracting("current")
		return state.currentValue
	}), nil, goja.FLAG_FALSE, goja.FLAG_TRUE)
	_ = handle.DefineAccessorProperty("previous", v.runtime.ToValue(func(goja.FunctionCall) goja.Value {
		v.checkNotExtracting("previous")
		return state.previousValue
	}), nil, goja.FLAG_FALSE, goja.FLAG_TRUE)
	_ = handle.Set("named", func(call goja.FunctionCall) goja.Value {
		state.name = call.Argument(0).String()
		return handle
	})

	v.extractors = append(v.extractors, state)
	return handle
}

// checkNotExtracting panics with a JS error when an extractor getter tries to
// read another extractor handle's current/previous. The message is identical to
// the web runtime's so authors see one diagnostic across engines.
func (v *Verifier) checkNotExtracting(slot string) {
	if v.extracting {
		panic(v.runtime.NewGoError(fmt.Errorf(
			"reading .%s of an extractor inside another extractor is not allowed; extractor getters may read only from the state argument",
			slot,
		)))
	}
}

// bindAlways accepts either a predicate function (legacy shape) or a formula
// handle (new shape). Both produce a formula handle tagged with
// __sanderlingFormulaSpec.
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

// extractSpecIndex reads __sanderlingFormulaSpec from a JS formula handle.
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
