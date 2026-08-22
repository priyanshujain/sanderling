package testrun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

// Every adb call in an android run resolves through $ANDROID_HOME and the
// standard SDK locations, so a preflight that only looks at PATH turns away a
// host the run itself would drive.
func TestPreflight_AndroidAcceptsAdbUnderAndroidHome(t *testing.T) {
	sdk := t.TempDir()
	writeExecutable(t, filepath.Join(sdk, "platform-tools", "adb"))
	pathDirectory := t.TempDir()
	writeExecutable(t, filepath.Join(pathDirectory, "java"))
	t.Setenv("PATH", pathDirectory)
	t.Setenv("ANDROID_HOME", sdk)
	t.Setenv("ANDROID_SDK_ROOT", "")

	if err := Preflight(context.Background(), "android"); err != nil {
		t.Fatalf("Preflight with adb under $ANDROID_HOME: %v", err)
	}
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, nil, 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestPreflight_iOSNeedsXcrun(t *testing.T) {
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

func TestPreflight_iOSDoesNotRequireJava(t *testing.T) {
	checked := []string{}
	check := func(name string) error {
		checked = append(checked, name)
		if name == "java" {
			return errors.New("java not found")
		}
		return nil
	}
	if err := runPreflight(context.Background(), "ios", check); err != nil {
		t.Fatalf("ios preflight should pass without java, got %v", err)
	}
	for _, name := range checked {
		if name == "java" {
			t.Errorf("ios preflight must not check java; simulator runs need no JVM")
		}
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

// Neither iOS path runs a JVM, so preflight must not turn a host away for
// want of java on the platform that never asks for it.
func TestPreflight_IosDoesNotRequireJava(t *testing.T) {
	check := func(name string) error {
		if name == "java" {
			return errors.New("java not found")
		}
		return nil
	}
	if err := runPreflight(context.Background(), "ios", check); err != nil {
		t.Errorf("ios preflight failed on a host with no java: %v", err)
	}
}

func TestPreflight_UnknownPlatform(t *testing.T) {
	check := func(string) error { return nil }
	if err := runPreflight(context.Background(), "fuchsia", check); err == nil {
		t.Error("expected error for unknown platform")
	}
}
