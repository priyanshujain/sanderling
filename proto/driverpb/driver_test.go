package driverpb

import (
	"testing"

	"google.golang.org/grpc"
)

func TestDriverServiceDescriptor(t *testing.T) {
	var sd grpc.ServiceDesc = Driver_ServiceDesc
	if sd.ServiceName != "sanderling.driver.v1.Driver" {
		t.Fatalf("unexpected service name: %q", sd.ServiceName)
	}

	want := map[string]bool{
		"Launch":      true,
		"Terminate":   true,
		"Tap":         true,
		"TapSelector": true,
		"InputText":   true,
		"Swipe":       true,
		"PressKey":    true,
		"LongPress":   true,
		"Screenshot":  true,
		"Hierarchy":   true,
		"Snapshot":    true,
		"RecentLogs":  true,
		"WaitForIdle": true,
		"Health":      true,
		"Metrics":     true,
	}
	got := map[string]bool{}
	for _, m := range sd.Methods {
		got[m.MethodName] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("missing method %q in service", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("unexpected method %q in service", name)
		}
	}
}
