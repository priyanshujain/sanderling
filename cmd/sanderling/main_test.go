package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

func TestParseTestArgs_Defaults(t *testing.T) {
	options, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "com.example",
		"--avd", "Pixel_5_API_33",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.spec != "s.ts" || options.bundleID != "com.example" {
		t.Errorf("unexpected options: %+v", options)
	}
	if options.platform != "android" {
		t.Errorf("platform default: got %q, want android", options.platform)
	}
	if options.duration != 5*time.Minute {
		t.Errorf("duration default: got %v, want 5m", options.duration)
	}
	if options.output != "./runs" {
		t.Errorf("output default: got %q, want ./runs", options.output)
	}
	if options.seed != 0 {
		t.Errorf("seed default: got %d, want 0", options.seed)
	}
}

func TestParseTestArgs_AllFlags(t *testing.T) {
	options, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "com.example",
		"--platform", "android",
		"--avd", "Pixel_5_API_33",
		"--duration", "10m",
		"--seed", "42",
		"--output", "./out",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.avd != "Pixel_5_API_33" || options.duration != 10*time.Minute || options.seed != 42 || options.output != "./out" {
		t.Errorf("unexpected options: %+v", options)
	}
}

func TestParseTestArgs_RequiresSpec(t *testing.T) {
	_, err := parseTestArgs([]string{"--bundle-id", "com.example", "--avd", "x"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--spec") {
		t.Fatalf("expected missing --spec error, got %v", err)
	}
}

func TestParseTestArgs_RequiresBundleID(t *testing.T) {
	_, err := parseTestArgs([]string{"--spec", "s.ts", "--avd", "x"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--bundle-id") {
		t.Fatalf("expected missing --bundle-id error, got %v", err)
	}
}

func TestParseTestArgs_AVDIsOptional(t *testing.T) {
	options, err := parseTestArgs([]string{"--spec", "s.ts", "--bundle-id", "com.example"}, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if options.avd != "" {
		t.Fatalf("avd default: got %q, want empty", options.avd)
	}
}

func TestParseTestArgs_RejectsUnknownPlatform(t *testing.T) {
	_, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "com.example",
		"--platform", "fuchsia",
		"--avd", "x",
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "unsupported platform") {
		t.Fatalf("expected unsupported-platform error, got %v", err)
	}
}

func TestParseTestArgs_AcceptsWebPlatform(t *testing.T) {
	options, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "http://localhost:3000",
		"--platform", "web",
	}, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error for web platform: %v", err)
	}
	if options.platform != "web" {
		t.Errorf("expected platform=web, got %q", options.platform)
	}
}

func TestRun_HelpPrintsUsage(t *testing.T) {
	var stdout bytes.Buffer
	if err := run([]string{"sanderling"}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "sanderling <command>") {
		t.Errorf("usage missing, got: %q", stdout.String())
	}
}

func TestRun_VersionPrintsVersion(t *testing.T) {
	prev := Version
	Version = "1.2.3-test"
	defer func() { Version = prev }()

	for _, arg := range []string{"version", "--version", "-v"} {
		var stdout bytes.Buffer
		if err := run([]string{"sanderling", arg}, &stdout, io.Discard); err != nil {
			t.Fatalf("%s: %v", arg, err)
		}
		if strings.TrimSpace(stdout.String()) != "1.2.3-test" {
			t.Errorf("%s: got %q, want 1.2.3-test", arg, stdout.String())
		}
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	err := run([]string{"sanderling", "wat"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("expected unknown-command error, got %v", err)
	}
}

func TestRun_Doctor(t *testing.T) {
	var stdout bytes.Buffer
	// Doctor may pass or fail depending on host environment; we just want to
	// confirm it runs and emits per-check lines.
	_ = run([]string{"sanderling", "doctor"}, &stdout, io.Discard)
	output := stdout.String()
	if !strings.Contains(output, "OK") && !strings.Contains(output, "FAIL") {
		t.Errorf("doctor output missing OK/FAIL lines: %q", output)
	}
}

func TestRun_TestSubcommand_PipelineErrors(t *testing.T) {
	// Without a real spec, a real device, or a bootable AVD the pipeline
	// must surface a specific error rather than panicking — proves the flag
	// wiring reaches the runner.
	err := run([]string{
		"sanderling", "test",
		"--spec", "definitely-missing-spec.ts",
		"--bundle-id", "com.example",
		"--avd", "definitely-missing-avd",
	}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	message := err.Error()
	ok := strings.Contains(message, "bundle") ||
		strings.Contains(message, "device") ||
		strings.Contains(message, "AVD") ||
		strings.Contains(message, "emulator")
	if !ok {
		t.Errorf("expected pipeline error (bundle/device/AVD/emulator), got %v", err)
	}
}
