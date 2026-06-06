package transport

import (
	pb "github.com/priyanshujain/sanderling/internal/driver/ioscompanion/companionpb"
)

// HIDEvent is one input event in a HID stream. It wraps the generated event so
// callers never import the companionpb package. Build one with a builder below.
type HIDEvent struct {
	event *pb.HIDEvent
}

func touchPress(x, y float64, direction pb.HIDEvent_HIDDirection) HIDEvent {
	return HIDEvent{event: &pb.HIDEvent{Event: &pb.HIDEvent_Press{Press: &pb.HIDEvent_HIDPress{
		Direction: direction,
		Action: &pb.HIDEvent_HIDPressAction{Action: &pb.HIDEvent_HIDPressAction_Touch{
			Touch: &pb.HIDEvent_HIDTouch{Point: &pb.Point{X: x, Y: y}},
		}},
	}}}}
}

func keyPress(usage uint32, direction pb.HIDEvent_HIDDirection) HIDEvent {
	return HIDEvent{event: &pb.HIDEvent{Event: &pb.HIDEvent_Press{Press: &pb.HIDEvent_HIDPress{
		Direction: direction,
		Action: &pb.HIDEvent_HIDPressAction{Action: &pb.HIDEvent_HIDPressAction_Key{
			Key: &pb.HIDEvent_HIDKey{Keycode: uint64(usage)},
		}},
	}}}}
}

// TouchDown presses a finger down at screen point (x, y).
func TouchDown(x, y float64) HIDEvent { return touchPress(x, y, pb.HIDEvent_DOWN) }

// TouchUp lifts the finger at screen point (x, y).
func TouchUp(x, y float64) HIDEvent { return touchPress(x, y, pb.HIDEvent_UP) }

// KeyDown presses the key with the given USB HID usage identifier.
func KeyDown(usage uint32) HIDEvent { return keyPress(usage, pb.HIDEvent_DOWN) }

// KeyUp releases the key with the given USB HID usage identifier.
func KeyUp(usage uint32) HIDEvent { return keyPress(usage, pb.HIDEvent_UP) }

// Delay pauses the HID stream. The companion measures delays in seconds, so the
// caller's milliseconds are converted here.
func Delay(milliseconds float64) HIDEvent {
	return HIDEvent{event: &pb.HIDEvent{Event: &pb.HIDEvent_Delay{
		Delay: &pb.HIDEvent_HIDDelay{Duration: milliseconds / 1000.0},
	}}}
}

// SwipeEvent drags from (fromX, fromY) to (toX, toY) over durationSeconds. The
// swipe message carries start, end, and a duration in seconds.
func SwipeEvent(fromX, fromY, toX, toY float64, durationSeconds float64) HIDEvent {
	return HIDEvent{event: &pb.HIDEvent{Event: &pb.HIDEvent_Swipe{Swipe: &pb.HIDEvent_HIDSwipe{
		Start:    &pb.Point{X: fromX, Y: fromY},
		End:      &pb.Point{X: toX, Y: toY},
		Duration: durationSeconds,
	}}}}
}
