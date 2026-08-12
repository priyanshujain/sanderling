package main

import (
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

func baseArguments() []string {
	return []string{
		"--spec", "/specs/folio.ts",
		"--bundle-id", "app.folio",
		"--platform", "android",
		"--arm", "seeded-baseline",
		"--generator", "seeded",
		"--max-steps", "300",
		"--seeds", "1-3",
		"--output", "/campaigns/a",
	}
}

func TestParseArguments_AcceptsFullInvocation(t *testing.T) {
	configuration, err := parseArguments(append(baseArguments(), "--devices", "emulator-5554,emulator-5556"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if configuration.arm != "seeded-baseline" || configuration.maxSteps != 300 {
		t.Errorf("arm/max-steps: got %q/%d", configuration.arm, configuration.maxSteps)
	}
	if !slices.Equal(configuration.seeds, []int64{1, 2, 3}) {
		t.Errorf("seeds: got %v", configuration.seeds)
	}
	if !slices.Equal(configuration.devices, []string{"emulator-5554", "emulator-5556"}) {
		t.Errorf("devices: got %v", configuration.devices)
	}
	if configuration.duration != 5*time.Minute {
		t.Errorf("duration default: got %s", configuration.duration)
	}
	if configuration.sanderlingPath != "sanderling" {
		t.Errorf("sanderling default: got %q", configuration.sanderlingPath)
	}
}

func TestParseArguments_Rejections(t *testing.T) {
	cases := []struct {
		name      string
		arguments []string
		want      string
	}{
		{"missing spec", []string{"--bundle-id", "a", "--arm", "b", "--seeds", "1", "--output", "o"}, "--spec is required"},
		{"missing arm", []string{"--spec", "s", "--bundle-id", "a", "--seeds", "1", "--output", "o", "--max-steps", "10"}, "--arm is required"},
		{"missing output", []string{"--spec", "s", "--bundle-id", "a", "--arm", "b", "--seeds", "1", "--max-steps", "10"}, "--output is required"},
		{"bad platform", append(baseArguments(), "--platform", "windows"), "unsupported platform"},
		{"bad generator", append(baseArguments(), "--generator", "vibes"), "unsupported generator"},
		{"zero max steps", append(baseArguments(), "--max-steps", "0"), "--max-steps must be positive"},
		{"seed zero", append(baseArguments(), "--seeds", "0-2"), "not reproducible"},
		{"duplicate device", append(baseArguments(), "--devices", "a,a"), "duplicate device"},
	}
	for _, testCase := range cases {
		_, err := parseArguments(testCase.arguments, io.Discard)
		if err == nil {
			t.Errorf("%s: expected error", testCase.name)
			continue
		}
		if !strings.Contains(err.Error(), testCase.want) {
			t.Errorf("%s: got %q, want it to contain %q", testCase.name, err, testCase.want)
		}
	}
}

func TestRunArguments_PlatformDeviceFlagAndPassthrough(t *testing.T) {
	cases := []struct {
		platform string
		want     []string
	}{
		{"android", []string{"--device", "target-1"}},
		{"ios", []string{"--ios-device", "target-1"}},
		{"web", nil},
	}
	for _, testCase := range cases {
		arguments := append(baseArguments(), "--platform", testCase.platform, "--", "--clear-data=false")
		configuration, err := parseArguments(arguments, io.Discard)
		if err != nil {
			t.Fatalf("%s: %v", testCase.platform, err)
		}
		got := runArguments(configuration, "7", "target-1")
		if got[0] != "test" {
			t.Errorf("%s: first argument is %q, want test", testCase.platform, got[0])
		}
		for _, pair := range [][2]string{
			{"--seed", "7"},
			{"--arm", "seeded-baseline"},
			{"--max-steps", "300"},
			{"--generator", "seeded"},
			{"--output", "/campaigns/a/seed-7"},
		} {
			if value := argumentValue(got, pair[0]); value != pair[1] {
				t.Errorf("%s: %s = %q, want %q", testCase.platform, pair[0], value, pair[1])
			}
		}
		if testCase.want != nil {
			if value := argumentValue(got, testCase.want[0]); value != testCase.want[1] {
				t.Errorf("%s: %s = %q, want %q", testCase.platform, testCase.want[0], value, testCase.want[1])
			}
		} else if slices.Contains(got, "--device") || slices.Contains(got, "--ios-device") {
			t.Errorf("web: unexpected device flag in %v", got)
		}
		if got[len(got)-1] != "--clear-data=false" {
			t.Errorf("%s: passthrough argument lost: %v", testCase.platform, got)
		}
	}
}

func argumentValue(arguments []string, name string) string {
	index := slices.Index(arguments, name)
	if index < 0 || index+1 >= len(arguments) {
		return ""
	}
	return arguments[index+1]
}
