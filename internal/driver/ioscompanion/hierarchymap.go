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

// emptyFieldValueSentinel is what the accessibility bridge reports as the
// AXValue of an empty editable field. It is bridge state, not app content, so
// it maps to an empty value rather than surfacing as literal field text.
const emptyFieldValueSentinel = "Invalid"

// dumpIsCollapsed reports whether a flat describe-all dump carries no real UI
// content: it is empty or holds only the application shell. The accessibility
// bridge briefly returns this state during cold start and screen transitions
// before the real tree reappears.
func dumpIsCollapsed(dump []byte) bool {
	elements := decodeDump(dump)
	for _, element := range elements {
		if element.Type != "" && element.Type != "Application" {
			return false
		}
	}
	return true
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
	Depth      int      `json:"depth"`
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
	elements := make([]rawElement, 0, len(rawElements))
	for _, raw := range rawElements {
		var element rawElement
		if err := json.Unmarshal(raw, &element); err != nil {
			continue
		}
		elements = append(elements, element)
	}

	scrollable := scrollableElements(elements, screenWidth, screenHeight)
	for index := range elements {
		if child, ok := mapElement(&elements[index], scrollable[index]); ok {
			root.Children = append(root.Children, child)
		}
	}

	return json.Marshal(root)
}

// frameTolerance absorbs the sub-point rounding in companion frames, so a child
// that sits flush against its container's edge does not read as escaping it.
const frameTolerance = 0.5

// scrollableElements reports, per element, whether it is a container that clips
// content reaching past its own frame. That is the same fact Android reads off
// uiautomator's scrollable attribute and the web driver derives from overflow:
// the container can actually scroll, because there is content it is not showing.
//
// Three conditions together, because the snapshot has no clipping flag. The
// element must sit strictly inside its parent on at least one edge, which
// separates a real container from the stack of full-screen wrappers that
// inherit its overflow; some element in its subtree must lie outside it; and it
// must be on the screen, since a dismissed keyboard is reported below the screen
// and clips a much taller child without any gesture being able to reach it.
// A dump without depth (the legacy accessibility bridge) makes every element a
// root, and roots are never marked, so that path reports no scroll rather than
// a guessed one.
func scrollableElements(elements []rawElement, screenWidth, screenHeight int) []bool {
	screen := rawFrame{Width: float64(screenWidth), Height: float64(screenHeight)}
	scrollable := make([]bool, len(elements))
	var ancestors []int
	for index, element := range elements {
		for len(ancestors) > 0 && elements[ancestors[len(ancestors)-1]].Depth >= element.Depth {
			ancestors = ancestors[:len(ancestors)-1]
		}
		if len(ancestors) > 0 && hasArea(element.Frame) &&
			overlaps(element.Frame, screen) &&
			sitsInside(element.Frame, elements[ancestors[len(ancestors)-1]].Frame) &&
			subtreeEscapes(elements, index) {
			scrollable[index] = true
		}
		ancestors = append(ancestors, index)
	}
	return scrollable
}

// overlaps reports whether two frames share any area.
func overlaps(frame, other rawFrame) bool {
	return frame.X < other.X+other.Width && other.X < frame.X+frame.Width &&
		frame.Y < other.Y+other.Height && other.Y < frame.Y+frame.Height
}

// sitsInside reports whether frame is strictly smaller than container on at
// least one edge.
func sitsInside(frame, container rawFrame) bool {
	return frame.X > container.X+frameTolerance ||
		frame.Y > container.Y+frameTolerance ||
		frame.X+frame.Width < container.X+container.Width-frameTolerance ||
		frame.Y+frame.Height < container.Y+container.Height-frameTolerance
}

// subtreeEscapes reports whether any descendant of the element at index is
// positioned outside its frame. Descendants are the run that follows it while
// the depth stays greater, which is the pre-order walk the companion emits.
func subtreeEscapes(elements []rawElement, index int) bool {
	frame := elements[index].Frame
	for next := index + 1; next < len(elements) && elements[next].Depth > elements[index].Depth; next++ {
		child := elements[next].Frame
		if !hasArea(child) {
			continue
		}
		if child.X < frame.X-frameTolerance ||
			child.Y < frame.Y-frameTolerance ||
			child.X+child.Width > frame.X+frame.Width+frameTolerance ||
			child.Y+child.Height > frame.Y+frame.Height+frameTolerance {
			return true
		}
	}
	return false
}

func hasArea(frame rawFrame) bool {
	return finite(frame.X) && finite(frame.Y) && finite(frame.Width) &&
		finite(frame.Height) && frame.Width > 0 && frame.Height > 0
}

func mapElement(element *rawElement, scrollable bool) (treeNode, bool) {
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

	if scrollable {
		attributes["scrollable"] = "true"
	}

	if id := stringValue(element.AXUniqueID); id != "" {
		attributes["identifier"] = id
	}

	value := stringValue(element.AXValue)
	if value == emptyFieldValueSentinel {
		// An empty editable field reads as this sentinel through the bridge;
		// it is not app content, so treat the field as empty.
		value = ""
	}
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
		if labelIsDisplayedText(element.Type) {
			attributes["text"] = label
		}
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

// labelIsDisplayedText reports whether an element type's AXLabel is the string
// drawn on screen rather than an accessibility annotation about it. Only
// StaticText qualifies: a text element's label IS what it renders, so it
// belongs in `text`, matching a TextView on Android and a text node on web.
//
// Buttons and images are deliberately excluded even though a titled button's
// label is also its visible title. The snapshot cannot tell that button apart
// from an icon-only one whose label exists purely for VoiceOver, nor from a
// container whose label is a comma-joined reading of its children ("CH,
// Checking, $0.00, 0 transactions"). Inventing `text` for those would put
// strings in `text` that no user can read, and would diverge from Android,
// which leaves `text` empty and reports a contentDescription as `description`.
func labelIsDisplayedText(elementType string) bool {
	return elementType == "StaticText"
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
