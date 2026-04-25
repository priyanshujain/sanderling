package verifier

import (
	"encoding/json"
	"fmt"
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
		return nodeObject(runtime, node, selectorStringFromJS(call.Argument(0)))
	}
	findAll := func(call goja.FunctionCall) goja.Value {
		if tree == nil {
			return goja.Undefined()
		}
		nodes := findAllNodesFromJS(runtime, tree, call.Argument(0))
		array := runtime.NewArray()
		for i, n := range nodes {
			_ = array.Set(fmt.Sprintf("%d", i), nodeObject(runtime, n, selectorStringFromJS(call.Argument(0))))
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
		return nodeObject(runtime, childNode, selectorStringFromJS(arg))
	}
	childFindAll := func(call goja.FunctionCall) goja.Value {
		arg := call.Argument(0)
		childNodes := findAllNodesInSubtreeFromJS(runtime, node, arg)
		array := runtime.NewArray()
		for i, n := range childNodes {
			_ = array.Set(fmt.Sprintf("%d", i), nodeObject(runtime, n, selectorStringFromJS(arg)))
		}
		return array
	}
	_ = object.Set("find", runtime.ToValue(childFind))
	_ = object.Set("findAll", runtime.ToValue(childFindAll))
	return object
}

// findNodeFromJS dispatches a JS value (string or object) to Tree-level node lookup.
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
	sel := selectorFromJSObject(runtime, arg)
	if len(sel.Filters) == 0 {
		return nil
	}
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
	sel := selectorFromJSObject(runtime, arg)
	if len(sel.Filters) == 0 {
		return nil
	}
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
	sel := selectorFromJSObject(runtime, arg)
	if len(sel.Filters) == 0 {
		return nil
	}
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
	sel := selectorFromJSObject(runtime, arg)
	if len(sel.Filters) == 0 {
		return nil
	}
	return node.FindAllBySelector(sel)
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

// selectorStringFromJS returns a string representation of the selector argument
// for tagging returned element objects (used by selectorOf to reconstruct the
// selector when the element is passed back as an action target).
func selectorStringFromJS(arg goja.Value) string {
	if goja.IsUndefined(arg) || goja.IsNull(arg) {
		return ""
	}
	if s, ok := arg.Export().(string); ok {
		return s
	}
	return arg.String()
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

// jsValueToAction converts a JS-side action object into a Go Action.
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
	case "Swipe":
		from := object.Get("from")
		to := object.Get("to")
		fromX, fromY := coordinatesOf(runtime, from)
		toX, toY := coordinatesOf(runtime, to)
		if fromX == 0 && fromY == 0 {
			fromX, fromY = pointCoordinates(runtime, from)
		}
		if toX == 0 && toY == 0 {
			toX, toY = pointCoordinates(runtime, to)
		}
		return Action{
			Kind:           ActionKindSwipe,
			FromX:          fromX,
			FromY:          fromY,
			ToX:            toX,
			ToY:            toY,
			DurationMillis: intField(object, "durationMillis"),
		}, nil
	case "PressKey":
		return Action{Kind: ActionKindPressKey, Key: stringOf(object.Get("key"))}, nil
	case "Wait":
		return Action{Kind: ActionKindWait, DurationMillis: intField(object, "durationMillis")}, nil
	default:
		return Action{}, fmt.Errorf("unknown action kind %q", kind)
	}
}

// pointCoordinates reads a plain {x, y} literal (not an AX element), which is
// how Swipe endpoints are commonly expressed in specs.
func pointCoordinates(runtime *goja.Runtime, value goja.Value) (int, int) {
	if value == nil || goja.IsNull(value) || goja.IsUndefined(value) {
		return 0, 0
	}
	object := value.ToObject(runtime)
	if object == nil {
		return 0, 0
	}
	x := object.Get("x")
	y := object.Get("y")
	if x == nil || y == nil {
		return 0, 0
	}
	return int(x.ToInteger()), int(y.ToInteger())
}

func intField(object *goja.Object, name string) int {
	value := object.Get(name)
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return 0
	}
	return int(value.ToInteger())
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
