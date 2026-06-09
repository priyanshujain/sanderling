package transport

import (
	"testing"

	pb "github.com/priyanshujain/sanderling/internal/driver/ioscompanion/companionpb"
)

func TestBuildersSetKindAndFields(t *testing.T) {
	cases := []struct {
		name  string
		event HIDEvent
		want  HIDEvent
	}{
		{"touch down", TouchDown(12, 34), HIDEvent{Kind: HIDKindTouchDown, X: 12, Y: 34}},
		{"touch up", TouchUp(12, 34), HIDEvent{Kind: HIDKindTouchUp, X: 12, Y: 34}},
		{"key down", KeyDown(225), HIDEvent{Kind: HIDKindKeyDown, Usage: 225}},
		{"key up", KeyUp(225), HIDEvent{Kind: HIDKindKeyUp, Usage: 225}},
		{"delay", Delay(250), HIDEvent{Kind: HIDKindDelay, Milliseconds: 250}},
		{"swipe", SwipeEvent(1, 2, 3, 4, 0.5), HIDEvent{
			Kind: HIDKindSwipe, FromX: 1, FromY: 2, ToX: 3, ToY: 4, Seconds: 0.5,
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.event != c.want {
				t.Errorf("event = %+v, want %+v", c.event, c.want)
			}
		})
	}
}

func TestTouchEventsToProto(t *testing.T) {
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
			message, err := hidEventToProto(c.event)
			if err != nil {
				t.Fatalf("hidEventToProto: %v", err)
			}
			press := message.GetPress()
			if press == nil {
				t.Fatalf("expected press event, got %#v", message.GetEvent())
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

func TestKeyEventsToProto(t *testing.T) {
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
			message, err := hidEventToProto(c.event)
			if err != nil {
				t.Fatalf("hidEventToProto: %v", err)
			}
			press := message.GetPress()
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

func TestDelayToProtoConvertsToSeconds(t *testing.T) {
	message, err := hidEventToProto(Delay(250))
	if err != nil {
		t.Fatalf("hidEventToProto: %v", err)
	}
	delay := message.GetDelay()
	if delay == nil {
		t.Fatalf("expected delay event")
	}
	if delay.GetDuration() != 0.25 {
		t.Errorf("duration = %v seconds, want 0.25", delay.GetDuration())
	}
}

func TestSwipeEventToProto(t *testing.T) {
	message, err := hidEventToProto(SwipeEvent(1, 2, 3, 4, 0.5))
	if err != nil {
		t.Fatalf("hidEventToProto: %v", err)
	}
	swipe := message.GetSwipe()
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

func TestUnknownKindToProtoErrors(t *testing.T) {
	if _, err := hidEventToProto(HIDEvent{Kind: HIDEventKind(99)}); err == nil {
		t.Fatal("expected error for unknown event kind")
	}
}
