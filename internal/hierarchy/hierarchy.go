// Package hierarchy parses the TreeNode JSON produced by the native sidecar
// and resolves selectors against it.
//
// Selector grammar (v2.0):
//
//	String selectors (global scan or element-scoped):
//	  attribute:value      - substring match; exact for "true"/"false" booleans
//	  id:<suffix>          - substring on resource-id / identifier (backward compat)
//	  text:<value>         - substring on text attribute
//	  desc:<value>         - substring on content-desc / accessibilityText
//	  descPrefix:<prefix>  - starts-with on content-desc / accessibilityText
//
//	Object selectors (multi-attribute AND, element-scoped or global):
//	  { attr: value, ... } - all key/value pairs must match; substring / boolean semantics
//
//	Path queries (global scan only, string form):
//	  <sel> > <sel> > ...  - each segment matched within subtree of previous match
//
// Cross-platform aliases are expanded automatically: "label" / "accessibilityLabel"
// resolve to accessibilityText; "content-desc" also checks accessibilityText and
// vice-versa; "identifier" / "accessibilityIdentifier" / "testTag" resolve to
// resource-id (and to each other) so a Compose testTag matches whether the
// underlying platform exposes it as resource-id (Android) or accessibilityIdentifier (iOS).
package hierarchy

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
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
	Bounds     Bounds            `json:"bounds"`
	Attributes map[string]string `json:"attrs,omitempty"`
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
}

// treeNodeJSON mirrors the sidecar TreeNode JSON structure.
type treeNodeJSON struct {
	Attributes map[string]string `json:"attributes"`
	Children   []treeNodeJSON    `json:"children"`
	Clickable  *bool             `json:"clickable"`
	Enabled    *bool             `json:"enabled"`
	Focused    *bool             `json:"focused"`
	Checked    *bool             `json:"checked"`
	Selected   *bool             `json:"selected"`
	Editable   *bool             `json:"editable"`
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
	// iOS AXElement / UIKit names
	"label":              {"accessibilityText"},
	"accessibilityLabel": {"accessibilityText"},
	// accessibilityText is the canonical key; also check content-desc for Android/web
	"accessibilityText": {"content-desc"},
	// resource-id canonical key; also check identifier (iOS AXElement raw field)
	"resource-id": {"identifier", "accessibilityIdentifier"},
	// iOS identifier names
	"identifier":              {"resource-id", "accessibilityIdentifier"},
	"accessibilityIdentifier": {"resource-id", "identifier"},
	// Compose testTag surfaces as resource-id on Android, accessibilityIdentifier on iOS
	"testTag": {"resource-id", "identifier", "accessibilityIdentifier"},
	// iOS AXElement raw name for hintText
	"placeholderValue": {"hintText"},
	// iOS AXElement raw name for class
	"elementType": {"class"},
}

// matchAttr returns true when the element has an attribute matching attr:value.
// Alias expansion is applied so cross-platform names resolve correctly.
// Boolean values ("true"/"false") use exact comparison; all others use substring.
// Returns false gracefully when no candidate attribute has data.
func matchAttr(element *Element, attr, value string) bool {
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

// matchSelector returns true when all filters in sel match the element (AND semantics).
func matchSelector(element *Element, sel Selector) bool {
	for _, f := range sel.Filters {
		if !matchAttr(element, f.Attr, f.Value) {
			return false
		}
	}
	return true
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

	if node.Clickable != nil {
		element.Clickable = *node.Clickable
	}
	if node.Enabled != nil {
		element.Enabled = *node.Enabled
	}
	if node.Focused != nil {
		element.Focused = *node.Focused
	}
	if node.Checked != nil {
		element.Checked = *node.Checked
	}
	if node.Selected != nil {
		element.Selected = *node.Selected
	}
	if node.Editable != nil {
		element.Editable = *node.Editable
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
	if node.Clickable != nil {
		element.Attributes["clickable"] = strconv.FormatBool(*node.Clickable)
	}
	if node.Enabled != nil {
		element.Attributes["enabled"] = strconv.FormatBool(*node.Enabled)
	}
	if node.Focused != nil {
		element.Attributes["focused"] = strconv.FormatBool(*node.Focused)
	}
	if node.Checked != nil {
		element.Attributes["checked"] = strconv.FormatBool(*node.Checked)
	}
	if node.Selected != nil {
		element.Attributes["selected"] = strconv.FormatBool(*node.Selected)
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

// FindBySelectorPath walks the selector chain starting from the tree root.
func (t *Tree) FindBySelectorPath(path []Selector) *Node {
	if t == nil || t.Root == nil {
		return nil
	}
	return t.Root.FindBySelectorPath(path)
}

// FindAllBySelectorPath walks the selector chain starting from the tree root.
func (t *Tree) FindAllBySelectorPath(path []Selector) []*Node {
	if t == nil || t.Root == nil {
		return nil
	}
	return t.Root.FindAllBySelectorPath(path)
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
	return n.scopedNodes(func(element *Element) bool {
		return match(element, kind, value)
	})
}

// FindBySelector returns the first Node scoped to this node matching sel (AND semantics).
func (n *Node) FindBySelector(sel Selector) *Node {
	return firstNode(n.FindAllBySelector(sel))
}

// FindAllBySelector returns all Nodes scoped to this node matching sel (AND semantics).
func (n *Node) FindAllBySelector(sel Selector) []*Node {
	return n.scopedNodes(func(element *Element) bool {
		return matchSelector(element, sel)
	})
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
	if match(&root.Element, kind, value) {
		result = append(result, root)
	}
	for _, child := range root.Children {
		result = append(result, searchSubtree(child, kind, value)...)
	}
	return result
}

// searchSubtreeBySelector returns all nodes under root (inclusive) matching sel.
func searchSubtreeBySelector(root *Node, sel Selector) []*Node {
	if root == nil {
		return nil
	}
	var result []*Node
	if matchSelector(&root.Element, sel) {
		result = append(result, root)
	}
	for _, child := range root.Children {
		result = append(result, searchSubtreeBySelector(child, sel)...)
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

func match(element *Element, kind, value string) bool {
	switch kind {
	case "id":
		if element.ResourceID == value {
			return true
		}
		return strings.HasSuffix(element.ResourceID, ":id/"+value)
	case "text":
		return matchAttr(element, "text", value)
	case "desc":
		return element.Description == value || strings.HasPrefix(element.Description, value+", ")
	case "descPrefix":
		return strings.HasPrefix(element.Description, value)
	default:
		return matchAttr(element, kind, value)
	}
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
