// Package hierarchy parses the TreeNode JSON produced by the Maestro sidecar
// and resolves selectors against it.
//
// Selector grammar (v0.1):
//
//	id:<suffix>            — match resource-id ending with ":id/<suffix>" or equal to <suffix>
//	text:<value>           — exact match on the node's text
//	desc:<value>           — exact match on the node's content-desc
//	descPrefix:<prefix>    — startsWith match on the node's content-desc (e.g. Compose testTag + UUID)
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
	Clickable   bool   `json:"clickable,omitempty"`
	Enabled     bool   `json:"enabled,omitempty"`
	Checked     bool   `json:"checked,omitempty"`
	Focused     bool   `json:"focused,omitempty"`
	Selected    bool   `json:"selected,omitempty"`
	Bounds      Bounds `json:"bounds"`
}

// Tree is a flat collection of every node in a hierarchy dump, in pre-order.
type Tree struct {
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
	walkNode(&root, tree)
	return tree, nil
}

func walkNode(node *treeNodeJSON, tree *Tree) {
	element := elementFromNode(node)
	tree.Elements = append(tree.Elements, element)
	for i := range node.Children {
		walkNode(&node.Children[i], tree)
	}
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
		return element.Description == value
	case "descPrefix":
		return strings.HasPrefix(element.Description, value)
	default:
		return false
	}
}

// boundsPattern matches "[l,t,r,b]" (4-value Maestro format).
var boundsPattern = regexp.MustCompile(`^\[(-?\d+),(-?\d+),(-?\d+),(-?\d+)\]$`)

func parseBounds(text string) (Bounds, error) {
	m := boundsPattern.FindStringSubmatch(text)
	if m == nil {
		return Bounds{}, fmt.Errorf("bounds %q: not in [L,T,R,B] form", text)
	}
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
