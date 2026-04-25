// Package hierarchy parses the TreeNode JSON produced by the Maestro sidecar
// and resolves selectors against it.
//
// Selector grammar (v1.0):
//
//   Single selectors (global scan):
//     id:<suffix>         - resource-id == suffix or ends with ":id/<suffix>"
//     text:<value>        - exact text match
//     desc:<value>        - exact content-desc match
//     descPrefix:<prefix> - content-desc starts with prefix
//
//   Path queries (segments separated by " > "):
//     <sel> > <sel> > ... - each segment is matched within the subtree of the
//                           previous match (any descendant, not just direct child)
//     example: id:LoginScreen > desc:EmailInput
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
	Screen    string `json:"screen,omitempty"`
	Clickable bool   `json:"clickable,omitempty"`
	Enabled   bool   `json:"enabled,omitempty"`
	Checked   bool   `json:"checked,omitempty"`
	Focused   bool   `json:"focused,omitempty"`
	Selected  bool   `json:"selected,omitempty"`
	Bounds    Bounds `json:"bounds"`
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

	return element
}

// Find returns the first element matching the selector, or nil.
func (t *Tree) Find(selector string) *Element {
	if strings.Contains(selector, " > ") {
		return findPath(t.Root, strings.Split(selector, " > "))
	}
	kind, value, ok := parseSelector(selector)
	if !ok {
		return nil
	}
	for _, element := range t.Elements {
		if match(element, kind, value) {
			return element
		}
	}
	return nil
}

// FindAll returns every element matching the selector.
func (t *Tree) FindAll(selector string) []*Element {
	if strings.Contains(selector, " > ") {
		return findPathAll(t.Root, strings.Split(selector, " > "))
	}
	kind, value, ok := parseSelector(selector)
	if !ok {
		return nil
	}
	var matches []*Element
	for _, element := range t.Elements {
		if match(element, kind, value) {
			matches = append(matches, element)
		}
	}
	return matches
}

func findPath(root *Node, segments []string) *Element {
	if root == nil || len(segments) == 0 {
		return nil
	}
	kind, value, ok := parseSelector(segments[0])
	if !ok {
		return nil
	}
	for _, node := range searchSubtree(root, kind, value) {
		if len(segments) == 1 {
			return &node.Element
		}
		if result := findPathDescendants(node, segments[1:]); result != nil {
			return result
		}
	}
	return nil
}

func findPathDescendants(root *Node, segments []string) *Element {
	kind, value, ok := parseSelector(segments[0])
	if !ok {
		return nil
	}
	for _, child := range root.Children {
		for _, node := range searchSubtree(child, kind, value) {
			if len(segments) == 1 {
				return &node.Element
			}
			if result := findPathDescendants(node, segments[1:]); result != nil {
				return result
			}
		}
	}
	return nil
}

func findPathAll(root *Node, segments []string) []*Element {
	if root == nil || len(segments) == 0 {
		return nil
	}
	kind, value, ok := parseSelector(segments[0])
	if !ok {
		return nil
	}
	var result []*Element
	for _, node := range searchSubtree(root, kind, value) {
		if len(segments) == 1 {
			result = append(result, &node.Element)
			continue
		}
		result = append(result, findPathAllDescendants(node, segments[1:])...)
	}
	return result
}

func findPathAllDescendants(root *Node, segments []string) []*Element {
	kind, value, ok := parseSelector(segments[0])
	if !ok {
		return nil
	}
	var result []*Element
	for _, child := range root.Children {
		for _, node := range searchSubtree(child, kind, value) {
			if len(segments) == 1 {
				result = append(result, &node.Element)
				continue
			}
			result = append(result, findPathAllDescendants(node, segments[1:])...)
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
		return element.Text == value
	case "desc":
		// Exact match, or iOS merged label "desc, child text".
		return element.Description == value || strings.HasPrefix(element.Description, value+", ")
	case "descPrefix":
		return strings.HasPrefix(element.Description, value)
	default:
		return false
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
