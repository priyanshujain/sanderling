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
		"Screenshot":  true,
		"Hierarchy":   true,
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

func TestMessageFields(t *testing.T) {
	lr := &LaunchRequest{BundleId: "com.example", ClearState: true}
	if lr.GetBundleId() != "com.example" || !lr.GetClearState() {
		t.Fatalf("LaunchRequest field round-trip failed: %+v", lr)
	}

	tap := &Point{X: 10, Y: 20}
	if tap.GetX() != 10 || tap.GetY() != 20 {
		t.Fatalf("Point field round-trip failed: %+v", tap)
	}

	img := &Image{Png: []byte{1, 2, 3}, Width: 100, Height: 200}
	if len(img.GetPng()) != 3 || img.GetWidth() != 100 || img.GetHeight() != 200 {
		t.Fatalf("Image field round-trip failed: %+v", img)
	}

	hs := &HealthStatus{Ready: true, Version: "0.0.1", Platform: "android"}
	if !hs.GetReady() || hs.GetVersion() != "0.0.1" || hs.GetPlatform() != "android" {
		t.Fatalf("HealthStatus field round-trip failed: %+v", hs)
	}
}
