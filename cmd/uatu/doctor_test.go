package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
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
