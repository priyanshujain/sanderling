package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/trace"
)

func testConfiguration(t *testing.T, outputDirectory string, extra ...string) config {
	t.Helper()
	arguments := append(baseArguments(), "--output", outputDirectory)
	configuration, err := parseArguments(append(arguments, extra...), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	return configuration
}

// versionAnswering wraps a test executor so every fake answers `sanderling
// version`, which the campaign probes before it writes the manifest.
func versionAnswering(executor commandExecutor) commandExecutor {
	return func(ctx context.Context, binary string, arguments []string, output io.Writer) (int, error) {
		if len(arguments) > 0 && arguments[0] == "version" {
			fmt.Fprintln(output, "stub-version")
			return 0, nil
		}
		return executor(ctx, binary, arguments, output)
	}
}

func readRecords(t *testing.T, campaignDirectory string) []runRecord {
	t.Helper()
	file, err := os.Open(filepath.Join(campaignDirectory, recordsFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var records []runRecord
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record runRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode %q: %v", scanner.Text(), err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return records
}

func writeFakeRun(t *testing.T, arguments []string, steps []trace.Step) {
	t.Helper()
	writeRunDirectory(t, argumentValue(arguments, "--output"), "20260101-000000", steps)
}

func TestRunCampaign_RecordsDispatchedActionsNotSteps(t *testing.T) {
	directory := t.TempDir()
	configuration := testConfiguration(t, directory, "--seeds", "1")

	executor := versionAnswering(func(_ context.Context, _ string, arguments []string, _ io.Writer) (int, error) {
		writeFakeRun(t, arguments, []trace.Step{
			actingStep(1), observedStep(2), skippedActionStep(3, "unresolved_selector"), actingStep(4),
		})
		return 0, nil
	})
	if err := runCampaign(context.Background(), configuration, executor, io.Discard); err != nil {
		t.Fatal(err)
	}

	records := readRecords(t, directory)
	if len(records) != 1 {
		t.Fatalf("records: got %d, want 1", len(records))
	}
	if records[0].Steps != 4 || records[0].Actions != 2 {
		t.Errorf("steps %d actions %d, want 4 and 2", records[0].Steps, records[0].Actions)
	}
}

// A run that never produced a readable trace still has to carry the field, so
// analysis can tell a zero-action run from a file written before the count.
func TestRunRecord_AlwaysCarriesTheActionCount(t *testing.T) {
	body, err := json.Marshal(runRecord{Seed: 7, TraceError: "no run directory with meta.json"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"actions":0`) {
		t.Errorf("record %s omits the action count", body)
	}
}

func TestRunCampaign_WritesManifestBeforeAnyRun(t *testing.T) {
	directory := t.TempDir()
	configuration := testConfiguration(t, directory)
	var stdout bytes.Buffer

	executor := versionAnswering(func(_ context.Context, _ string, arguments []string, _ io.Writer) (int, error) {
		if _, err := os.Stat(filepath.Join(directory, manifestFileName)); err != nil {
			t.Errorf("manifest missing when the first run started: %v", err)
		}
		writeFakeRun(t, arguments, []trace.Step{observedStep(1)})
		return 0, nil
	})
	if err := runCampaign(context.Background(), configuration, executor, &stdout); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(directory, manifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	var recorded manifest
	if err := json.Unmarshal(body, &recorded); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(recorded.Seeds, []int64{1, 2, 3}) {
		t.Errorf("intended seeds: got %v", recorded.Seeds)
	}
	if recorded.SanderlingVersion != "stub-version" {
		t.Errorf("version: got %q", recorded.SanderlingVersion)
	}
	if recorded.Host == "" {
		t.Error("host was not recorded")
	}
}

func TestRunCampaign_RecordsPerRunSummary(t *testing.T) {
	directory := t.TempDir()
	configuration := testConfiguration(t, directory, "--seeds", "1-3")
	var stdout bytes.Buffer

	executor := versionAnswering(func(_ context.Context, _ string, arguments []string, output io.Writer) (int, error) {
		fmt.Fprintln(output, "stub run log")
		steps := []trace.Step{observedStep(1), observedStep(2), observedStep(3)}
		if argumentValue(arguments, "--seed") == "2" {
			violating := observedStep(3)
			violating.Violations = []string{"listNeverEmpty"}
			violating.Witnesses = map[string]trace.Witness{
				"listNeverEmpty": {Reason: "list emptied", Step: 2, DetectedStep: 3},
			}
			steps[2] = violating
		}
		writeFakeRun(t, arguments, steps)
		return 0, nil
	})
	if err := runCampaign(context.Background(), configuration, executor, &stdout); err != nil {
		t.Fatal(err)
	}

	records := readRecords(t, directory)
	if len(records) != 3 {
		t.Fatalf("records: got %d, want 3", len(records))
	}
	for _, record := range records {
		if record.Steps != 3 {
			t.Errorf("seed %d steps: got %d, want 3", record.Seed, record.Steps)
		}
		if record.RunDirectory != fmt.Sprintf("seed-%d/20260101-000000", record.Seed) {
			t.Errorf("seed %d run directory: got %q", record.Seed, record.RunDirectory)
		}
		if record.TraceError != "" {
			t.Errorf("seed %d trace error: %s", record.Seed, record.TraceError)
		}
		violated := record.FirstViolationOriginStep != nil
		if violated != (record.Seed == 2) {
			t.Errorf("seed %d violation: got %v", record.Seed, record.FirstViolationOriginStep)
		}
		if record.Seed != 2 {
			continue
		}
		if *record.FirstViolationOriginStep != 2 || *record.FirstViolationDetectedStep != 3 {
			t.Errorf("seed 2 violation steps: origin %d detected %d",
				*record.FirstViolationOriginStep, *record.FirstViolationDetectedStep)
		}
		if !slices.Equal(record.ViolatedProperties, []string{"listNeverEmpty"}) {
			t.Errorf("seed 2 properties: got %v", record.ViolatedProperties)
		}
	}

	log, err := os.ReadFile(filepath.Join(directory, "seed-1", "sanderling.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "stub run log") {
		t.Errorf("per-run log: got %q", log)
	}
	if !strings.Contains(stdout.String(), "seed=2") {
		t.Errorf("progress output missing seed 2: %q", stdout.String())
	}
}

func TestRunCampaign_DistributesSeedsAcrossDeviceWorkers(t *testing.T) {
	directory := t.TempDir()
	configuration := testConfiguration(t, directory, "--seeds", "1-9", "--devices", "device-a,device-b,device-c")

	var mutex sync.Mutex
	assignments := map[int64]string{}
	var inFlight atomic.Int32
	var releaseOnce sync.Once
	var timedOut atomic.Bool
	release := make(chan struct{})

	executor := versionAnswering(func(_ context.Context, _ string, arguments []string, _ io.Writer) (int, error) {
		if inFlight.Add(1) >= 3 {
			releaseOnce.Do(func() { close(release) })
		}
		select {
		case <-release:
		case <-time.After(5 * time.Second):
			timedOut.Store(true)
		}
		seed, err := strconv.ParseInt(argumentValue(arguments, "--seed"), 10, 64)
		if err != nil {
			t.Errorf("seed argument: %v", err)
		}
		mutex.Lock()
		if previous, seen := assignments[seed]; seen {
			t.Errorf("seed %d ran twice (%s then %s)", seed, previous, argumentValue(arguments, "--device"))
		}
		assignments[seed] = argumentValue(arguments, "--device")
		mutex.Unlock()
		writeFakeRun(t, arguments, []trace.Step{observedStep(1)})
		return 0, nil
	})
	if err := runCampaign(context.Background(), configuration, executor, io.Discard); err != nil {
		t.Fatal(err)
	}
	if timedOut.Load() {
		t.Fatal("three device workers never ran concurrently")
	}
	if len(assignments) != 9 {
		t.Fatalf("seeds run: got %d, want 9", len(assignments))
	}
	used := map[string]int{}
	for seed, device := range assignments {
		if !slices.Contains(configuration.devices, device) {
			t.Errorf("seed %d ran on unknown device %q", seed, device)
		}
		used[device]++
	}
	if len(used) != 3 {
		t.Errorf("devices used: got %v, want all three", used)
	}
	if got := len(readRecords(t, directory)); got != 9 {
		t.Errorf("records: got %d, want 9", got)
	}
}

func TestRunCampaign_ContinuesAfterFailingRun(t *testing.T) {
	directory := t.TempDir()
	configuration := testConfiguration(t, directory, "--seeds", "1-3")

	executor := versionAnswering(func(_ context.Context, _ string, arguments []string, output io.Writer) (int, error) {
		if argumentValue(arguments, "--seed") == "2" {
			fmt.Fprintln(output, "error: device offline")
			return 1, nil
		}
		writeFakeRun(t, arguments, []trace.Step{observedStep(1)})
		return 0, nil
	})
	err := runCampaign(context.Background(), configuration, executor, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "1 of 3 runs failed") {
		t.Fatalf("expected a failure summary, got %v", err)
	}

	records := readRecords(t, directory)
	if len(records) != 3 {
		t.Fatalf("a failing run must not abort the campaign: got %d records", len(records))
	}
	for _, record := range records {
		if record.Seed == 2 {
			if record.ExitCode != 1 {
				t.Errorf("seed 2 exit code: got %d, want 1", record.ExitCode)
			}
			if record.TraceError == "" {
				t.Error("seed 2 produced no trace; that should be recorded")
			}
			continue
		}
		if record.ExitCode != 0 {
			t.Errorf("seed %d exit code: got %d", record.Seed, record.ExitCode)
		}
	}
}

func TestRunCampaign_RefusesToReuseACampaignDirectory(t *testing.T) {
	directory := t.TempDir()
	configuration := testConfiguration(t, directory)
	if err := os.WriteFile(filepath.Join(directory, manifestFileName), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := versionAnswering(func(context.Context, string, []string, io.Writer) (int, error) {
		t.Error("no run should start in a directory that already holds a campaign")
		return 0, nil
	})
	if err := runCampaign(context.Background(), configuration, executor, io.Discard); err == nil {
		t.Fatal("expected a refusal to reuse the campaign directory")
	}
}

func TestRunCampaign_AbortsWhenVersionProbeFails(t *testing.T) {
	directory := t.TempDir()
	configuration := testConfiguration(t, directory)
	executor := func(_ context.Context, _ string, arguments []string, output io.Writer) (int, error) {
		if arguments[0] != "version" {
			t.Error("a run started despite an unusable binary")
		}
		fmt.Fprintln(output, "no such command")
		return 2, nil
	}
	if err := runCampaign(context.Background(), configuration, executor, io.Discard); err == nil {
		t.Fatal("expected the campaign to abort before writing a manifest it cannot attribute")
	}
	if _, err := os.Stat(filepath.Join(directory, manifestFileName)); err == nil {
		t.Error("manifest was written despite an unusable binary")
	}
}

func TestRunCampaign_UnreadableTraceIsNotASuccessfulCampaign(t *testing.T) {
	campaignDirectory := filepath.Join(t.TempDir(), "cell")
	configuration := testConfiguration(t, campaignDirectory, "--seeds", "1-2")

	executor := versionAnswering(func(context.Context, string, []string, io.Writer) (int, error) {
		return 0, nil
	})

	var stdout bytes.Buffer
	err := runCampaign(context.Background(), configuration, executor, &stdout)
	if err == nil || !strings.Contains(err.Error(), "2 of 2 runs produced an unreadable trace") {
		t.Fatalf("a campaign whose runs left no readable trace must not report success: %v", err)
	}
	for _, record := range readRecords(t, campaignDirectory) {
		if record.TraceError == "" {
			t.Errorf("seed %d: expected a trace error, got none", record.Seed)
		}
	}
}

func TestRunCampaign_KillsAndRecordsAWedgedRun(t *testing.T) {
	campaignDirectory := filepath.Join(t.TempDir(), "cell")
	configuration := testConfiguration(t, campaignDirectory,
		"--seeds", "1", "--duration", "50ms", "--run-timeout", "300ms")

	executor := versionAnswering(func(ctx context.Context, _ string, _ []string, _ io.Writer) (int, error) {
		<-ctx.Done()
		return -1, ctx.Err()
	})

	var stdout bytes.Buffer
	if err := runCampaign(context.Background(), configuration, executor, &stdout); err == nil {
		t.Fatal("a campaign whose only run was killed must not report success")
	}
	records := readRecords(t, campaignDirectory)
	if len(records) != 1 {
		t.Fatalf("want one record, got %d", len(records))
	}
	if !records[0].TimedOut {
		t.Error("a run killed by the run timeout must be recorded as timed out")
	}
	if !strings.Contains(stdout.String(), "timed out") {
		t.Errorf("the progress line must name the outcome:\n%s", stdout.String())
	}
}

func TestParseArguments_RunTimeoutMustExceedDuration(t *testing.T) {
	_, err := parseArguments(append(baseArguments(),
		"--output", t.TempDir(), "--duration", "5m", "--run-timeout", "1m"), io.Discard)
	if err == nil {
		t.Fatal("a run timeout below the duration would kill every run before it finished")
	}
}

func TestParseArguments_RunTimeoutDefaultsToThreeTimesDuration(t *testing.T) {
	configuration, err := parseArguments(append(baseArguments(),
		"--output", t.TempDir(), "--duration", "4m"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if configuration.runTimeout != 12*time.Minute {
		t.Errorf("run timeout default: got %s, want 12m", configuration.runTimeout)
	}
}
