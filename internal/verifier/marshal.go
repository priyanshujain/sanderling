package verifier

import (
	"encoding/json"
	"fmt"

	"github.com/dop251/goja"

	"github.com/priyanshujain/uatu/internal/hierarchy"
)

// Snapshots is the per-step extractor output forwarded by the SDK.
type Snapshots map[string]json.RawMessage

// stateObject builds a JS-side `{ snapshots, ax }` matching the State type
// from pkg/spec-api. ax is backed by the parsed uiautomator hierarchy when
// one is provided.
func stateObject(runtime *goja.Runtime, snapshots Snapshots, tree *hierarchy.Tree) (*goja.Object, error) {
	state := runtime.NewObject()
	snapshotsObject := runtime.NewObject()
	for key, raw := range snapshots {
		value, err := jsonToJSValue(runtime, raw)
		if err != nil {
			return nil, fmt.Errorf("snapshot %q: %w", key, err)
		}
		if err := snapshotsObject.Set(key, value); err != nil {
			return nil, err
		}
	}
	if err := state.Set("snapshots", snapshotsObject); err != nil {
		return nil, err
	}
	if err := state.Set("ax", accessibilityObject(runtime, tree)); err != nil {
		return nil, err
	}
	return state, nil
}

func accessibilityObject(runtime *goja.Runtime, tree *hierarchy.Tree) *goja.Object {
	accessibility := runtime.NewObject()
	find := func(selector string) goja.Value {
		if tree == nil {
			return goja.Undefined()
		}
		element := tree.Find(selector)
		if element == nil {
			return goja.Undefined()
		}
		return elementObject(runtime, element, selector)
	}
	findAll := func(selector string) []goja.Value {
		if tree == nil {
			return nil
		}
		elements := tree.FindAll(selector)
		result := make([]goja.Value, len(elements))
		for index, element := range elements {
			result[index] = elementObject(runtime, element, selector)
		}
		return result
	}
	_ = accessibility.Set("find", runtime.ToValue(find))
	_ = accessibility.Set("findAll", runtime.ToValue(findAll))
	return accessibility
}

func elementObject(runtime *goja.Runtime, element *hierarchy.Element, selector string) goja.Value {
	object := runtime.NewObject()
	centerX, centerY := element.Bounds.Center()
	_ = object.Set("id", element.ResourceID)
	_ = object.Set("text", element.Text)
	_ = object.Set("desc", element.Description)
	_ = object.Set("class", element.Class)
	_ = object.Set("x", centerX)
	_ = object.Set("y", centerY)
	_ = object.Set(tagSelector, selector)
	bounds := runtime.NewObject()
	_ = bounds.Set("left", element.Bounds.Left)
	_ = bounds.Set("top", element.Bounds.Top)
	_ = bounds.Set("right", element.Bounds.Right)
	_ = bounds.Set("bottom", element.Bounds.Bottom)
	_ = object.Set("bounds", bounds)
	return object
}

func jsonToJSValue(runtime *goja.Runtime, raw json.RawMessage) (goja.Value, error) {
	if len(raw) == 0 {
		return goja.Undefined(), nil
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	return runtime.ToValue(generic), nil
}

// jsValueToAction converts a JS-side {kind, on?, into?, text?} into a Go Action.
func jsValueToAction(runtime *goja.Runtime, value goja.Value) (Action, error) {
	if value == nil || goja.IsNull(value) || goja.IsUndefined(value) {
		return Action{}, fmt.Errorf("nil action")
	}
	object := value.ToObject(runtime)
	kindValue := object.Get("kind")
	if kindValue == nil {
		return Action{}, fmt.Errorf("action missing kind")
	}
	kind := kindValue.String()
	switch kind {
	case "Tap":
		on := object.Get("on")
		x, y := coordinatesOf(runtime, on)
		return Action{Kind: ActionKindTap, On: selectorOf(runtime, on), X: x, Y: y}, nil
	case "InputText":
		into := object.Get("into")
		text := object.Get("text")
		x, y := coordinatesOf(runtime, into)
		return Action{Kind: ActionKindInputText, On: selectorOf(runtime, into), Text: stringOf(text), X: x, Y: y}, nil
	default:
		return Action{}, fmt.Errorf("unknown action kind %q", kind)
	}
}

func selectorOf(runtime *goja.Runtime, value goja.Value) string {
	if value == nil || goja.IsNull(value) || goja.IsUndefined(value) {
		return ""
	}
	object := value.ToObject(runtime)
	if object == nil {
		return value.String()
	}
	if tag := object.Get(tagSelector); tag != nil && !goja.IsUndefined(tag) {
		return tag.String()
	}
	return value.String()
}

func coordinatesOf(runtime *goja.Runtime, value goja.Value) (int, int) {
	if value == nil || goja.IsNull(value) || goja.IsUndefined(value) {
		return 0, 0
	}
	object := value.ToObject(runtime)
	if object == nil {
		return 0, 0
	}
	xValue := object.Get("x")
	yValue := object.Get("y")
	if xValue == nil || yValue == nil || goja.IsUndefined(xValue) || goja.IsUndefined(yValue) {
		return 0, 0
	}
	return int(xValue.ToInteger()), int(yValue.ToInteger())
}

func stringOf(value goja.Value) string {
	if value == nil || goja.IsNull(value) || goja.IsUndefined(value) {
		return ""
	}
	return value.String()
}
