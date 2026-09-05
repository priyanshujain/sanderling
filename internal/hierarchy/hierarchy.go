// Package hierarchy parses the TreeNode JSON produced by the native sidecar
// and resolves selectors against it.
//
// Selector grammar (v2.0):
//
//	String selectors (global scan or element-scoped):
//	  attribute:value      - substring match; exact for "true"/"false" booleans
//	  id:<suffix>          - substring on resource-id / identifier (backward compat)
//	  idPrefix:<prefix>    - starts-with on resource-id / identifier, package prefix skipped
//	  text:<value>         - substring on text attribute, innermost match only
//	  desc:<value>         - substring on content-desc / accessibilityText
//	  descPrefix:<prefix>  - starts-with on content-desc / accessibilityText
//	  tag:<value>          - exact match on the element's tag name (web)
//
//	Object selectors (multi-attribute AND, element-scoped or global):
//	  { attr: value, ... } - all key/value pairs must match, each key resolved by
//	                         the same rule its string form above uses
//
//	Path queries (global scan only, string form):
//	  <sel> > <sel> > ...  - each segment matched within subtree of previous match
//
// Cross-platform aliases are expanded automatically, one level deep: every name
// for a fact lists every key a producer writes it under rather than hopping
// through another alias. "label" / "accessibilityLabel" / "ariaLabel" /
// "contentDescription" resolve to accessibilityText and content-desc, which also
// check each other; "identifier" / "accessibilityIdentifier" / "testTag" /
// "testID" resolve to resource-id, to each other and to data-testid, so a
// Compose testTag matches whether the platform exposes it as resource-id
// (Android), accessibilityIdentifier (iOS) or data-testid (web).
package hierarchy

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Bounds is an inclusive rectangle in device pixels.
type Bounds struct {
	Left   int `json:"left"`
	Top    int `json:"top"`
	Right  int `json:"right"`
	Bottom int `json:"bottom"`
}

// Center returns the center point of the bounds.
func (b Bounds) Center() (int, int) {
	return (b.Left + b.Right) / 2, (b.Top + b.Bottom) / 2
}

// Width returns the bounds' width.
func (b Bounds) Width() int { return b.Right - b.Left }

// Height returns the bounds' height.
func (b Bounds) Height() int { return b.Bottom - b.Top }

// Element is a flattened view of one hierarchy node.
type Element struct {
	ResourceID  string `json:"resourceId,omitempty"`
	Text        string `json:"text,omitempty"`
	Description string `json:"description,omitempty"`
	Class       string `json:"class,omitempty"`
	Package     string `json:"package,omitempty"`
	// Screen holds the current route/screen name when set by the driver on the
	// root element (web platform only; empty for native platforms).
	Screen     string            `json:"screen,omitempty"`
	Clickable  bool              `json:"clickable,omitempty"`
	Enabled    bool              `json:"enabled,omitempty"`
	Checked    bool              `json:"checked,omitempty"`
	Focused    bool              `json:"focused,omitempty"`
	Selected   bool              `json:"selected,omitempty"`
	Editable   bool              `json:"editable,omitempty"`
	Secure     bool              `json:"secure,omitempty"`
	Bounds     Bounds            `json:"bounds"`
	Attributes map[string]string `json:"attrs,omitempty"`
}

// SecureReported reports whether the producer stated this element's secure
// fact at all. Android never does, so an element without it is unknown rather
// than known not to be a secure entry, and a caller deciding what may be
// written down has to tell those two apart.
func (e *Element) SecureReported() bool {
	_, reported := e.Attributes["secure"]
	return reported
}

// Node is one node in the hierarchy tree.
type Node struct {
	Element
	Children []*Node `json:"-"`
	tree     *Tree
}

// Tree is a flat collection of every node in a hierarchy dump, in pre-order.
type Tree struct {
	Root     *Node      `json:"-"`
	Elements []*Element `json:"elements"`
	// UnreadableFlags counts the boolean fields the producer sent as something
	// other than a boolean. They are dropped rather than failing the dump, so
	// the count is what keeps the drop from being silent.
	UnreadableFlags int `json:"unreadableFlags,omitempty"`
}

// treeJSON is the stored form of a Tree. `depths` is the pre-order depth of
// each element, which is what turns the flat array back into Root: a stored
// tree without it (every trace written before the field existed) decodes with
// a nil Root and resolves no selector, exactly as it did before.
//
// A depth per element rather than a parent index per element: the numbers are
// one digit deep into most hierarchies where a parent index is three, and a
// step already costs 86 KB on android.
type treeJSON struct {
	Elements        []*Element `json:"elements"`
	Depths          []int      `json:"depths,omitempty"`
	UnreadableFlags int        `json:"unreadable_flags,omitempty"`
}

func (t Tree) MarshalJSON() ([]byte, error) {
	return json.Marshal(treeJSON{
		Elements:        t.Elements,
		Depths:          t.depths(),
		UnreadableFlags: t.UnreadableFlags,
	})
}

// depths walks Root, and yields nothing unless the walk covers exactly the
// elements the flat array holds: a hand-built Tree whose Root and Elements
// disagree would otherwise store a shape that rebuilds into a different tree.
func (t Tree) depths() []int {
	if t.Root == nil {
		return nil
	}
	depths := make([]int, 0, len(t.Elements))
	var walk func(node *Node, depth int)
	walk = func(node *Node, depth int) {
		depths = append(depths, depth)
		for _, child := range node.Children {
			walk(child, depth+1)
		}
	}
	walk(t.Root, 0)
	if len(depths) != len(t.Elements) {
		return nil
	}
	return depths
}

func (t *Tree) UnmarshalJSON(data []byte) error {
	var stored treeJSON
	if err := json.Unmarshal(data, &stored); err != nil {
		return err
	}
	t.Elements = stored.Elements
	t.UnreadableFlags = stored.UnreadableFlags
	t.Root = t.rebuild(stored.Depths)
	return nil
}

// rebuild re-parents the flat pre-order array from the stored depths. Every
// element is re-seated inside its Node so Tree.Elements and &node.Element stay
// the same pointer, which is the identity the verifier's element scope and the
// picker's target list are keyed on.
func (t *Tree) rebuild(depths []int) *Node {
	if !wellFormedDepths(depths, len(t.Elements)) {
		return nil
	}
	stack := make([]*Node, 0, 32)
	for index, depth := range depths {
		node := &Node{Element: *t.Elements[index], tree: t}
		t.Elements[index] = &node.Element
		stack = stack[:depth]
		if depth > 0 {
			parent := stack[depth-1]
			parent.Children = append(parent.Children, node)
		}
		stack = append(stack, node)
	}
	return stack[0]
}

// wellFormedDepths accepts only a single-rooted pre-order sequence: one root at
// the head and no child deeper than one level below its predecessor.
func wellFormedDepths(depths []int, elementCount int) bool {
	if len(depths) == 0 || len(depths) != elementCount || depths[0] != 0 {
		return false
	}
	for index := 1; index < len(depths); index++ {
		if depths[index] < 1 || depths[index] > depths[index-1]+1 {
			return false
		}
	}
	return true
}

// treeNodeJSON mirrors the sidecar TreeNode JSON structure.
type treeNodeJSON struct {
	Attributes map[string]string `json:"attributes"`
	Children   []treeNodeJSON    `json:"children"`
	Clickable  flagJSON          `json:"clickable"`
	Enabled    flagJSON          `json:"enabled"`
	Focused    flagJSON          `json:"focused"`
	Checked    flagJSON          `json:"checked"`
	Selected   flagJSON          `json:"selected"`
	Editable   flagJSON          `json:"editable"`
	Secure     flagJSON          `json:"secure"`
}

// flagJSON is one boolean field of a node. A value that is not a boolean
// leaves the flag unset and marks itself unreadable rather than failing the
// document: a dump is one observation of a whole screen, and one node's bit is
// no reason to discard every element on it. Malformed bounds are already
// treated this way.
type flagJSON struct {
	set        bool
	value      bool
	unreadable bool
}

func (f *flagJSON) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	var value bool
	if err := json.Unmarshal(data, &value); err != nil {
		f.unreadable = true
		return nil
	}
	f.set = true
	f.value = value
	return nil
}

func (n *treeNodeJSON) unreadableFlags() int {
	count := 0
	for _, flag := range []flagJSON{n.Clickable, n.Enabled, n.Focused, n.Checked, n.Selected, n.Editable, n.Secure} {
		if flag.unreadable {
			count++
		}
	}
	return count
}

// Selector describes a multi-attribute AND match.
type Selector struct {
	Filters []AttrFilter
}

// AttrFilter is a single attribute predicate within a Selector.
type AttrFilter struct {
	Attr  string
	Value string
}

// attributeAliases maps user-written attribute names to the actual keys present
// in the TreeNode attributes map. Both directions are listed so cross-platform
// matching works regardless of which name the caller uses.
var attributeAliases = map[string][]string{
	// Android XML legacy name; web driver uses content-desc; the sidecar normalises to accessibilityText
	"content-desc": {"accessibilityText"},
	// Every other name for the accessible label. Alias expansion is one level,
	// so each name lists both keys a producer writes the fact under rather than
	// hopping through accessibilityText: android and the chrome dump write
	// content-desc, the ios sidecar writes accessibilityText.
	"label":              {"accessibilityText", "content-desc"},
	"accessibilityLabel": {"accessibilityText", "content-desc"},
	"ariaLabel":          {"accessibilityText", "content-desc"},
	"contentDescription": {"accessibilityText", "content-desc"},
	// accessibilityText is the canonical key; also check content-desc for Android/web
	"accessibilityText": {"content-desc"},
	// resource-id canonical key; also check identifier (iOS AXElement raw field)
	"resource-id": {"identifier", "accessibilityIdentifier"},
	// iOS identifier names
	"identifier":              {"resource-id", "accessibilityIdentifier"},
	"accessibilityIdentifier": {"resource-id", "identifier"},
	// Compose testTag surfaces as resource-id on Android, accessibilityIdentifier
	// on iOS and data-testid on web, which is the key the web runtime resolves
	// both names against.
	"testTag": {"resource-id", "identifier", "accessibilityIdentifier", "data-testid"},
	"testID":  {"data-testid"},
	// iOS AXElement raw name for hintText
	"placeholderValue": {"hintText"},
	// iOS AXElement raw name for class
	"elementType": {"class"},
	// DOM property name for class; every producer writes the attribute as class
	"className": {"class"},
}

// selectorKeys is every key an object selector may use. It is the union of the
// selector kinds, the attribute names the drivers emit on some platform, and
// the cross-platform aliases, so a key that is meaningful on ONE platform stays
// silently empty on the others rather than failing the run there.
//
// pkg/spec/test/fixtures/selector-keys.json holds the same list for the web
// runtime; a test on each side asserts its own list against that file, which is
// what keeps one spec from being accepted by one runtime and rejected by the
// other.
var selectorKeys = []string{
	"accessibilityIdentifier",
	"accessibilityLabel",
	"accessibilityText",
	"aria-label",
	"ariaLabel",
	"checked",
	"class",
	"className",
	"clickable",
	"content-desc",
	"contentDescription",
	"data-testid",
	"desc",
	"descPrefix",
	"editable",
	"elementType",
	"enabled",
	"focused",
	"hintText",
	"id",
	"idPrefix",
	"identifier",
	"label",
	"package",
	"placeholder",
	"placeholderValue",
	"resource-id",
	"scrollable",
	"secure",
	"selected",
	"tag",
	"testID",
	"testTag",
	"text",
	"title",
	"value",
}

var selectorKeySet = func() map[string]bool {
	set := make(map[string]bool, len(selectorKeys))
	for _, key := range selectorKeys {
		set[key] = true
	}
	return set
}()

// SelectorKeys returns the accepted object-selector keys, sorted.
func SelectorKeys() []string {
	return slices.Clone(selectorKeys)
}

// UnknownSelectorKeys returns the keys in sel that name neither an accepted
// selector key nor an attribute some element in the tree carries. Such a key
// can never match: the caller gets an empty result on every screen, which reads
// exactly like a screen that has no matching element.
func (t *Tree) UnknownSelectorKeys(sel Selector) []string {
	if t == nil {
		return nil
	}
	var unknown []string
	for _, filter := range sel.Filters {
		if selectorKeySet[filter.Attr] || t.carriesAttribute(filter.Attr) {
			continue
		}
		if !slices.Contains(unknown, filter.Attr) {
			unknown = append(unknown, filter.Attr)
		}
	}
	return unknown
}

// carriesAttribute is the escape hatch for raw driver attributes this package
// does not enumerate: a key some element actually has is a key that can match.
func (t *Tree) carriesAttribute(key string) bool {
	for _, element := range t.Elements {
		if _, ok := element.Attributes[key]; ok {
			return true
		}
	}
	return false
}

// UnknownSelectorKeyMessage is the diagnostic for keys UnknownSelectorKeys
// returned. pkg/spec/src/web-runtime.ts raises the identical text, so one
// mistake reads the same whichever runtime the spec ran on.
func UnknownSelectorKeyMessage(keys []string) string {
	quoted := make([]string, len(keys))
	for i, key := range keys {
		quoted[i] = strconv.Quote(key)
	}
	return fmt.Sprintf(
		"selector key %s cannot match: no element carries that attribute, and it is not one of the accepted keys: %s",
		strings.Join(quoted, ", "),
		strings.Join(selectorKeys, ", "),
	)
}

// matchSelectorKind resolves the selector keys that name a matching rule rather
// than an attribute: they read a derived field and compare it their own way,
// where an ordinary key does a substring test against the raw attribute map.
// The second return is false when kind names an ordinary attribute.
//
// The string form and the object form both come through here, so one key cannot
// mean one thing in "id:save" and another in {id: "save"}. It used to: the
// object form fell through to the attribute map, which carries no `id` or
// `desc` key on any platform, so those selectors matched nothing at all and
// said nothing about it.
func matchSelectorKind(element *Element, kind, value string) (bool, bool) {
	switch kind {
	case "id":
		return element.ResourceID == value ||
			strings.HasSuffix(element.ResourceID, ":id/"+value), true
	case "idPrefix":
		return matchIDPrefix(element.ResourceID, value), true
	case "desc":
		return element.Description == value ||
			strings.HasPrefix(element.Description, value+", "), true
	case "descPrefix":
		return strings.HasPrefix(element.Description, value), true
	case "tag":
		// Both DOM resolvers compile this to a CSS type selector, which is the
		// whole tag name. A substring rule here made {tag: "li"} name
		// <todo-list> and {tag: "a"} name <todo-app>, so a selector meant for a
		// row resolved to the container holding it.
		tag, ok := element.Attributes["tag"]
		return ok && tag == value, true
	default:
		return false, false
	}
}

// matchIDPrefix is the id: rule with starts-with in place of equality: the
// whole identifier, or the local name after Android's "<package>:id/". Without
// the second form a role prefix would only match when the caller wrote the
// package out, which is exactly the string that varies between build variants.
func matchIDPrefix(resourceID, value string) bool {
	if strings.HasPrefix(resourceID, value) {
		return true
	}
	const marker = ":id/"
	if index := strings.Index(resourceID, marker); index >= 0 {
		return strings.HasPrefix(resourceID[index+len(marker):], value)
	}
	return false
}

// matchAttr returns true when the element matches key:value. It is the one
// entry point for both selector forms: the string form's kind and the object
// form's key are the same name and get the same rule.
//
// Keys naming a rule (id, desc and the prefix forms) resolve in
// matchSelectorKind; everything else is an attribute name, with alias expansion
// so cross-platform names resolve correctly. Boolean values ("true"/"false")
// use exact comparison; all others use substring. Returns false gracefully when
// no candidate attribute has data.
func matchAttr(element *Element, attr, value string) bool {
	if matched, handled := matchSelectorKind(element, attr, value); handled {
		return matched
	}
	candidates := append([]string{attr}, attributeAliases[attr]...)
	for _, key := range candidates {
		attrVal, ok := element.Attributes[key]
		if !ok || attrVal == "" {
			continue
		}
		if value == "true" || value == "false" {
			if attrVal == value {
				return true
			}
		} else {
			if strings.Contains(attrVal, value) {
				return true
			}
		}
	}
	return false
}

// matchSelector returns true when all filters in sel match the element (AND
// semantics). Each filter goes through matchAttr, the same rule the string form
// resolves a "kind:value" segment by, so {id: "Submit"} and "id:Submit" can
// never resolve to different elements. Reaching the attribute map directly here
// made the object form skip the kind arms entirely: id, desc and descPrefix
// name no attribute any producer writes, so those keys matched NOTHING through
// an object selector while the string form matched, and every property over the
// missing element passed vacuously.
func matchSelector(element *Element, sel Selector) bool {
	for _, f := range sel.Filters {
		if !matchAttr(element, f.Attr, f.Value) {
			return false
		}
	}
	return true
}

func selectorReadsText(sel Selector) bool {
	for _, f := range sel.Filters {
		if f.Attr == "text" {
			return true
		}
	}
	return false
}

// innermostMatches drops a match a descendant of it also makes. An element's
// text is its whole subtree's text on web and on iOS, so every ancestor of a
// matching element matches too, up to the root, and the deepest match is the
// element the author named. An ancestor whose own text carries the value where
// no descendant of it does keeps its match.
func innermostMatches(nodes []*Node) []*Node {
	if len(nodes) == 0 {
		return nodes
	}
	matched := make(map[*Node]bool, len(nodes))
	for _, node := range nodes {
		matched[node] = true
	}
	var kept []*Node
	for _, node := range nodes {
		if !hasMatchingDescendant(node, matched) {
			kept = append(kept, node)
		}
	}
	return kept
}

func hasMatchingDescendant(node *Node, matched map[*Node]bool) bool {
	for _, child := range node.Children {
		if matched[child] || hasMatchingDescendant(child, matched) {
			return true
		}
	}
	return false
}

// Parse parses a sidecar TreeNode JSON hierarchy.
func Parse(text string) (*Tree, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return &Tree{}, nil
	}
	var root treeNodeJSON
	if err := json.Unmarshal([]byte(text), &root); err != nil {
		return nil, fmt.Errorf("hierarchy: %w", err)
	}
	tree := &Tree{}
	tree.Root = walkNode(&root, tree)
	return tree, nil
}

func walkNode(node *treeNodeJSON, tree *Tree) *Node {
	tree.UnreadableFlags += node.unreadableFlags()
	n := &Node{Element: *elementFromNode(node), tree: tree}
	tree.Elements = append(tree.Elements, &n.Element)
	for i := range node.Children {
		n.Children = append(n.Children, walkNode(&node.Children[i], tree))
	}
	return n
}

func elementFromNode(node *treeNodeJSON) *Element {
	attrs := node.Attributes
	element := &Element{}

	element.ResourceID = attrs["resource-id"]
	if element.ResourceID == "" {
		element.ResourceID = attrs["identifier"]
	}
	if element.ResourceID == "" {
		element.ResourceID = attrs["accessibilityIdentifier"]
	}
	element.Text = attrs["text"]
	element.Description = attrs["content-desc"]
	if element.Description == "" {
		element.Description = attrs["accessibilityText"]
	}
	element.Class = attrs["class"]
	element.Package = attrs["package"]
	if element.Package == "" {
		// Android omits an explicit package attribute, but native views carry
		// it as the resource-id prefix (`com.android.systemui:id/...`). Compose
		// testTags are colon-less and leave the package empty, which keeps them
		// in scope. This lets target selection tell the app apart from the soft
		// keyboard and system UI.
		if resourceID := attrs["resource-id"]; resourceID != "" {
			if colon := strings.IndexByte(resourceID, ':'); colon > 0 {
				element.Package = resourceID[:colon]
			}
		}
	}
	element.Screen = attrs["sanderling-screen"]

	if node.Clickable.set {
		element.Clickable = node.Clickable.value
	}
	if node.Enabled.set {
		element.Enabled = node.Enabled.value
	}
	if node.Focused.set {
		element.Focused = node.Focused.value
	}
	if node.Checked.set {
		element.Checked = node.Checked.value
	}
	if node.Selected.set {
		element.Selected = node.Selected.value
	}
	if node.Secure.set {
		element.Secure = node.Secure.value
	}
	if node.Editable.set {
		element.Editable = node.Editable.value
	} else {
		element.Editable = strings.Contains(element.Class, "EditText") || attrs["hintText"] != ""
	}

	if b, ok := attrs["bounds"]; ok && b != "" {
		bounds, err := parseBounds(b)
		if err == nil {
			element.Bounds = bounds
		}
	}

	element.Attributes = make(map[string]string, len(attrs)+5)
	maps.Copy(element.Attributes, attrs)
	if node.Clickable.set {
		element.Attributes["clickable"] = strconv.FormatBool(node.Clickable.value)
	}
	if node.Enabled.set {
		element.Attributes["enabled"] = strconv.FormatBool(node.Enabled.value)
	}
	if node.Focused.set {
		element.Attributes["focused"] = strconv.FormatBool(node.Focused.value)
	}
	if node.Checked.set {
		element.Attributes["checked"] = strconv.FormatBool(node.Checked.value)
	}
	if node.Selected.set {
		element.Attributes["selected"] = strconv.FormatBool(node.Selected.value)
	}
	if node.Secure.set {
		element.Attributes["secure"] = strconv.FormatBool(node.Secure.value)
	}
	element.Attributes["editable"] = strconv.FormatBool(element.Editable)

	return element
}

// Transitional reports more than one resource id ending in "Screen": the marker
// of a Compose NavHost mid cross-fade, where the source and destination route
// composables are both alive in a collapsed, mid-animation layout.
func (t *Tree) Transitional() bool {
	if t == nil {
		return false
	}
	screens := 0
	for _, element := range t.Elements {
		if strings.HasSuffix(element.ResourceID, "Screen") {
			screens++
			if screens > 1 {
				return true
			}
		}
	}
	return false
}

// ScreenName names the route this tree shows: the driver-set screen when the
// platform reports one (web), otherwise the resource id ending in "Screen" that
// marks the route composable. A transitional tree names no screen.
func (t *Tree) ScreenName() string {
	if t == nil || len(t.Elements) == 0 {
		return ""
	}
	if screen := t.Elements[0].Screen; screen != "" {
		return screen
	}
	if t.Transitional() {
		return ""
	}
	for _, element := range t.Elements {
		if strings.HasSuffix(element.ResourceID, "Screen") {
			return element.ResourceID
		}
	}
	return ""
}

// Find returns the first element matching the selector, or nil.
func (t *Tree) Find(selector string) *Element {
	node := t.FindNode(selector)
	if node == nil {
		return nil
	}
	return &node.Element
}

// FindAll returns every element matching the selector.
func (t *Tree) FindAll(selector string) []*Element {
	nodes := t.FindAllNodes(selector)
	elements := make([]*Element, len(nodes))
	for i, n := range nodes {
		elements[i] = &n.Element
	}
	return elements
}

// FindNode returns the first Node matching the selector, or nil.
func (t *Tree) FindNode(selector string) *Node {
	if strings.Contains(selector, " > ") {
		return findPathNode(t.Root, strings.Split(selector, " > "))
	}
	kind, value, ok := parseSelector(selector)
	if !ok {
		return nil
	}
	nodes := searchSubtree(t.Root, kind, value)
	if len(nodes) == 0 {
		return nil
	}
	return nodes[0]
}

// FindAllNodes returns every Node matching the selector.
func (t *Tree) FindAllNodes(selector string) []*Node {
	if strings.Contains(selector, " > ") {
		return findPathAllNodes(t.Root, strings.Split(selector, " > "))
	}
	kind, value, ok := parseSelector(selector)
	if !ok {
		return nil
	}
	return searchSubtree(t.Root, kind, value)
}

// FindBySelector returns the first Node in the tree matching sel, or nil. The
// root is a candidate, the way it is for the string form: one selector cannot
// mean one thing written "id:page" and another written {id: "page"}.
func (t *Tree) FindBySelector(sel Selector) *Node {
	if t == nil {
		return nil
	}
	return firstNode(searchSubtreeBySelector(t.Root, sel))
}

// FindAllBySelector returns every Node in the tree matching sel, root included.
func (t *Tree) FindAllBySelector(sel Selector) []*Node {
	if t == nil {
		return nil
	}
	return searchSubtreeBySelector(t.Root, sel)
}

// FindBySelectorPath walks the selector chain starting from the tree root.
func (t *Tree) FindBySelectorPath(path []Selector) *Node {
	if t == nil || t.Root == nil || len(path) == 0 {
		return nil
	}
	for _, candidate := range t.FindAllBySelector(path[0]) {
		if len(path) == 1 {
			return candidate
		}
		if deeper := candidate.FindBySelectorPath(path[1:]); deeper != nil {
			return deeper
		}
	}
	return nil
}

// FindAllBySelectorPath walks the selector chain starting from the tree root.
func (t *Tree) FindAllBySelectorPath(path []Selector) []*Node {
	if t == nil || t.Root == nil || len(path) == 0 {
		return nil
	}
	var result []*Node
	for _, candidate := range t.FindAllBySelector(path[0]) {
		if len(path) == 1 {
			result = append(result, candidate)
			continue
		}
		result = append(result, candidate.FindAllBySelectorPath(path[1:])...)
	}
	return result
}

// Find returns the first Node scoped to this node (descendants, with spatial
// fallback) matching the string selector. Path queries within the selector are
// not supported here.
func (n *Node) Find(selector string) *Node {
	return firstNode(n.FindAll(selector))
}

// FindAll returns all Nodes scoped to this node (descendants, with spatial
// fallback) matching the string selector.
func (n *Node) FindAll(selector string) []*Node {
	kind, value, ok := parseSelector(selector)
	if !ok {
		return nil
	}
	nodes := n.scopedNodes(func(element *Element) bool {
		return matchAttr(element, kind, value)
	})
	if kind == "text" {
		return innermostMatches(nodes)
	}
	return nodes
}

// FindBySelector returns the first Node scoped to this node matching sel (AND semantics).
func (n *Node) FindBySelector(sel Selector) *Node {
	return firstNode(n.FindAllBySelector(sel))
}

// FindAllBySelector returns all Nodes scoped to this node matching sel (AND semantics).
func (n *Node) FindAllBySelector(sel Selector) []*Node {
	nodes := n.scopedNodes(func(element *Element) bool {
		return matchSelector(element, sel)
	})
	if selectorReadsText(sel) {
		return innermostMatches(nodes)
	}
	return nodes
}

// FindBySelectorPath walks a chain of selectors. The first selector is matched
// in the receiver's scope; each subsequent selector is matched in the scope of
// the previous match. Returns the deepest match or nil.
func (n *Node) FindBySelectorPath(path []Selector) *Node {
	if len(path) == 0 {
		return nil
	}
	for _, candidate := range n.FindAllBySelector(path[0]) {
		if len(path) == 1 {
			return candidate
		}
		if deeper := candidate.FindBySelectorPath(path[1:]); deeper != nil {
			return deeper
		}
	}
	return nil
}

// FindAllBySelectorPath returns every deepest match for the selector chain
// scoped under the receiver.
func (n *Node) FindAllBySelectorPath(path []Selector) []*Node {
	if len(path) == 0 {
		return nil
	}
	var result []*Node
	for _, candidate := range n.FindAllBySelector(path[0]) {
		if len(path) == 1 {
			result = append(result, candidate)
			continue
		}
		result = append(result, candidate.FindAllBySelectorPath(path[1:])...)
	}
	return result
}

func firstNode(nodes []*Node) *Node {
	if len(nodes) == 0 {
		return nil
	}
	return nodes[0]
}

// scopedNodes returns this node's descendants matching accept, in pre-order.
// When no descendant matches, nodes spatially contained in this node's bounds
// are matched instead. Compose on iOS emits a testTag node as an empty leaf
// sibling of the content it labels rather than as an ancestor, so descendant
// search under the tagged node finds nothing; bounds containment recovers the
// intended scope.
func (n *Node) scopedNodes(accept func(*Element) bool) []*Node {
	var result []*Node
	for _, child := range n.Children {
		collectMatches(child, accept, &result)
	}
	if len(result) > 0 {
		return result
	}
	for _, candidate := range n.spatialScope() {
		if accept(&candidate.Element) {
			result = append(result, candidate)
		}
	}
	// Spatial containment alone lets a large container outrank the intended
	// small element; the most specific (smallest) match wins instead.
	sortBySpecificity(result)
	return result
}

// sortBySpecificity orders nodes ascending by bounds area, so the smallest
// (most specific) containing match comes first. Equal-area nodes keep their
// pre-order position.
func sortBySpecificity(nodes []*Node) {
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].Bounds.Width()*nodes[i].Bounds.Height() <
			nodes[j].Bounds.Width()*nodes[j].Bounds.Height()
	})
}

func collectMatches(node *Node, accept func(*Element) bool, result *[]*Node) {
	if accept(&node.Element) {
		*result = append(*result, node)
	}
	for _, child := range node.Children {
		collectMatches(child, accept, result)
	}
}

// spatialScope returns every node in the tree, in pre-order, whose positive
// bounds lie fully inside this node's bounds, excluding the node itself.
func (n *Node) spatialScope() []*Node {
	if n.tree == nil || n.tree.Root == nil {
		return nil
	}
	if n.Bounds.Width() <= 0 || n.Bounds.Height() <= 0 {
		return nil
	}
	var result []*Node
	var walk func(*Node)
	walk = func(candidate *Node) {
		if candidate != n &&
			candidate.Bounds.Width() > 0 && candidate.Bounds.Height() > 0 &&
			containsBounds(n.Bounds, candidate.Bounds) {
			result = append(result, candidate)
		}
		for _, child := range candidate.Children {
			walk(child)
		}
	}
	walk(n.tree.Root)
	return result
}

// containsBounds reports whether outer fully contains inner (inclusive).
func containsBounds(outer, inner Bounds) bool {
	return inner.Left >= outer.Left && inner.Top >= outer.Top &&
		inner.Right <= outer.Right && inner.Bottom <= outer.Bottom
}

func findPathNode(root *Node, segments []string) *Node {
	if root == nil || len(segments) == 0 {
		return nil
	}
	kind, value, ok := parseSelector(segments[0])
	if !ok {
		return nil
	}
	for _, node := range searchSubtree(root, kind, value) {
		if len(segments) == 1 {
			return node
		}
		if result := findPathDescendantsNode(node, segments[1:]); result != nil {
			return result
		}
	}
	return nil
}

func findPathDescendantsNode(root *Node, segments []string) *Node {
	for _, node := range root.FindAll(segments[0]) {
		if len(segments) == 1 {
			return node
		}
		if result := findPathDescendantsNode(node, segments[1:]); result != nil {
			return result
		}
	}
	return nil
}

func findPathAllNodes(root *Node, segments []string) []*Node {
	if root == nil || len(segments) == 0 {
		return nil
	}
	kind, value, ok := parseSelector(segments[0])
	if !ok {
		return nil
	}
	var result []*Node
	for _, node := range searchSubtree(root, kind, value) {
		if len(segments) == 1 {
			result = append(result, node)
			continue
		}
		result = append(result, findPathAllDescendantsNodes(node, segments[1:])...)
	}
	return result
}

func findPathAllDescendantsNodes(root *Node, segments []string) []*Node {
	var result []*Node
	for _, node := range root.FindAll(segments[0]) {
		if len(segments) == 1 {
			result = append(result, node)
			continue
		}
		result = append(result, findPathAllDescendantsNodes(node, segments[1:])...)
	}
	return result
}

// searchSubtree returns all nodes under root (inclusive) matching kind:value.
func searchSubtree(root *Node, kind, value string) []*Node {
	if root == nil {
		return nil
	}
	var result []*Node
	collectMatches(root, func(element *Element) bool {
		return matchAttr(element, kind, value)
	}, &result)
	if kind == "text" {
		return innermostMatches(result)
	}
	return result
}

// searchSubtreeBySelector returns all nodes under root (inclusive) matching sel.
func searchSubtreeBySelector(root *Node, sel Selector) []*Node {
	if root == nil {
		return nil
	}
	var result []*Node
	collectMatches(root, func(element *Element) bool {
		return matchSelector(element, sel)
	}, &result)
	if selectorReadsText(sel) {
		return innermostMatches(result)
	}
	return result
}

func parseSelector(selector string) (string, string, bool) {
	index := strings.IndexByte(selector, ':')
	if index <= 0 {
		return "", "", false
	}
	return selector[:index], selector[index+1:], true
}

// boundsPattern matches "[l,t,r,b]" (4-value Android/sidecar format).
var boundsPattern = regexp.MustCompile(`^\[(-?\d+),(-?\d+),(-?\d+),(-?\d+)\]$`)

// boundsPatternTwo matches "[x1,y1][x2,y2]" (iOS XCUITest format).
var boundsPatternTwo = regexp.MustCompile(`^\[(-?\d+),(-?\d+)\]\[(-?\d+),(-?\d+)\]$`)

func parseBounds(text string) (Bounds, error) {
	if m := boundsPattern.FindStringSubmatch(text); m != nil {
		coords := make([]int, 4)
		for i := range 4 {
			v, err := strconv.Atoi(m[i+1])
			if err != nil {
				return Bounds{}, err
			}
			coords[i] = v
		}
		return Bounds{Left: coords[0], Top: coords[1], Right: coords[2], Bottom: coords[3]}, nil
	}
	if m := boundsPatternTwo.FindStringSubmatch(text); m != nil {
		coords := make([]int, 4)
		for i := range 4 {
			v, err := strconv.Atoi(m[i+1])
			if err != nil {
				return Bounds{}, err
			}
			coords[i] = v
		}
		return Bounds{Left: coords[0], Top: coords[1], Right: coords[2], Bottom: coords[3]}, nil
	}
	return Bounds{}, fmt.Errorf("bounds %q: not in [L,T,R,B] or [x1,y1][x2,y2] form", text)
}

// Tree returns the tree this node belongs to, or nil for a node built outside
// Parse. Selector validation needs the whole tree: a key absent from one
// subtree but present elsewhere is a key that can match.
func (n *Node) Tree() *Tree {
	if n == nil {
		return nil
	}
	return n.tree
}
