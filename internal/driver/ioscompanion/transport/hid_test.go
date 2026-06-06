package transport

import (
	"testing"

	pb "github.com/priyanshujain/sanderling/internal/driver/ioscompanion/companionpb"
)

func TestTouchBuilders(t *testing.T) {
	cases := []struct {
		name      string
		event     HIDEvent
		direction pb.HIDEvent_HIDDirection
	}{
		{"down", TouchDown(12, 34), pb.HIDEvent_DOWN},
		{"up", TouchUp(12, 34), pb.HIDEvent_UP},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			press := c.event.event.GetPress()
			if press == nil {
				t.Fatalf("expected press event, got %#v", c.event.event.GetEvent())
			}
			if press.GetDirection() != c.direction {
				t.Errorf("direction = %v, want %v", press.GetDirection(), c.direction)
			}
			touch := press.GetAction().GetTouch()
			if touch == nil {
				t.Fatalf("expected touch action")
			}
			if got := touch.GetPoint(); got.GetX() != 12 || got.GetY() != 34 {
				t.Errorf("point = (%v, %v), want (12, 34)", got.GetX(), got.GetY())
			}
		})
	}
}

func TestKeyBuilders(t *testing.T) {
	cases := []struct {
		name      string
		event     HIDEvent
		direction pb.HIDEvent_HIDDirection
	}{
		{"down", KeyDown(225), pb.HIDEvent_DOWN},
		{"up", KeyUp(225), pb.HIDEvent_UP},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			press := c.event.event.GetPress()
			if press == nil {
				t.Fatalf("expected press event")
			}
			if press.GetDirection() != c.direction {
				t.Errorf("direction = %v, want %v", press.GetDirection(), c.direction)
			}
			key := press.GetAction().GetKey()
			if key == nil {
				t.Fatalf("expected key action")
			}
			if key.GetKeycode() != 225 {
				t.Errorf("keycode = %d, want 225", key.GetKeycode())
			}
		})
	}
}

func TestDelayConvertsToSeconds(t *testing.T) {
	delay := Delay(250).event.GetDelay()
	if delay == nil {
		t.Fatalf("expected delay event")
	}
	if delay.GetDuration() != 0.25 {
		t.Errorf("duration = %v seconds, want 0.25", delay.GetDuration())
	}
}

func TestSwipeEvent(t *testing.T) {
	swipe := SwipeEvent(1, 2, 3, 4, 0.5).event.GetSwipe()
	if swipe == nil {
		t.Fatalf("expected swipe event")
	}
	if s := swipe.GetStart(); s.GetX() != 1 || s.GetY() != 2 {
		t.Errorf("start = (%v, %v), want (1, 2)", s.GetX(), s.GetY())
	}
	if e := swipe.GetEnd(); e.GetX() != 3 || e.GetY() != 4 {
		t.Errorf("end = (%v, %v), want (3, 4)", e.GetX(), e.GetY())
	}
	if swipe.GetDuration() != 0.5 {
		t.Errorf("duration = %v, want 0.5", swipe.GetDuration())
	}
}
