// Package hierarchy parses the XML produced by `uiautomator dump` and
// resolves selectors against it.
//
// Selector grammar (v0.1):
//
//	id:<suffix>     — match resource-id ending with ":id/<suffix>" or equal to <suffix>
//	text:<value>    — exact match on the node's text
//	desc:<value>    — exact match on the node's content-desc
package hierarchy

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Bounds is an inclusive rectangle in device pixels.
type Bounds struct {
	Left, Top, Right, Bottom int
}

// Center returns the center point of the bounds.
func (b Bounds) Center() (int, int) {
	return (b.Left + b.Right) / 2, (b.Top + b.Bottom) / 2
}

// Width returns the bounds' width.
func (b Bounds) Width() int { return b.Right - b.Left }

// Height returns the bounds' height.
func (b Bounds) Height() int { return b.Bottom - b.Top }

// Element is a flattened view of one uiautomator node.
type Element struct {
	ResourceID  string
	Text        string
	Description string
	Class       string
	Package     string
	Clickable   bool
	Enabled     bool
	Bounds      Bounds
}

// Tree is a flat collection of every node in a hierarchy dump, in pre-order.
type Tree struct {
	Elements []*Element
}

// Parse parses a uiautomator-style XML dump.
func Parse(xmlText string) (*Tree, error) {
	xmlText = strings.TrimSpace(xmlText)
	if xmlText == "" {
		return &Tree{}, nil
	}
	decoder := xml.NewDecoder(strings.NewReader(xmlText))
	tree := &Tree{}
	for {
		token, err := decoder.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local != "node" {
			continue
		}
		element, parseErr := elementFromStart(start)
		if parseErr != nil {
			return nil, parseErr
		}
		tree.Elements = append(tree.Elements, element)
	}
	return tree, nil
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
		// ResourceID looks like "<package>:id/<suffix>".
		if strings.HasSuffix(element.ResourceID, ":id/"+value) {
			return true
		}
		return false
	case "text":
		return element.Text == value
	case "desc":
		return element.Description == value
	default:
		return false
	}
}

func elementFromStart(start xml.StartElement) (*Element, error) {
	element := &Element{}
	var boundsText string
	for _, attribute := range start.Attr {
		switch attribute.Name.Local {
		case "resource-id":
			element.ResourceID = attribute.Value
		case "text":
			element.Text = attribute.Value
		case "content-desc":
			element.Description = attribute.Value
		case "class":
			element.Class = attribute.Value
		case "package":
			element.Package = attribute.Value
		case "clickable":
			element.Clickable = attribute.Value == "true"
		case "enabled":
			element.Enabled = attribute.Value == "true"
		case "bounds":
			boundsText = attribute.Value
		}
	}
	if boundsText != "" {
		bounds, err := parseBounds(boundsText)
		if err != nil {
			return nil, fmt.Errorf("bounds %q: %w", boundsText, err)
		}
		element.Bounds = bounds
	}
	return element, nil
}

var boundsPattern = regexp.MustCompile(`^\[(-?\d+),(-?\d+)\]\[(-?\d+),(-?\d+)\]$`)

func parseBounds(text string) (Bounds, error) {
	match := boundsPattern.FindStringSubmatch(text)
	if match == nil {
		return Bounds{}, fmt.Errorf("not in [L,T][R,B] form")
	}
	coordinates := make([]int, 4)
	for index := range 4 {
		value, err := strconv.Atoi(match[index+1])
		if err != nil {
			return Bounds{}, err
		}
		coordinates[index] = value
	}
	return Bounds{Left: coordinates[0], Top: coordinates[1], Right: coordinates[2], Bottom: coordinates[3]}, nil
}
