package verifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
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
// pkg/spec. Fields beyond snapshots/ax are included when the caller
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
		return nodeObject(runtime, tree, node, selectorStringFromJS(runtime, call.Argument(0)))
	}
	findAll := func(call goja.FunctionCall) goja.Value {
		if tree == nil {
			return goja.Undefined()
		}
		nodes := findAllNodesFromJS(runtime, tree, call.Argument(0))
		selector := selectorStringFromJS(runtime, call.Argument(0))
		array := runtime.NewArray()
		for i, n := range nodes {
			_ = array.Set(fmt.Sprintf("%d", i), nodeObject(runtime, tree, n, selector))
		}
		return array
	}
	_ = accessibility.Set("find", runtime.ToValue(find))
	_ = accessibility.Set("findAll", runtime.ToValue(findAll))
	return accessibility
}

// unambiguousSelector returns selector only when no node other than this one
// answers to it. A shared selector names all the siblings and every consumer
// resolves it to the first match. The runner recovers where the action carries
// usable coordinates, since resolveCoordinates prefers them over an ambiguous
// name, but the recorded selector is also the element's identity in the trace
// and the replay UI, and the driver's TapSelector path resolves it on the
// device where nothing can tell the siblings apart. An unnamed element keeps
// its own coordinates, which are already right, matching what selectorsFor does
// for the builtin target enumeration in pkg/spec/src/web-runtime.ts.
func unambiguousSelector(tree *hierarchy.Tree, node *hierarchy.Node, selector string) string {
	if tree == nil || selector == "" {
		return ""
	}
	for _, match := range tree.FindAllNodes(selector) {
		if match != node {
			return ""
		}
	}
	return selector
}

func nodeObject(runtime *goja.Runtime, tree *hierarchy.Tree, node *hierarchy.Node, selector string) goja.Value {
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
	// Three-valued, unlike the other state flags: null where the platform
	// reported nothing at all, which is what separates an ordinary field from a
	// password field on a platform that cannot tell them apart.
	var secure any
	if element.SecureReported() {
		secure = element.Secure
	}
	_ = object.Set("secure", secure)
	_ = object.Set("x", centerX)
	_ = object.Set("y", centerY)
	_ = object.Set(tagSelector, unambiguousSelector(tree, node, selector))
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
		return nodeObject(runtime, tree, childNode, selectorStringFromJS(runtime, arg))
	}
	childFindAll := func(call goja.FunctionCall) goja.Value {
		arg := call.Argument(0)
		childNodes := findAllNodesInSubtreeFromJS(runtime, node, arg)
		childSelector := selectorStringFromJS(runtime, arg)
		array := runtime.NewArray()
		for i, n := range childNodes {
			_ = array.Set(fmt.Sprintf("%d", i), nodeObject(runtime, tree, n, childSelector))
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
	return tree.FindBySelector(sel)
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
	return tree.FindAllBySelector(sel)
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

// actionField is one property of the lastAction object, in the order the
// object is built. Value is a string, an int, or a nested []actionField for
// the from/to points.
type actionField struct {
	key   string
	value any
}

// lastActionFields is the ONE description of the lastAction shape. The goja
// host turns it into a JS object (lastActionObject); the web host receives the
// same fields as JSON (EncodeLastAction) and installs them as state.lastAction
// in the page. Both hosts therefore expose identical field names, casing,
// presence and order, so a property reading state.lastAction cannot mean one
// thing on native and another on web.
func lastActionFields(action *Action) []actionField {
	point := func(x, y int) []actionField {
		return []actionField{{key: "x", value: x}, {key: "y", value: y}}
	}
	// An action whose apply call failed is not an action that did not happen:
	// the dispatch may have landed before the error. That is unknown, and
	// unknown is null here for the same reason every other absence in the spec
	// surface is, so a property decides for itself instead of being handed a
	// "nothing happened" the runner cannot vouch for.
	var applied any
	if action.Applied {
		applied = true
	}
	// A relaunch between two readings is not "no action happened", which is
	// what dropping the action reported instead: the app restarted after an
	// action that did run. Only the positive report is a fact the runner can
	// vouch for, so the other side is null rather than false: a target whose
	// foreground the runner cannot read (web, iOS) never relaunches the app and
	// still cannot promise it never restarted.
	var relaunched any
	if action.Relaunched {
		relaunched = true
	}
	fields := []actionField{
		{key: "kind", value: string(action.Kind)},
		{key: "applied", value: applied},
		{key: "relaunched", value: relaunched},
	}
	if action.On != "" {
		fields = append(fields, actionField{key: "on", value: action.On})
	}
	if action.Text != "" {
		fields = append(fields, actionField{key: "text", value: action.Text})
	}
	switch action.Kind {
	case ActionKindSwipe:
		fields = append(fields,
			actionField{key: "from", value: point(action.FromX, action.FromY)},
			actionField{key: "to", value: point(action.ToX, action.ToY)})
		if action.DurationMillis > 0 {
			fields = append(fields,
				actionField{key: "durationMillis", value: action.DurationMillis})
		}
	case ActionKindScroll:
		fields = append(fields,
			actionField{key: "direction", value: action.Direction},
			actionField{key: "from", value: point(action.FromX, action.FromY)},
			actionField{key: "to", value: point(action.ToX, action.ToY)})
	case ActionKindPressKey:
		fields = append(fields, actionField{key: "key", value: action.Key})
	case ActionKindWait:
		fields = append(fields,
			actionField{key: "durationMillis", value: action.DurationMillis})
	}
	return fields
}

func lastActionObject(runtime *goja.Runtime, action *Action) goja.Value {
	if action == nil {
		return goja.Null()
	}
	return objectFromFields(runtime, lastActionFields(action))
}

func objectFromFields(runtime *goja.Runtime, fields []actionField) *goja.Object {
	object := runtime.NewObject()
	for _, field := range fields {
		if nested, ok := field.value.([]actionField); ok {
			_ = object.Set(field.key, objectFromFields(runtime, nested))
			continue
		}
		_ = object.Set(field.key, field.value)
	}
	return object
}

// EncodeLastAction renders the previous step's action for the web host, which
// has no Go-side state object to read: the runner pushes this JSON into the
// page before each extractor evaluation. A nil action encodes as JSON null,
// the same value the goja host reports on the first step of a run and after a
// step whose action was never dispatched.
func EncodeLastAction(action *Action) json.RawMessage {
	if action == nil {
		return json.RawMessage("null")
	}
	return encodeFields(lastActionFields(action))
}

func encodeFields(fields []actionField) json.RawMessage {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for index, field := range fields {
		if index > 0 {
			buffer.WriteByte(',')
		}
		buffer.Write(encodeJSValue(field.key))
		buffer.WriteByte(':')
		if nested, ok := field.value.([]actionField); ok {
			buffer.Write(encodeFields(nested))
			continue
		}
		buffer.Write(encodeJSValue(field.value))
	}
	buffer.WriteByte('}')
	return buffer.Bytes()
}

// encodeJSValue encodes one value the way JS JSON.stringify would, so the JSON
// the web host parses is byte-identical to what the goja object stringifies to.
// Go escapes <, > and & by default, which JSON.stringify does not, and that
// alone would make the two hosts encode the same selector differently.
func encodeJSValue(value any) []byte {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return []byte("null")
	}
	return bytes.TrimRight(buffer.Bytes(), "\n")
}

func runtimeMillis(stepTime, runStart time.Time) int64 {
	if stepTime.IsZero() || runStart.IsZero() {
		return 0
	}
	return stepTime.Sub(runStart).Milliseconds()
}

// logFields is the ONE description of a state.logs entry, for the same reason
// lastActionFields is: the goja host turns it into a JS object (logsArray) and
// the web host receives the same fields as JSON (EncodeLogs), so a property
// counting error-level lines cannot read one shape on native and another on web.
func logFields(entry LogEntry) []actionField {
	return []actionField{
		{key: "unixMillis", value: entry.UnixMillis},
		{key: "level", value: entry.Level},
		{key: "tag", value: entry.Tag},
		{key: "message", value: entry.Message},
	}
}

func logsArray(runtime *goja.Runtime, logs []LogEntry) *goja.Object {
	array := runtime.NewArray()
	for index, entry := range logs {
		_ = array.Set(fmt.Sprintf("%d", index), objectFromFields(runtime, logFields(entry)))
	}
	return array
}

// EncodeLogs renders this step's log entries for the web host, which has no
// Go-side state object to read: the runner pushes this JSON into the page
// before each extractor evaluation. No entries encodes as an empty array, the
// same value the goja host reports for a step whose log fetch found nothing.
func EncodeLogs(logs []LogEntry) json.RawMessage {
	var buffer bytes.Buffer
	buffer.WriteByte('[')
	for index, entry := range logs {
		if index > 0 {
			buffer.WriteByte(',')
		}
		buffer.Write(encodeFields(logFields(entry)))
	}
	buffer.WriteByte(']')
	return buffer.Bytes()
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

// ActionWireContract names the encoding wireAction below reads, and must equal
// the ACTION_WIRE_CONTRACT the bundled runtime entry declares. It is versioned
// apart from @sanderling/spec because the encoding and the package move
// independently: what this binary needs to know is which reading of the fields
// it is being handed, not which release shipped it.
//
// The two halves can disagree without either failing. Revision 1
// (@sanderling/spec 0.0.3 and earlier, which declares nothing) sent an authored
// Scroll's container point as both endpoints; this binary reads pre-computed
// endpoints as authoritative, so every such scroll dispatched successfully as a
// 250ms press and hold and travelled zero distance, and no run said so.
const ActionWireContract = "action-wire/2"

// actionEncodingGlobal is where the bundled runtime entry declares its
// contract (defineLockedGlobal in pkg/spec/src/runtime-entry.ts).
const actionEncodingGlobal = "__sanderlingActionEncoding__"

// ActionEncodingError reports a bundle whose declared contract is not the one
// this binary decodes. declared is empty for a package published before the
// declaration existed, which is the pairing that has to fail loudest: it is the
// one that already produced a campaign of zero-distance scrolls.
func ActionEncodingError(declared string) error {
	built := strconv.Quote(declared)
	if declared == "" {
		built = "an @sanderling/spec that declares none at all (every release before " +
			strconv.Quote(ActionWireContract) + ")"
	}
	return fmt.Errorf(
		"action encoding mismatch: this specification was bundled against %s and this "+
			"binary implements %q. Every action would still dispatch successfully while "+
			"executing a different gesture, so the run stops here instead of producing "+
			"wrong data. The @sanderling/spec the spec is bundled against and this binary "+
			"must come from the same commit or a compatible release: re-install "+
			"@sanderling/spec, or point the spec at the pkg/spec checkout this binary was "+
			"built from",
		built, ActionWireContract)
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
	// Source names the generator that produced the action: the spec's setup or
	// the action root. Empty from the candidate enumeration, which serializes
	// actions nothing has chosen yet.
	Source string `json:"source"`
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
	action, err := actionFromWire(wire)
	if err != nil {
		return Action{}, err
	}
	action.Source = wire.Source
	return action, nil
}

func actionFromWire(wire wireAction) (Action, error) {
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
