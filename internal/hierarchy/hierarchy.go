// Package hierarchy parses the TreeNode JSON produced by the Maestro sidecar
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
	"regexp"
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
	Bounds     Bounds            `json:"bounds"`
	Attributes map[string]string `json:"attrs,omitempty"`
}

// Node is one node in the hierarchy tree.
type Node struct {
	Element
	Children []*Node `json:"-"`
}

// Tree is a flat collection of every node in a hierarchy dump, in pre-order.
type Tree struct {
	Root     *Node      `json:"-"`
	Elements []*Element `json:"elements"`
}

// treeNodeJSON mirrors the Maestro TreeNode JSON structure.
type treeNodeJSON struct {
	Attributes map[string]string `json:"attributes"`
	Children   []treeNodeJSON    `json:"children"`
	Clickable  *bool             `json:"clickable"`
	Enabled    *bool             `json:"enabled"`
	Focused    *bool             `json:"focused"`
	Checked    *bool             `json:"checked"`
	Selected   *bool             `json:"selected"`
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
	// Android XML legacy name; web driver uses content-desc; Maestro normalises to accessibilityText
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

// Parse parses a Maestro TreeNode JSON hierarchy.
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
	n := &Node{Element: *elementFromNode(node)}
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

	if b, ok := attrs["bounds"]; ok && b != "" {
		bounds, err := parseBounds(b)
		if err == nil {
			element.Bounds = bounds
		}
	}

	element.Attributes = make(map[string]string, len(attrs)+5)
	for k, v := range attrs {
		element.Attributes[k] = v
	}
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

	return element
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

// Find returns the first Node in this node's subtree (descendants only) matching
// the string selector. Path queries within the selector are not supported here.
func (n *Node) Find(selector string) *Node {
	kind, value, ok := parseSelector(selector)
	if !ok {
		return nil
	}
	for _, child := range n.Children {
		if nodes := searchSubtree(child, kind, value); len(nodes) > 0 {
			return nodes[0]
		}
	}
	return nil
}

// FindAll returns all Nodes in this node's subtree (descendants only) matching
// the string selector.
func (n *Node) FindAll(selector string) []*Node {
	kind, value, ok := parseSelector(selector)
	if !ok {
		return nil
	}
	var result []*Node
	for _, child := range n.Children {
		result = append(result, searchSubtree(child, kind, value)...)
	}
	return result
}

// FindBySelector returns the first Node in this node's subtree matching sel (AND semantics).
func (n *Node) FindBySelector(sel Selector) *Node {
	for _, child := range n.Children {
		if nodes := searchSubtreeBySelector(child, sel); len(nodes) > 0 {
			return nodes[0]
		}
	}
	return nil
}

// FindAllBySelector returns all Nodes in this node's subtree matching sel (AND semantics).
func (n *Node) FindAllBySelector(sel Selector) []*Node {
	var result []*Node
	for _, child := range n.Children {
		result = append(result, searchSubtreeBySelector(child, sel)...)
	}
	return result
}

// FindBySelectorPath walks a chain of selectors. The first selector is matched
// against descendants of the receiver; each subsequent selector is matched
// against descendants of the previous match. Returns the deepest match or nil.
func (n *Node) FindBySelectorPath(path []Selector) *Node {
	if len(path) == 0 {
		return nil
	}
	for _, child := range n.Children {
		for _, candidate := range searchSubtreeBySelector(child, path[0]) {
			if len(path) == 1 {
				return candidate
			}
			if deeper := candidate.FindBySelectorPath(path[1:]); deeper != nil {
				return deeper
			}
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
	for _, child := range n.Children {
		for _, candidate := range searchSubtreeBySelector(child, path[0]) {
			if len(path) == 1 {
				result = append(result, candidate)
				continue
			}
			result = append(result, candidate.FindAllBySelectorPath(path[1:])...)
		}
	}
	return result
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
	kind, value, ok := parseSelector(segments[0])
	if !ok {
		return nil
	}
	for _, child := range root.Children {
		for _, node := range searchSubtree(child, kind, value) {
			if len(segments) == 1 {
				return node
			}
			if result := findPathDescendantsNode(node, segments[1:]); result != nil {
				return result
			}
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
	kind, value, ok := parseSelector(segments[0])
	if !ok {
		return nil
	}
	var result []*Node
	for _, child := range root.Children {
		for _, node := range searchSubtree(child, kind, value) {
			if len(segments) == 1 {
				result = append(result, node)
				continue
			}
			result = append(result, findPathAllDescendantsNodes(node, segments[1:])...)
		}
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

// boundsPattern matches "[l,t,r,b]" (4-value Android/Maestro format).
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
