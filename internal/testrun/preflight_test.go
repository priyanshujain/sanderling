package testrun

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPreflight_WebSkips(t *testing.T) {
	called := 0
	check := func(name string) error {
		called++
		return nil
	}
	if err := runPreflight(context.Background(), "web", check); err != nil {
		t.Fatalf("web preflight should be no-op, got %v", err)
	}
	if called != 0 {
		t.Errorf("web preflight ran %d binary checks; expected 0", called)
	}
}

func TestPreflight_AndroidNeedsAdbAndJava(t *testing.T) {
	cases := []struct {
		name      string
		missing   string
		wantInErr string
	}{
		{name: "missing adb", missing: "adb", wantInErr: "adb"},
		{name: "missing java", missing: "java", wantInErr: "java"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			check := func(name string) error {
				if name == testCase.missing {
					return errors.New(name + " not found")
				}
				return nil
			}
			err := runPreflight(context.Background(), "android", check)
			if err == nil || !strings.Contains(err.Error(), testCase.wantInErr) {
				t.Fatalf("expected error mentioning %q, got %v", testCase.wantInErr, err)
			}
			if !strings.Contains(err.Error(), "sanderling doctor --platform=android") {
				t.Errorf("error missing doctor hint: %v", err)
			}
		})
	}
}

func TestPreflight_iOSNeedsXcrunAndJava(t *testing.T) {
	check := func(name string) error {
		if name == "xcrun" {
			return errors.New("xcrun not found")
		}
		return nil
	}
	err := runPreflight(context.Background(), "ios", check)
	if err == nil || !strings.Contains(err.Error(), "xcrun") {
		t.Fatalf("expected xcrun error, got %v", err)
	}
	if !strings.Contains(err.Error(), "sanderling doctor --platform=ios") {
		t.Errorf("error missing doctor hint: %v", err)
	}
}

func TestPreflight_AllOK(t *testing.T) {
	check := func(name string) error { return nil }
	for _, platform := range []string{"web", "android", "ios"} {
		if err := runPreflight(context.Background(), platform, check); err != nil {
			t.Errorf("%s: unexpected error %v", platform, err)
		}
	}
}

func TestPreflight_UnknownPlatform(t *testing.T) {
	check := func(string) error { return nil }
	if err := runPreflight(context.Background(), "fuchsia", check); err == nil {
		t.Error("expected error for unknown platform")
	}
}
