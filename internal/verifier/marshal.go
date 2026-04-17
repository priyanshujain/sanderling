package verifier

import (
	"encoding/json"
	"fmt"

	"github.com/dop251/goja"
)

// Snapshots is the per-step extractor output forwarded by the SDK.
type Snapshots map[string]json.RawMessage

// stateObject builds a JS-side `{ snapshots, ax }` matching the State type
// from pkg/spec-api. ax is currently a stub returning nothing.
func stateObject(runtime *goja.Runtime, snapshots Snapshots) (*goja.Object, error) {
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

	accessibility := runtime.NewObject()
	if err := accessibility.Set("find", runtime.ToValue(func(string) goja.Value { return goja.Undefined() })); err != nil {
		return nil, err
	}
	if err := accessibility.Set("findAll", runtime.ToValue(func(string) []goja.Value { return nil })); err != nil {
		return nil, err
	}
	if err := state.Set("ax", accessibility); err != nil {
		return nil, err
	}
	return state, nil
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
		return Action{Kind: ActionKindTap, On: stringOf(on)}, nil
	case "InputText":
		into := object.Get("into")
		text := object.Get("text")
		return Action{Kind: ActionKindInputText, On: stringOf(into), Text: stringOf(text)}, nil
	default:
		return Action{}, fmt.Errorf("unknown action kind %q", kind)
	}
}

func stringOf(value goja.Value) string {
	if value == nil || goja.IsNull(value) || goja.IsUndefined(value) {
		return ""
	}
	return value.String()
}
