package verifier

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/dop251/goja"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// Snapshots is the per-step extractor output forwarded by the SDK.
type Snapshots map[string]json.RawMessage

type stateInput struct {
	snapshots  Snapshots
	tree       *hierarchy.Tree
	lastAction *Action
	stepTime   time.Time
	runStart   time.Time
	logs       []LogEntry
	exceptions []Exception
}

// stateObject builds the JS-side `state` object matching the State type from
// pkg/spec-api. Fields beyond snapshots/ax are included when the caller
// populated them on stateInput.
func stateObject(runtime *goja.Runtime, input stateInput) (*goja.Object, error) {
	state := runtime.NewObject()
	snapshotsObject := runtime.NewObject()
	for key, raw := range input.snapshots {
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
	if err := state.Set("ax", accessibilityObject(runtime, input.tree)); err != nil {
		return nil, err
	}
	if err := state.Set("lastAction", lastActionObject(runtime, input.lastAction)); err != nil {
		return nil, err
	}
	if err := state.Set("time", runtimeMillis(input.stepTime, input.runStart)); err != nil {
		return nil, err
	}
	if err := state.Set("logs", logsArray(runtime, input.logs)); err != nil {
		return nil, err
	}
	if err := state.Set("exceptions", exceptionsArray(runtime, input.exceptions)); err != nil {
		return nil, err
	}
	return state, nil
}

func accessibilityObject(runtime *goja.Runtime, tree *hierarchy.Tree) *goja.Object {
	accessibility := runtime.NewObject()
	find := func(call goja.FunctionCall) goja.Value {
		if tree == nil {
			return goja.Undefined()
		}
		node := findNodeFromJS(runtime, tree, call.Argument(0))
		if node == nil {
			return goja.Undefined()
		}
		return nodeObject(runtime, node, selectorStringFromJS(runtime, call.Argument(0)))
	}
	findAll := func(call goja.FunctionCall) goja.Value {
		if tree == nil {
			return goja.Undefined()
		}
		nodes := findAllNodesFromJS(runtime, tree, call.Argument(0))
		array := runtime.NewArray()
		for i, n := range nodes {
			_ = array.Set(fmt.Sprintf("%d", i), nodeObject(runtime, n, selectorStringFromJS(runtime, call.Argument(0))))
		}
		return array
	}
	_ = accessibility.Set("find", runtime.ToValue(find))
	_ = accessibility.Set("findAll", runtime.ToValue(findAll))
	return accessibility
}

func nodeObject(runtime *goja.Runtime, node *hierarchy.Node, selector string) goja.Value {
	element := &node.Element
	object := runtime.NewObject()
	centerX, centerY := element.Bounds.Center()
	_ = object.Set("id", element.ResourceID)
	_ = object.Set("text", element.Text)
	_ = object.Set("desc", element.Description)
	_ = object.Set("class", element.Class)
	_ = object.Set("clickable", element.Clickable)
	_ = object.Set("enabled", element.Enabled)
	_ = object.Set("checked", element.Checked)
	_ = object.Set("focused", element.Focused)
	_ = object.Set("selected", element.Selected)
	_ = object.Set("editable", element.Editable)
	_ = object.Set("x", centerX)
	_ = object.Set("y", centerY)
	_ = object.Set(tagSelector, selector)
	bounds := runtime.NewObject()
	_ = bounds.Set("left", element.Bounds.Left)
	_ = bounds.Set("top", element.Bounds.Top)
	_ = bounds.Set("right", element.Bounds.Right)
	_ = bounds.Set("bottom", element.Bounds.Bottom)
	_ = object.Set("bounds", bounds)
	attrs := runtime.NewObject()
	for k, v := range element.Attributes {
		_ = attrs.Set(k, v)
	}
	_ = object.Set("attrs", attrs)
	childFind := func(call goja.FunctionCall) goja.Value {
		arg := call.Argument(0)
		childNode := findNodeInSubtreeFromJS(runtime, node, arg)
		if childNode == nil {
			return goja.Undefined()
		}
		return nodeObject(runtime, childNode, selectorStringFromJS(runtime, arg))
	}
	childFindAll := func(call goja.FunctionCall) goja.Value {
		arg := call.Argument(0)
		childNodes := findAllNodesInSubtreeFromJS(runtime, node, arg)
		array := runtime.NewArray()
		for i, n := range childNodes {
			_ = array.Set(fmt.Sprintf("%d", i), nodeObject(runtime, n, selectorStringFromJS(runtime, arg)))
		}
		return array
	}
	_ = object.Set("find", runtime.ToValue(childFind))
	_ = object.Set("findAll", runtime.ToValue(childFindAll))
	return object
}

// findNodeFromJS dispatches a JS value (string, object, or array of objects)
// to Tree-level node lookup.
func findNodeFromJS(runtime *goja.Runtime, tree *hierarchy.Tree, arg goja.Value) *hierarchy.Node {
	if goja.IsUndefined(arg) || goja.IsNull(arg) {
		return nil
	}
	if tree == nil {
		return nil
	}
	if s, ok := arg.Export().(string); ok {
		return tree.FindNode(s)
	}
	if path, ok := selectorPathFromJS(runtime, arg); ok {
		requireKnownSelectorKeys(runtime, tree, path...)
		return tree.FindBySelectorPath(path)
	}
	sel := selectorFromJSObject(runtime, arg)
	if len(sel.Filters) == 0 {
		return nil
	}
	requireKnownSelectorKeys(runtime, tree, sel)
	return tree.Root.FindBySelector(sel)
}

// findAllNodesFromJS dispatches a JS value to Tree-level multi-node lookup.
func findAllNodesFromJS(runtime *goja.Runtime, tree *hierarchy.Tree, arg goja.Value) []*hierarchy.Node {
	if goja.IsUndefined(arg) || goja.IsNull(arg) || tree == nil {
		return nil
	}
	if s, ok := arg.Export().(string); ok {
		return tree.FindAllNodes(s)
	}
	if path, ok := selectorPathFromJS(runtime, arg); ok {
		requireKnownSelectorKeys(runtime, tree, path...)
		return tree.FindAllBySelectorPath(path)
	}
	sel := selectorFromJSObject(runtime, arg)
	if len(sel.Filters) == 0 {
		return nil
	}
	requireKnownSelectorKeys(runtime, tree, sel)
	return tree.Root.FindAllBySelector(sel)
}

// findNodeInSubtreeFromJS dispatches a JS value to Node-level scoped lookup.
func findNodeInSubtreeFromJS(runtime *goja.Runtime, node *hierarchy.Node, arg goja.Value) *hierarchy.Node {
	if goja.IsUndefined(arg) || goja.IsNull(arg) {
		return nil
	}
	if s, ok := arg.Export().(string); ok {
		return node.Find(s)
	}
	if path, ok := selectorPathFromJS(runtime, arg); ok {
		requireKnownSelectorKeys(runtime, node.Tree(), path...)
		return node.FindBySelectorPath(path)
	}
	sel := selectorFromJSObject(runtime, arg)
	if len(sel.Filters) == 0 {
		return nil
	}
	requireKnownSelectorKeys(runtime, node.Tree(), sel)
	return node.FindBySelector(sel)
}

// findAllNodesInSubtreeFromJS dispatches a JS value to Node-level scoped multi-lookup.
func findAllNodesInSubtreeFromJS(runtime *goja.Runtime, node *hierarchy.Node, arg goja.Value) []*hierarchy.Node {
	if goja.IsUndefined(arg) || goja.IsNull(arg) {
		return nil
	}
	if s, ok := arg.Export().(string); ok {
		return node.FindAll(s)
	}
	if path, ok := selectorPathFromJS(runtime, arg); ok {
		requireKnownSelectorKeys(runtime, node.Tree(), path...)
		return node.FindAllBySelectorPath(path)
	}
	sel := selectorFromJSObject(runtime, arg)
	if len(sel.Filters) == 0 {
		return nil
	}
	requireKnownSelectorKeys(runtime, node.Tree(), sel)
	return node.FindAllBySelector(sel)
}

// requireKnownSelectorKeys throws a JS error when a selector names a key that
// can never match. Returning an empty result instead is indistinguishable from
// a screen that simply has no such element, so a spec built on a mistyped key
// generates no action, the runner waits out every step, and the campaign
// finishes clean having explored nothing.
func requireKnownSelectorKeys(
	runtime *goja.Runtime,
	tree *hierarchy.Tree,
	selectors ...hierarchy.Selector,
) {
	var unknown []string
	for _, sel := range selectors {
		for _, key := range tree.UnknownSelectorKeys(sel) {
			if !slices.Contains(unknown, key) {
				unknown = append(unknown, key)
			}
		}
	}
	if len(unknown) == 0 {
		return
	}
	panic(runtime.NewTypeError(hierarchy.UnknownSelectorKeyMessage(unknown)))
}

// selectorFromJSObject converts a JS object {attr: value, ...} into a Selector.
func selectorFromJSObject(runtime *goja.Runtime, arg goja.Value) hierarchy.Selector {
	obj := arg.ToObject(runtime)
	if obj == nil {
		return hierarchy.Selector{}
	}
	var sel hierarchy.Selector
	for _, key := range obj.Keys() {
		if key == tagSelector {
			continue
		}
		val := obj.Get(key)
		if val == nil || goja.IsUndefined(val) {
			continue
		}
		sel.Filters = append(sel.Filters, hierarchy.AttrFilter{Attr: key, Value: val.String()})
	}
	return sel
}

// selectorPathFromJS recognizes a JS array of selector objects and converts it
// into a Selector chain. Returns ok=false for non-arrays so callers fall
// through to single-object dispatch.
func selectorPathFromJS(runtime *goja.Runtime, arg goja.Value) ([]hierarchy.Selector, bool) {
	exported := arg.Export()
	slice, ok := exported.([]any)
	if !ok {
		return nil, false
	}
	obj := arg.ToObject(runtime)
	if obj == nil {
		return nil, false
	}
	path := make([]hierarchy.Selector, 0, len(slice))
	for index := range slice {
		entry := obj.Get(fmt.Sprintf("%d", index))
		if entry == nil || goja.IsUndefined(entry) || goja.IsNull(entry) {
			return nil, false
		}
		sel := selectorFromJSObject(runtime, entry)
		if len(sel.Filters) == 0 {
			return nil, false
		}
		path = append(path, sel)
	}
	if len(path) == 0 {
		return nil, false
	}
	return path, true
}

// selectorStringFromJS returns a string representation of the selector argument
// for tagging returned ax element objects (state.ax.find/findAll), so a spec
// reading an element's selector sees the canonical form. Output follows the
// hierarchy selector grammar: "k:v" pairs space-joined per object, chains
// joined by " > ".
func selectorStringFromJS(runtime *goja.Runtime, arg goja.Value) string {
	if arg == nil || goja.IsUndefined(arg) || goja.IsNull(arg) {
		return ""
	}
	if s, ok := arg.Export().(string); ok {
		return s
	}
	exported := arg.Export()
	if slice, ok := exported.([]any); ok {
		object := arg.ToObject(runtime)
		if object == nil {
			return ""
		}
		parts := make([]string, 0, len(slice))
		for index := range slice {
			entry := object.Get(fmt.Sprintf("%d", index))
			if entry == nil || goja.IsUndefined(entry) || goja.IsNull(entry) {
				continue
			}
			segment := selectorObjectToString(runtime, entry)
			if segment == "" {
				continue
			}
			parts = append(parts, segment)
		}
		return strings.Join(parts, " > ")
	}
	return selectorObjectToString(runtime, arg)
}

// selectorObjectToString formats a single JS object as a space-joined sequence
// of "k:v" pairs, mirroring the hierarchy package's predicate grammar.
func selectorObjectToString(runtime *goja.Runtime, arg goja.Value) string {
	if arg == nil || goja.IsUndefined(arg) || goja.IsNull(arg) {
		return ""
	}
	object := arg.ToObject(runtime)
	if object == nil {
		return ""
	}
	keys := object.Keys()
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if key == tagSelector {
			continue
		}
		value := object.Get(key)
		if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%s", key, value.String()))
	}
	return strings.Join(parts, " ")
}

func lastActionObject(runtime *goja.Runtime, action *Action) goja.Value {
	if action == nil {
		return goja.Null()
	}
	object := runtime.NewObject()
	_ = object.Set("kind", string(action.Kind))
	if action.On != "" {
		_ = object.Set("on", action.On)
	}
	if action.Text != "" {
		_ = object.Set("text", action.Text)
	}
	switch action.Kind {
	case ActionKindSwipe:
		from := runtime.NewObject()
		_ = from.Set("x", action.FromX)
		_ = from.Set("y", action.FromY)
		to := runtime.NewObject()
		_ = to.Set("x", action.ToX)
		_ = to.Set("y", action.ToY)
		_ = object.Set("from", from)
		_ = object.Set("to", to)
		if action.DurationMillis > 0 {
			_ = object.Set("durationMillis", action.DurationMillis)
		}
	case ActionKindScroll:
		_ = object.Set("direction", action.Direction)
		from := runtime.NewObject()
		_ = from.Set("x", action.FromX)
		_ = from.Set("y", action.FromY)
		to := runtime.NewObject()
		_ = to.Set("x", action.ToX)
		_ = to.Set("y", action.ToY)
		_ = object.Set("from", from)
		_ = object.Set("to", to)
	case ActionKindPressKey:
		_ = object.Set("key", action.Key)
	case ActionKindWait:
		_ = object.Set("durationMillis", action.DurationMillis)
	}
	return object
}

func runtimeMillis(stepTime, runStart time.Time) int64 {
	if stepTime.IsZero() || runStart.IsZero() {
		return 0
	}
	return stepTime.Sub(runStart).Milliseconds()
}

func logsArray(runtime *goja.Runtime, logs []LogEntry) *goja.Object {
	array := runtime.NewArray()
	for index, entry := range logs {
		item := runtime.NewObject()
		_ = item.Set("unixMillis", entry.UnixMillis)
		_ = item.Set("level", entry.Level)
		_ = item.Set("tag", entry.Tag)
		_ = item.Set("message", entry.Message)
		_ = array.Set(fmt.Sprintf("%d", index), item)
	}
	return array
}

func exceptionsArray(runtime *goja.Runtime, exceptions []Exception) *goja.Object {
	array := runtime.NewArray()
	for index, exception := range exceptions {
		item := runtime.NewObject()
		_ = item.Set("class", exception.Class)
		_ = item.Set("message", exception.Message)
		_ = item.Set("stackTrace", exception.StackTrace)
		if exception.UnixMillis > 0 {
			_ = item.Set("unixMillis", exception.UnixMillis)
		}
		_ = array.Set(fmt.Sprintf("%d", index), item)
	}
	return array
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

// wireAction is the unified flat wire contract both runtime entries emit
// (runtime-entry.ts serializeAction). ONE decoder reads it on both the goja
// (native) and the runner (web) sides, so a field rename cannot silently turn
// an action into a no-op on one path only.
type wireAction struct {
	Kind           string `json:"kind"`
	X              int    `json:"x"`
	Y              int    `json:"y"`
	Selector       string `json:"selector"`
	Text           string `json:"text"`
	Key            string `json:"key"`
	Direction      string `json:"direction"`
	FromX          int    `json:"fromX"`
	FromY          int    `json:"fromY"`
	ToX            int    `json:"toX"`
	ToY            int    `json:"toY"`
	DurationMillis int    `json:"durationMillis"`
}

// DecodeAction turns one serialized action (the flat camelCase wire contract)
// into a Go Action. An empty payload or JSON "null" reports ErrNoAction.
func DecodeAction(raw json.RawMessage) (Action, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return Action{}, ErrNoAction
	}
	var wire wireAction
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Action{}, fmt.Errorf("decode action: %w", err)
	}
	switch wire.Kind {
	case "Tap":
		return Action{Kind: ActionKindTap, On: wire.Selector, X: wire.X, Y: wire.Y}, nil
	case "DoubleTap":
		return Action{Kind: ActionKindDoubleTap, On: wire.Selector, X: wire.X, Y: wire.Y}, nil
	case "LongPress":
		return Action{Kind: ActionKindLongPress, On: wire.Selector, X: wire.X, Y: wire.Y}, nil
	case "InputText":
		return Action{Kind: ActionKindInputText, On: wire.Selector, Text: wire.Text, X: wire.X, Y: wire.Y}, nil
	case "Swipe":
		return Action{
			Kind:           ActionKindSwipe,
			FromX:          wire.FromX,
			FromY:          wire.FromY,
			ToX:            wire.ToX,
			ToY:            wire.ToY,
			DurationMillis: wire.DurationMillis,
		}, nil
	case "Scroll":
		return Action{
			Kind:           ActionKindScroll,
			On:             wire.Selector,
			Direction:      wire.Direction,
			FromX:          wire.FromX,
			FromY:          wire.FromY,
			ToX:            wire.ToX,
			ToY:            wire.ToY,
			DurationMillis: wire.DurationMillis,
		}, nil
	case "PressKey":
		return Action{Kind: ActionKindPressKey, Key: wire.Key}, nil
	case "Wait":
		return Action{Kind: ActionKindWait, DurationMillis: wire.DurationMillis}, nil
	default:
		return Action{}, fmt.Errorf("unknown action kind %q", wire.Kind)
	}
}
