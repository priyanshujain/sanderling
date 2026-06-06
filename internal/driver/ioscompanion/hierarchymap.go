package ioscompanion

import (
	"encoding/json"
	"math"
	"strconv"
)

// rawFrame is the simulator companion frame, in points, with float coordinates.
type rawFrame struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// rawElement is one entry in the flat describe-all dump returned by the
// simulator companion. Only the fields the mapper consumes are declared;
// unknown fields are ignored.
type rawElement struct {
	Frame      rawFrame `json:"frame"`
	AXUniqueID *string  `json:"AXUniqueId"`
	AXLabel    *string  `json:"AXLabel"`
	AXValue    *string  `json:"AXValue"`
	Type       string   `json:"type"`
	Enabled    bool     `json:"enabled"`
}

// treeNode mirrors the TreeNode JSON the hierarchy package parses. Bool fields
// are pointers so absent ones marshal to null and stay absent for the consumer.
type treeNode struct {
	Attributes map[string]string `json:"attributes"`
	Children   []treeNode        `json:"children,omitempty"`
	Clickable  *bool             `json:"clickable,omitempty"`
	Enabled    *bool             `json:"enabled,omitempty"`
	Editable   *bool             `json:"editable,omitempty"`
}

// MapHierarchy converts a flat describe-all dump from the simulator companion
// into the TreeNode JSON consumed by hierarchy.Parse. The result is a single
// synthesized root covering the whole screen with every dump element as a
// direct child. Coordinates are points throughout; nothing is scaled.
//
// The function is total: malformed input yields an empty-but-valid root rather
// than an error, and individual malformed elements are skipped.
func MapHierarchy(dump []byte, screenWidth, screenHeight int) ([]byte, error) {
	root := treeNode{
		Attributes: map[string]string{
			"bounds": boundsString(0, 0, screenWidth, screenHeight),
			"class":  "Window",
		},
	}

	// Decode element-by-element so a single malformed entry (a bad frame, an
	// out-of-range number) is skipped rather than discarding the whole dump.
	var rawElements []json.RawMessage
	if len(dump) > 0 {
		_ = json.Unmarshal(dump, &rawElements)
	}
	for _, raw := range rawElements {
		var element rawElement
		if err := json.Unmarshal(raw, &element); err != nil {
			continue
		}
		if child, ok := mapElement(&element); ok {
			root.Children = append(root.Children, child)
		}
	}

	return json.Marshal(root)
}

func mapElement(element *rawElement) (treeNode, bool) {
	if element.Type == "" {
		return treeNode{}, false
	}

	frame := element.Frame
	if !finite(frame.X) || !finite(frame.Y) || !finite(frame.Width) || !finite(frame.Height) {
		return treeNode{}, false
	}

	left := roundCoord(frame.X)
	top := roundCoord(frame.Y)
	attributes := map[string]string{
		"bounds": boundsString(left, top, left+roundCoord(frame.Width), top+roundCoord(frame.Height)),
		"class":  element.Type,
	}

	if id := stringValue(element.AXUniqueID); id != "" {
		attributes["identifier"] = id
	}

	value := stringValue(element.AXValue)
	label := stringValue(element.AXLabel)
	editable := isEditable(element.Type)

	if value != "" {
		attributes["text"] = value
		if label != "" {
			attributes["accessibilityText"] = label
		}
	} else if editable && label != "" {
		// An empty text field surfaces its placeholder as the AXLabel.
		attributes["hintText"] = label
	} else if label != "" {
		attributes["accessibilityText"] = label
	}

	enabled := element.Enabled
	node := treeNode{Attributes: attributes, Enabled: &enabled}

	if editable {
		yes := true
		node.Editable = &yes
	}
	if element.Type == "Button" {
		yes := true
		node.Clickable = &yes
	}

	return node, true
}

func isEditable(elementType string) bool {
	return elementType == "TextArea" || elementType == "TextField"
}

func stringValue(pointer *string) string {
	if pointer == nil {
		return ""
	}
	return *pointer
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func roundCoord(value float64) int {
	return int(math.Round(value))
}

func boundsString(left, top, right, bottom int) string {
	return "[" + strconv.Itoa(left) + "," + strconv.Itoa(top) + "][" +
		strconv.Itoa(right) + "," + strconv.Itoa(bottom) + "]"
}
