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

func TestParseTestArgs_RequiresAVDOnAndroid(t *testing.T) {
	_, err := parseTestArgs([]string{"--spec", "s.ts", "--bundle-id", "com.example"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--avd") {
		t.Fatalf("expected missing --avd error, got %v", err)
	}
}

func TestParseTestArgs_RejectsNonAndroidPlatform(t *testing.T) {
	_, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "com.example",
		"--platform", "ios",
		"--avd", "x",
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "unsupported platform") {
		t.Fatalf("expected unsupported-platform error, got %v", err)
	}
}

func TestRun_HelpPrintsUsage(t *testing.T) {
	var stdout bytes.Buffer
	if err := run([]string{"uatu"}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "uatu <command>") {
		t.Errorf("usage missing, got: %q", stdout.String())
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	err := run([]string{"uatu", "wat"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("expected unknown-command error, got %v", err)
	}
}

func TestRun_Doctor(t *testing.T) {
	var stdout bytes.Buffer
	// Doctor may pass or fail depending on host environment; we just want to
	// confirm it runs and emits per-check lines.
	_ = run([]string{"uatu", "doctor"}, &stdout, io.Discard)
	output := stdout.String()
	if !strings.Contains(output, "OK") && !strings.Contains(output, "FAIL") {
		t.Errorf("doctor output missing OK/FAIL lines: %q", output)
	}
}

func TestRun_TestSubcommand(t *testing.T) {
	var stdout bytes.Buffer
	err := run([]string{
		"uatu", "test",
		"--spec", "s.ts",
		"--bundle-id", "com.example",
		"--avd", "Pixel_5_API_33",
	}, &stdout, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "uatu test (stub)") {
		t.Errorf("test stub output missing, got: %q", stdout.String())
	}
}
