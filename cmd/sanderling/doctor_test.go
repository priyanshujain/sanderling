package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/priyanshujain/sanderling/internal/ios"
)

func TestRunDoctorChecks_AllPass(t *testing.T) {
	var stdout bytes.Buffer
	checks := []doctorCheck{
		{Name: "always ok", Run: func(context.Context) error { return nil }},
		{Name: "also ok", Run: func(context.Context) error { return nil }},
	}
	if err := runDoctorChecks(context.Background(), checks, &stdout); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	output := stdout.String()
	if !strings.Contains(output, "OK    always ok") || !strings.Contains(output, "OK    also ok") {
		t.Errorf("expected OK lines, got: %s", output)
	}
}

func TestRunDoctorChecks_ReportsFailures(t *testing.T) {
	var stdout bytes.Buffer
	checks := []doctorCheck{
		{Name: "ok", Run: func(context.Context) error { return nil }},
		{Name: "broken", Run: func(context.Context) error { return errors.New("boom") }},
	}
	err := runDoctorChecks(context.Background(), checks, &stdout)
	if err == nil || !strings.Contains(err.Error(), "1 check(s) failed") {
		t.Fatalf("expected failure summary, got %v", err)
	}
	output := stdout.String()
	if !strings.Contains(output, "FAIL  broken") {
		t.Errorf("expected FAIL line, got: %s", output)
	}
}

func TestParseJavaMajor_AcceptsModernFormat(t *testing.T) {
	cases := []struct {
		input string
		major int
	}{
		{`openjdk version "17.0.10"` + "\n", 17},
		{`openjdk version "21" 2023-09-19` + "\n", 21},
		{`java version "25.0.2" 2026-01-20` + "\n", 25},
		{`openjdk version "1.8.0_402"` + "\n", 8},
	}
	for _, testCase := range cases {
		got, err := parseJavaMajor(testCase.input)
		if err != nil {
			t.Errorf("parseJavaMajor(%q): unexpected error %v", testCase.input, err)
			continue
		}
		if got != testCase.major {
			t.Errorf("parseJavaMajor(%q): got %d, want %d", testCase.input, got, testCase.major)
		}
	}
}

func TestParseJavaMajor_RejectsUnrecognized(t *testing.T) {
	_, err := parseJavaMajor("not java output\n")
	if err == nil {
		t.Errorf("expected error for unrecognized output")
	}
}

func TestCheckExecutableOnPath_FindsRealCommand(t *testing.T) {
	check := checkExecutableOnPath("ls")
	if err := check(context.Background()); err != nil {
		t.Errorf("ls should be on PATH on macOS/linux, got %v", err)
	}
}

func TestCheckExecutableOnPath_MissingCommand(t *testing.T) {
	check := checkExecutableOnPath("definitely-not-a-real-command-xyz-123")
	if err := check(context.Background()); err == nil {
		t.Errorf("expected error for missing command")
	}
}

func TestDoctorChecksFor_Web_OmitsJava(t *testing.T) {
	for _, c := range doctorChecksFor("web") {
		if strings.Contains(c.Name, "java") || strings.Contains(c.Name, "sidecar") || strings.Contains(c.Name, "adb") {
			t.Errorf("web checks should not include %q", c.Name)
		}
	}
	if len(doctorChecksFor("web")) == 0 {
		t.Error("web checks empty")
	}
}

func TestDoctorChecksFor_Android_IncludesADB(t *testing.T) {
	checks := doctorChecksFor("android")
	found := false
	for _, c := range checks {
		if strings.Contains(c.Name, "adb") {
			found = true
		}
	}
	if !found {
		t.Errorf("android checks missing adb: %+v", checks)
	}
}

// The SDK tools a run invokes are resolved through $ANDROID_HOME and the
// standard install locations, never PATH alone, so a doctor that turns away a
// host on a PATH lookup condemns a setup every run on it would drive fine.
func TestAndroidChecks_AcceptSDKToolsThatAreNotOnPath(t *testing.T) {
	sdk := t.TempDir()
	for _, tool := range []string{"platform-tools/adb", "emulator/emulator"} {
		path := filepath.Join(sdk, filepath.FromSlash(tool))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, nil, 0o755); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("ANDROID_HOME", sdk)
	t.Setenv("ANDROID_SDK_ROOT", "")

	var sdkChecks []doctorCheck
	for _, check := range doctorChecksFor("android") {
		if strings.Contains(check.Name, "adb") || strings.Contains(check.Name, "emulator") {
			sdkChecks = append(sdkChecks, check)
		}
	}
	if len(sdkChecks) != 2 {
		t.Fatalf("expected the adb and emulator checks, got %d", len(sdkChecks))
	}

	var stdout bytes.Buffer
	if err := runDoctorChecks(context.Background(), sdkChecks, &stdout); err != nil {
		t.Fatalf("doctor: %v\n%s", err, stdout.String())
	}
	if strings.Contains(stdout.String(), "FAIL") {
		t.Errorf("doctor rejected an SDK it can resolve:\n%s", stdout.String())
	}
}

func TestDoctorChecksFor_iOS_IncludesXcrun(t *testing.T) {
	checks := doctorChecksFor("ios")
	found := false
	for _, c := range checks {
		if strings.Contains(c.Name, "xcrun") {
			found = true
		}
	}
	if !found {
		t.Errorf("ios checks missing xcrun: %+v", checks)
	}
}

func TestDoctorChecksFor_All_IsUnion(t *testing.T) {
	all := doctorChecksFor("all")
	names := map[string]int{}
	for _, c := range all {
		names[c.Name]++
	}
	for _, name := range []string{"adb on PATH or under the Android SDK", "xcrun on PATH (ios simulator)", "headless chromium can launch"} {
		if names[name] != 1 {
			t.Errorf("expected %q in 'all' exactly once, got %d", name, names[name])
		}
	}
}

func TestDoctorChecksFor_iOSSimulator_OmitsJava(t *testing.T) {
	for _, c := range doctorChecksFor("ios") {
		if strings.Contains(c.Name, "java") || strings.Contains(c.Name, "sidecar") {
			t.Errorf("ios simulator checks must omit %q; simulator runs need no JVM", c.Name)
		}
	}
}

func TestDoctorChecksFor_iOSDevice_CoversDevicePrereqs(t *testing.T) {
	checks := doctorChecksFor("ios-device")
	for _, c := range checks {
		if strings.Contains(c.Name, "java") || strings.Contains(c.Name, "sidecar") {
			t.Errorf("device checks must not include the retired %q", c.Name)
		}
	}
	for _, want := range []string{"devicectl", "usbmuxd", "connected and paired", "signing credentials"} {
		found := false
		for _, c := range checks {
			if strings.Contains(c.Name, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("ios-device checks missing %q: %+v", want, checks)
		}
	}
}

func TestCheckDeviceConnected(t *testing.T) {
	original := doctorConnectedDevices
	t.Cleanup(func() { doctorConnectedDevices = original })

	doctorConnectedDevices = func(context.Context) ([]ios.Device, error) {
		return []ios.Device{{Name: "iPhone"}}, nil
	}
	if err := checkDeviceConnected(context.Background()); err != nil {
		t.Fatalf("a connected device must pass: %v", err)
	}

	doctorConnectedDevices = func(context.Context) ([]ios.Device, error) { return nil, nil }
	if err := checkDeviceConnected(context.Background()); err == nil {
		t.Fatal("no device must fail")
	}
}

func TestCheckDeviceSigning_SurfacesSeamResult(t *testing.T) {
	// checkDeviceSigning is a passthrough to the driver's credential check; the
	// credential logic itself is covered by TestReadSigningCredentials* in the
	// ioscompanion package. Here we only confirm the wiring through the seam.
	original := doctorVerifySigning
	t.Cleanup(func() { doctorVerifySigning = original })

	doctorVerifySigning = func() error { return nil }
	if err := checkDeviceSigning(context.Background()); err != nil {
		t.Fatalf("a passing signing check must surface nil: %v", err)
	}

	doctorVerifySigning = func() error { return errors.New("missing credentials") }
	if err := checkDeviceSigning(context.Background()); err == nil {
		t.Fatal("a failing signing check must surface the error")
	}
}

func TestDoctorChecksFor_UnknownPlatform(t *testing.T) {
	if got := doctorChecksFor("fuchsia"); got != nil {
		t.Errorf("expected nil for unknown platform, got %+v", got)
	}
}

func TestParseDoctorArgs_DefaultAll(t *testing.T) {
	options, err := parseDoctorArgs(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.platform != "all" {
		t.Errorf("default platform: got %q, want all", options.platform)
	}
}

func TestParseDoctorArgs_ExplicitPlatform(t *testing.T) {
	for _, form := range [][]string{
		{"--platform", "web"},
		{"--platform=web"},
	} {
		options, err := parseDoctorArgs(form, io.Discard)
		if err != nil {
			t.Fatalf("%v: %v", form, err)
		}
		if options.platform != "web" {
			t.Errorf("%v: got platform=%q, want web", form, options.platform)
		}
	}
}

func TestParseDoctorArgs_RejectsUnknown(t *testing.T) {
	if _, err := parseDoctorArgs([]string{"--platform=fuchsia"}, io.Discard); err == nil {
		t.Error("expected error for unsupported platform")
	}
	if _, err := parseDoctorArgs([]string{"--bogus"}, io.Discard); err == nil {
		t.Error("expected error for unknown argument")
	}
}

func TestParseDoctorArgs_HelpReturnsErrHelp(t *testing.T) {
	if _, err := parseDoctorArgs([]string{"-h"}, io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("expected flag.ErrHelp for -h, got %v", err)
	}
	if _, err := parseDoctorArgs([]string{"--help"}, io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("expected flag.ErrHelp for --help, got %v", err)
	}
}
