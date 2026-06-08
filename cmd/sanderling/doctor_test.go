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
	for _, name := range []string{"adb on PATH", "xcrun on PATH (ios simulator)", "headless chromium can launch"} {
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
	for _, want := range []string{"devicectl", "iproxy", "connected and paired", "signing credentials"} {
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

func TestCheckDeviceSigning_EnvAndKeyFile(t *testing.T) {
	// The signing check defers to the driver's credential verification, which
	// reads the same environment a real build would.
	for _, key := range []string{"SANDERLING_IOS_TEAM", "DEVELOPMENT_TEAM", "ASC_API_KEY_PATH", "ASC_API_KEY_ID", "ASC_API_ISSUER_ID"} {
		t.Setenv(key, "")
	}
	if err := checkDeviceSigning(context.Background()); err == nil {
		t.Fatal("missing signing env must fail")
	}

	keyPath := filepath.Join(t.TempDir(), "AuthKey.p8")
	if err := os.WriteFile(keyPath, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SANDERLING_IOS_TEAM", "TEAM1")
	t.Setenv("ASC_API_KEY_ID", "KID")
	t.Setenv("ASC_API_ISSUER_ID", "ISS")
	t.Setenv("ASC_API_KEY_PATH", keyPath)
	if err := checkDeviceSigning(context.Background()); err != nil {
		t.Fatalf("complete signing env with a present key must pass: %v", err)
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
