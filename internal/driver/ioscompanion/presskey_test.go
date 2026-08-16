package ioscompanion

import (
	"context"
	"testing"

	"github.com/priyanshujain/sanderling/internal/driver/ioscompanion/transport"
)

// keyRecordingCompanion keeps the HID events a press produced, so the assertion
// is over what reached the transport rather than over the lookup that built it.
type keyRecordingCompanion struct {
	fakeCompanion
	events []transport.HIDEvent
}

func (c *keyRecordingCompanion) SendHID(
	_ context.Context,
	events ...transport.HIDEvent,
) error {
	c.events = append(c.events, events...)
	return nil
}

// docs/manual/spec-language.md documents escape, and the simulator's HID stream
// carries it: usage 41 is the keyboard escape. Without it a spec clause over
// escape reports unsupported on iOS while the same clause runs on web.
func TestPressKeyEscapeReachesTheHIDStream(t *testing.T) {
	companion := &keyRecordingCompanion{}
	d := newTestDriver(companion)

	if err := d.PressKey(context.Background(), "escape"); err != nil {
		t.Fatalf("PressKey escape: %v", err)
	}

	// 41 is the USB HID keyboard escape usage, stated here rather than read
	// from the production table so a wrong table entry cannot agree with itself.
	want := []transport.HIDEvent{transport.KeyDown(41), transport.KeyUp(41)}
	if len(companion.events) != len(want) {
		t.Fatalf("sent %v, want %v", companion.events, want)
	}
	for index, event := range companion.events {
		if event != want[index] {
			t.Fatalf("sent %v, want %v", companion.events, want)
		}
	}
}
