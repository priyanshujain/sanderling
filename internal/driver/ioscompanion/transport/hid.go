package transport

// HIDEventKind discriminates the neutral HID event variants.
type HIDEventKind int

const (
	HIDKindTouchDown HIDEventKind = iota
	HIDKindTouchUp
	HIDKindKeyDown
	HIDKindKeyUp
	HIDKindDelay
	HIDKindSwipe
)

// HIDEvent is one input event in a HID stream, expressed in transport-neutral
// terms so each companion transport encodes it for its own wire format. Build
// one with a builder below; only the fields for the event's kind are set.
type HIDEvent struct {
	Kind HIDEventKind

	// X, Y is the touch point for TouchDown and TouchUp.
	X, Y float64

	// Usage is the USB HID usage identifier for KeyDown and KeyUp.
	Usage uint32

	// Milliseconds is the pause length for Delay.
	Milliseconds float64

	// FromX through Seconds describe a Swipe.
	FromX, FromY float64
	ToX, ToY     float64
	Seconds      float64
}

// TouchDown presses a finger down at screen point (x, y).
func TouchDown(x, y float64) HIDEvent { return HIDEvent{Kind: HIDKindTouchDown, X: x, Y: y} }

// TouchUp lifts the finger at screen point (x, y).
func TouchUp(x, y float64) HIDEvent { return HIDEvent{Kind: HIDKindTouchUp, X: x, Y: y} }

// KeyDown presses the key with the given USB HID usage identifier.
func KeyDown(usage uint32) HIDEvent { return HIDEvent{Kind: HIDKindKeyDown, Usage: usage} }

// KeyUp releases the key with the given USB HID usage identifier.
func KeyUp(usage uint32) HIDEvent { return HIDEvent{Kind: HIDKindKeyUp, Usage: usage} }

// Delay pauses the HID stream for the given duration.
func Delay(milliseconds float64) HIDEvent {
	return HIDEvent{Kind: HIDKindDelay, Milliseconds: milliseconds}
}

// SwipeEvent drags from (fromX, fromY) to (toX, toY) over durationSeconds.
func SwipeEvent(fromX, fromY, toX, toY float64, durationSeconds float64) HIDEvent {
	return HIDEvent{
		Kind:    HIDKindSwipe,
		FromX:   fromX,
		FromY:   fromY,
		ToX:     toX,
		ToY:     toY,
		Seconds: durationSeconds,
	}
}
