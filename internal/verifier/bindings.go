package verifier

import (
	"fmt"

	"github.com/dop251/goja"
)

type ActionKind string

const (
	ActionKindTap       ActionKind = "Tap"
	ActionKindInputText ActionKind = "InputText"
)

type Action struct {
	Kind ActionKind
	On   string
	Text string
	// X, Y hold the element center when the spec passed an ax element to
	// Tap/InputText. Zero means the runner must resolve On against the
	// current hierarchy.
	X, Y int
}

type extractorState struct {
	getter goja.Callable
	handle *goja.Object
}

type formulaState struct {
	predicate goja.Callable
}

const (
	tagFormula            = "__uatuFormula"
	tagActionGenerator    = "__uatuActionGenerator"
	tagInternalKind       = "__uatuKind"
	tagSelector           = "__uatuSelector"
	internalKindActions   = "actions"
	internalKindWeighted  = "weighted"
	internalKindBuiltinTaps   = "taps"
	internalKindBuiltinSwipes = "swipes"
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
	if err := uatu.Set("actions", v.bindActions); err != nil {
		return err
	}
	if err := uatu.Set("weighted", v.bindWeighted); err != nil {
		return err
	}
	if err := uatu.Set("tap", v.bindTap); err != nil {
		return err
	}
	if err := uatu.Set("inputText", v.bindInputText); err != nil {
		return err
	}
	if err := uatu.Set("taps", v.builtinGenerator(internalKindBuiltinTaps)); err != nil {
		return err
	}
	if err := uatu.Set("swipes", v.builtinGenerator(internalKindBuiltinSwipes)); err != nil {
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

func (v *Verifier) bindAlways(call goja.FunctionCall) goja.Value {
	if len(call.Arguments) != 1 {
		panic(v.runtime.NewTypeError("always requires exactly one argument"))
	}
	predicate, ok := goja.AssertFunction(call.Arguments[0])
	if !ok {
		panic(v.runtime.NewTypeError("always argument must be a function"))
	}

	formula := &formulaState{predicate: predicate}
	v.formulas = append(v.formulas, formula)
	formulaIndex := len(v.formulas) - 1

	handle := v.runtime.NewObject()
	_ = handle.Set(tagFormula, true)
	_ = handle.Set("__uatuIndex", formulaIndex)
	return handle
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

func (v *Verifier) builtinGenerator(kind string) *goja.Object {
	handle := v.runtime.NewObject()
	_ = handle.Set(tagActionGenerator, true)
	_ = handle.Set(tagInternalKind, kind)
	return handle
}
