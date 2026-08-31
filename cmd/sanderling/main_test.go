package main

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/testrun"
	"github.com/priyanshujain/sanderling/internal/verifier"
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
	if !options.clearData {
		t.Errorf("clearData default: got false, want true")
	}
}

func TestParseTestArgs_ClearDataDisabled(t *testing.T) {
	options, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "com.example",
		"--clear-data=false",
	}, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if options.clearData {
		t.Errorf("expected clearData=false, got true")
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

// folioAPK is the android package's fixture, borrowed rather than copied so
// there is one compiled manifest in the tree to keep current.
const folioAPK = "../../internal/android/testdata/folio.apk"

func TestParseTestArgs_ReadsBundleIDFromTheAPK(t *testing.T) {
	options, err := parseTestArgs([]string{"--spec", "s.ts", "--android-app-path", folioAPK}, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if options.bundleID != "app.folio" {
		t.Fatalf("bundle id: got %q, want app.folio", options.bundleID)
	}
}

func TestParseTestArgs_ExplicitBundleIDOutranksTheAPK(t *testing.T) {
	options, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "app.folio.debug",
		"--android-app-path", folioAPK,
	}, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if options.bundleID != "app.folio.debug" {
		t.Fatalf("bundle id: got %q, want app.folio.debug", options.bundleID)
	}
}

func TestParseTestArgs_APKDoesNotNameTheBundleOffAndroid(t *testing.T) {
	_, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--platform", "ios",
		"--android-app-path", folioAPK,
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--bundle-id") {
		t.Fatalf("expected missing --bundle-id error, got %v", err)
	}
}

func TestParseTestArgs_UnreadableAPKIsReported(t *testing.T) {
	_, err := parseTestArgs([]string{"--spec", "s.ts", "--android-app-path", "testdata/absent.apk"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "testdata/absent.apk") {
		t.Fatalf("expected an error naming the apk, got %v", err)
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

func TestParseTestArgs_GeneratorDefaultsToSeeded(t *testing.T) {
	options, err := parseTestArgs([]string{"--spec", "s.ts", "--bundle-id", "com.example"}, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if options.generator != "seeded" {
		t.Fatalf("generator default: got %q, want seeded", options.generator)
	}
}

func TestParseTestArgs_AcceptsLLMGenerator(t *testing.T) {
	options, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "com.example",
		"--generator", "llm",
	}, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if options.generator != "llm" {
		t.Fatalf("generator: got %q, want llm", options.generator)
	}
}

func TestParseTestArgs_RejectsUnknownGenerator(t *testing.T) {
	_, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "com.example",
		"--generator", "oracle",
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "unsupported generator") {
		t.Fatalf("expected unsupported-generator error, got %v", err)
	}
}

// The label-source cases compare against the verifier's own constants, because
// the flag and the code that reads it are the two halves of one contract: a
// rename on either side would otherwise leave every run silently labelled by
// the default channel while meta.json claimed the other one.
func TestParseTestArgs_LabelSourceDefaultsToVisibleText(t *testing.T) {
	options, err := parseTestArgs([]string{"--spec", "s.ts", "--bundle-id", "com.example"}, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if options.labelSource != verifier.LabelSourceVisibleText {
		t.Fatalf("label source default: got %q, want %q", options.labelSource, verifier.LabelSourceVisibleText)
	}
}

func TestParseTestArgs_AcceptsResourceIDLabelSource(t *testing.T) {
	options, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "com.example",
		"--label-source", verifier.LabelSourceResourceID,
	}, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if options.labelSource != verifier.LabelSourceResourceID {
		t.Fatalf("label source: got %q, want %q", options.labelSource, verifier.LabelSourceResourceID)
	}
}

func TestParseTestArgs_RejectsUnknownLabelSource(t *testing.T) {
	_, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "com.example",
		"--label-source", "resource_id",
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "unsupported label source") {
		t.Fatalf("expected unsupported-label-source error, got %v", err)
	}
}

func TestPipelineOptionsCarriesTheExperimentCell(t *testing.T) {
	options, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "com.example",
		"--generator", "llm",
		"--label-source", verifier.LabelSourceResourceID,
		"--arm", "llm-resource-id",
	}, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pipeline := pipelineOptions(options)
	if pipeline.Generator != "llm" || pipeline.LabelSource != verifier.LabelSourceResourceID || pipeline.Arm != "llm-resource-id" {
		t.Errorf("cell lost between the flags and the pipeline: generator=%q labelSource=%q arm=%q",
			pipeline.Generator, pipeline.LabelSource, pipeline.Arm)
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

func TestParseTestArgs_AcceptsIosPlatform(t *testing.T) {
	options, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "com.example.app",
		"--platform", "ios",
	}, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error for ios platform: %v", err)
	}
	if options.platform != "ios" {
		t.Errorf("expected platform=ios, got %q", options.platform)
	}
}

func TestParseTestArgs_IosDeviceFlag(t *testing.T) {
	options, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "com.example.app",
		"--platform", "ios",
		"--ios-device", "iPhone 15 Pro",
	}, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if options.iosDevice != "iPhone 15 Pro" {
		t.Errorf("expected iosDevice=iPhone 15 Pro, got %q", options.iosDevice)
	}
}

func TestParseTestArgs_IosAppPathFlag(t *testing.T) {
	options, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "com.example.app",
		"--platform", "ios",
		"--ios-app-path", "/tmp/build/iosApp.app",
	}, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if options.iosAppPath != "/tmp/build/iosApp.app" {
		t.Errorf("expected iosAppPath=/tmp/build/iosApp.app, got %q", options.iosAppPath)
	}
}

func TestParseTestArgs_IosAppPathOptional(t *testing.T) {
	options, err := parseTestArgs([]string{"--spec", "s.ts", "--bundle-id", "com.example.app"}, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if options.iosAppPath != "" {
		t.Errorf("iosAppPath default: got %q, want empty", options.iosAppPath)
	}
}

func TestRun_TestSubcommand_PipelineErrors(t *testing.T) {
	// Web skips host-dependent device boot and bundles first, so a missing spec
	// deterministically surfaces a bundle-resolution error. This proves the
	// flag wiring reaches the bundler rather than panicking on the way.
	err := run([]string{
		"sanderling", "test",
		"--spec", "definitely-missing-spec.ts",
		"--bundle-id", "http://localhost:3000",
		"--platform", "web",
	}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	message := err.Error()
	if !strings.HasPrefix(message, "bundle spec:") || !strings.Contains(message, "definitely-missing-spec.ts") {
		t.Errorf("expected bundle-resolution error for the missing spec, got %v", err)
	}
}

func TestParseTestArgs_MaxStepsDefaultsToUncapped(t *testing.T) {
	options, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "com.example",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.maxSteps != 0 {
		t.Errorf("maxSteps default: got %d, want 0", options.maxSteps)
	}
}

func TestParseTestArgs_MaxSteps(t *testing.T) {
	options, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "com.example",
		"--max-steps", "300",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.maxSteps != 300 {
		t.Errorf("maxSteps: got %d, want 300", options.maxSteps)
	}
}

func TestParseTestArgs_RejectsNegativeMaxSteps(t *testing.T) {
	_, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "com.example",
		"--max-steps", "-1",
	}, io.Discard)
	if err == nil {
		t.Fatal("expected an error for a negative --max-steps")
	}
}

func TestParseTestArgs_ArmLabel(t *testing.T) {
	options, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "com.example",
		"--arm", "seeded-identifier",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.arm != "seeded-identifier" {
		t.Errorf("arm: got %q, want seeded-identifier", options.arm)
	}
}

func TestParseTestArgs_ExitOnViolationDefaultsOff(t *testing.T) {
	options, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "com.example",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.exitOnViolation {
		t.Error("exitOnViolation default: got true, want false")
	}
}

func TestParseTestArgs_ExitOnViolation(t *testing.T) {
	options, err := parseTestArgs([]string{
		"--spec", "s.ts",
		"--bundle-id", "com.example",
		"--exit-on-violation",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !options.exitOnViolation {
		t.Error("expected exitOnViolation=true")
	}
}

// TestParseTestArgs_AllowNoPropertiesReachesThePipeline pins the opt-out the
// extraction and portability sweeps pass. Dropped here, the guard is either
// unreachable or permanent: a sweep that deliberately judges nothing cannot ask
// for it, and every other run keeps the false green the guard exists to stop.
func TestParseTestArgs_AllowNoPropertiesReachesThePipeline(t *testing.T) {
	base := []string{"--spec", "s.ts", "--bundle-id", "com.example"}

	options, err := parseTestArgs(base, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if pipelineOptions(options).AllowNoProperties {
		t.Error("allowNoProperties default: got true, want false")
	}

	options, err = parseTestArgs(append(base, "--allow-no-properties"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !pipelineOptions(options).AllowNoProperties {
		t.Error("--allow-no-properties never reached the pipeline options")
	}
}

// The exploration sweeps ask for a run the generator never drove, and they ask
// for that alone. Riding on --allow-no-properties made one flag name two
// unrelated waivers, so a sweep that wanted the property-free one silently lost
// the dead-run detector as well.
func TestParseTestArgs_AllowNoGeneratorActionsIsItsOwnFlag(t *testing.T) {
	base := []string{"--spec", "s.ts", "--bundle-id", "com.example"}

	options, err := parseTestArgs(base, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if pipelineOptions(options).AllowNoGeneratorActions {
		t.Error("allowNoGeneratorActions default: got true, want false")
	}

	options, err = parseTestArgs(append(base, "--allow-no-generator-actions"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !pipelineOptions(options).AllowNoGeneratorActions {
		t.Error("--allow-no-generator-actions never reached the pipeline options")
	}

	options, err = parseTestArgs(append(base, "--allow-no-properties"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if pipelineOptions(options).AllowNoGeneratorActions {
		t.Error("--allow-no-properties still waives the dead-run refusal it does not name")
	}
}

// TestExitCode_SeparatesFoundBugsFromBrokenHarnesses pins the three statuses CI
// reads: 0 clean, 2 the run found violations, 1 everything else. A workflow
// that asserts "the known bug is still found" is only meaningful while 2 and 1
// stay distinct.
func TestExitCode_SeparatesFoundBugsFromBrokenHarnesses(t *testing.T) {
	for _, testCase := range []struct {
		name string
		err  error
		want int
		says string
	}{
		{"clean run", nil, 0, ""},
		{"help", flag.ErrHelp, 0, ""},
		{"violations found", testrun.ViolationsError{Count: 2}, 2, "violations: 2"},
		{"broken harness", errors.New("launch app: no device"), 1, "error: launch app"},
		{"spec judges nothing", testrun.NoPropertiesError{Spec: "s.ts"}, 1, "registers no properties"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if got := exitCode(testCase.err, &stderr); got != testCase.want {
				t.Errorf("exit code: got %d, want %d", got, testCase.want)
			}
			if testCase.says != "" && !strings.Contains(stderr.String(), testCase.says) {
				t.Errorf("stderr %q does not mention %q", stderr.String(), testCase.says)
			}
		})
	}
}
