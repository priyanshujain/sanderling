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

// readings hands out the given instants in turn, so a test can script a clock
// that jumps across a host sleep independently of one that stops through it.
func readings(instants ...time.Time) func() time.Time {
	var index int
	return func() time.Time {
		instant := instants[min(index, len(instants)-1)]
		index++
		return instant
	}
}

// A sleeping host stops the monotonic clock and not the wall clock, so a run
// timed on the monotonic clock alone reports the sleep as time that never
// passed. The record carries both, named for the clock each came from.
func TestRunSeed_RecordsTheTimeWorkedAndTheTimeThatPassed(t *testing.T) {
	directory := t.TempDir()
	startedAt := time.Date(2026, 8, 14, 2, 0, 0, 0, time.UTC)
	var records bytes.Buffer
	sweep := &campaign{
		configuration: testConfiguration(t, directory, "--seeds", "1"),
		executor: func(context.Context, string, []string, io.Writer) (int, error) {
			return 0, nil
		},
		stdout:  io.Discard,
		records: &records,
		clocks: clocks{
			monotonicNow: readings(startedAt, startedAt.Add(2*time.Minute)),
			wallClockNow: readings(startedAt, startedAt.Add(17*time.Minute)),
		},
	}

	sweep.report(sweep.runSeed(context.Background(), 1, ""))

	var written map[string]any
	if err := json.Unmarshal(records.Bytes(), &written); err != nil {
		t.Fatalf("decode %q: %v", records.String(), err)
	}
	if written["monotonic_millis"] != float64((2 * time.Minute).Milliseconds()) {
		t.Errorf("monotonic_millis %v, want the two minutes of work", written["monotonic_millis"])
	}
	if written["wall_clock_millis"] != float64((17 * time.Minute).Milliseconds()) {
		t.Errorf("wall_clock_millis %v, want the seventeen minutes that passed", written["wall_clock_millis"])
	}
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
	devicesPresent(t, "device-a", "device-b", "device-c")

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

func TestRunCampaign_GivesEveryRunTheCellsLabelSource(t *testing.T) {
	directory := t.TempDir()
	configuration := testConfiguration(t, directory, "--seeds", "1-3", "--label-source", "resource-id")

	var mutex sync.Mutex
	var dispatched []string
	executor := versionAnswering(func(_ context.Context, _ string, arguments []string, _ io.Writer) (int, error) {
		mutex.Lock()
		dispatched = append(dispatched, argumentValue(arguments, "--label-source"))
		mutex.Unlock()
		writeFakeRun(t, arguments, []trace.Step{observedStep(1)})
		return 0, nil
	})
	if err := runCampaign(context.Background(), configuration, executor, io.Discard); err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(dispatched, []string{"resource-id", "resource-id", "resource-id"}) {
		t.Errorf("--label-source reaching sanderling: got %v, want resource-id on every run", dispatched)
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

// devicesPresent points the preflight at a fixed set of serials, so a campaign
// can be preflighted on a host with no device farm attached.
func devicesPresent(t *testing.T, present ...string) {
	t.Helper()
	original := connectedDevices
	connectedDevices = func(context.Context) ([]string, error) { return present, nil }
	t.Cleanup(func() { connectedDevices = original })
}

// A worker aimed at a serial that no longer exists fails in seconds and pulls
// the next seed, so a few dead serials drain the queue while the healthy
// workers are still inside their first run. That has to be caught before the
// first seed is dispatched: a sweep that discovers it on run 1 of 20 has
// already been destroyed, and its output does not say so.
func TestRunCampaign_RefusesToStartWhenADeviceIsMissing(t *testing.T) {
	directory := t.TempDir()
	configuration := testConfiguration(t, directory,
		"--seeds", "1-20", "--devices", "emulator-5554,emulator-5564,emulator-5556")
	devicesPresent(t, "emulator-5554", "emulator-5556")

	executor := func(_ context.Context, _ string, arguments []string, _ io.Writer) (int, error) {
		t.Errorf("the campaign dispatched %v despite a missing device", arguments)
		return 0, nil
	}
	err := runCampaign(context.Background(), configuration, executor, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "not connected: emulator-5564") {
		t.Fatalf("the error must name the missing serial, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(directory, manifestFileName)); statErr == nil {
		t.Error("a campaign that cannot run wrote a manifest")
	}
}

func TestRunCampaign_RunsWhenEveryDeviceIsPresent(t *testing.T) {
	directory := t.TempDir()
	configuration := testConfiguration(t, directory, "--seeds", "1-2", "--devices", "emulator-5554,emulator-5556")
	devicesPresent(t, "emulator-5556", "emulator-5580", "emulator-5554")

	executor := versionAnswering(func(_ context.Context, _ string, arguments []string, _ io.Writer) (int, error) {
		writeFakeRun(t, arguments, []trace.Step{observedStep(1)})
		return 0, nil
	})
	if err := runCampaign(context.Background(), configuration, executor, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := len(readRecords(t, directory)); got != 2 {
		t.Errorf("records: got %d, want 2", got)
	}
}

// Preflight only means something where the worker names a device. A campaign
// without --devices has one worker and no serial, and a web worker is a label
// with no device behind it; neither may be blocked by a device check.
func TestRunCampaign_PreflightsOnlyWhereWorkersNameADevice(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		extra []string
	}{
		{"no devices", []string{"--seeds", "1"}},
		{"web workers", []string{"--seeds", "1", "--platform", "web", "--devices", "worker-a,worker-b"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			directory := t.TempDir()
			configuration := testConfiguration(t, directory, testCase.extra...)
			original := connectedDevices
			connectedDevices = func(context.Context) ([]string, error) {
				t.Error("preflight looked for devices where the workers name none")
				return nil, fmt.Errorf("no adb server")
			}
			t.Cleanup(func() { connectedDevices = original })

			executor := versionAnswering(func(_ context.Context, _ string, arguments []string, _ io.Writer) (int, error) {
				writeFakeRun(t, arguments, []trace.Step{observedStep(1)})
				return 0, nil
			})
			if err := runCampaign(context.Background(), configuration, executor, io.Discard); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Preflight cannot catch a device that disappears mid-campaign. A device that
// keeps failing in seconds is drained of seeds by exactly the speed of its
// failure, so it has to stop being given any.
func TestRunCampaign_QuarantinesADeviceThatKeepsFailingFast(t *testing.T) {
	directory := t.TempDir()
	configuration := testConfiguration(t, directory, "--seeds", "1-8", "--devices", "device-a,device-b")
	devicesPresent(t, "device-a", "device-b")

	failedEnough := make(chan struct{})
	var closeOnce sync.Once
	var timedOut atomic.Bool
	var mutex sync.Mutex
	dispatched := map[string][]int64{}

	executor := versionAnswering(func(_ context.Context, _ string, arguments []string, output io.Writer) (int, error) {
		device := argumentValue(arguments, "--device")
		seed, err := strconv.ParseInt(argumentValue(arguments, "--seed"), 10, 64)
		if err != nil {
			t.Errorf("seed argument: %v", err)
		}
		mutex.Lock()
		dispatched[device] = append(dispatched[device], seed)
		failures := len(dispatched["device-a"])
		mutex.Unlock()
		if device == "device-a" {
			fmt.Fprintln(output, "device 'device-a' not found")
			if failures >= fastFailuresBeforeQuarantine {
				closeOnce.Do(func() { close(failedEnough) })
			}
			return 1, nil
		}
		// The healthy worker holds its seed until the sick one has failed its
		// way to quarantine, so the split of the queue is the scheduler's
		// decision and not a race between two equally fast fakes.
		select {
		case <-failedEnough:
		case <-time.After(5 * time.Second):
			timedOut.Store(true)
		}
		writeFakeRun(t, arguments, []trace.Step{observedStep(1)})
		return 0, nil
	})

	var stdout bytes.Buffer
	err := runCampaign(context.Background(), configuration, executor, &stdout)
	if timedOut.Load() {
		t.Fatal("device-a never reached quarantine")
	}
	if err == nil || !strings.Contains(err.Error(), "3 of 8 runs failed") {
		t.Fatalf("expected the three fast failures to be reported, got %v", err)
	}
	if got := len(dispatched["device-a"]); got != fastFailuresBeforeQuarantine {
		t.Errorf("seeds sent to the failing device: got %d, want %d", got, fastFailuresBeforeQuarantine)
	}
	if got := len(dispatched["device-b"]); got != 8-fastFailuresBeforeQuarantine {
		t.Errorf("seeds sent to the healthy device: got %d, want %d", got, 8-fastFailuresBeforeQuarantine)
	}
	ran := append(append([]int64{}, dispatched["device-a"]...), dispatched["device-b"]...)
	slices.Sort(ran)
	if !slices.Equal(ran, []int64{1, 2, 3, 4, 5, 6, 7, 8}) {
		t.Errorf("every seed must be dispatched exactly once: got %v", ran)
	}
	if !strings.Contains(stdout.String(), `quarantined device "device-a"`) {
		t.Errorf("the quarantine must be reported in the campaign output:\n%s", stdout.String())
	}

	recorded := readManifest(t, directory)
	if len(recorded.Quarantined) != 1 || recorded.Quarantined[0].Device != "device-a" {
		t.Fatalf("the manifest must record the quarantine: %+v", recorded.Quarantined)
	}
	burned := slices.Clone(dispatched["device-a"])
	slices.Sort(burned)
	if !slices.Equal(recorded.Quarantined[0].ConsumedSeeds, burned) {
		t.Errorf("consumed seeds: got %v, want %v", recorded.Quarantined[0].ConsumedSeeds, burned)
	}
	if !slices.Equal(recorded.UnrunSeeds, burned) {
		t.Errorf("the seeds the quarantined device burned have no result and must be reported unrun: got %v, want %v",
			recorded.UnrunSeeds, burned)
	}
}

// Every device gone is not a campaign that should keep pulling seeds: the rest
// of the queue would be spent producing the same failure.
func TestRunCampaign_AbortsWhenEveryDeviceIsQuarantined(t *testing.T) {
	directory := t.TempDir()
	configuration := testConfiguration(t, directory, "--seeds", "1-10", "--devices", "device-a,device-b")
	devicesPresent(t, "device-a", "device-b")

	var dispatches atomic.Int32
	executor := versionAnswering(func(_ context.Context, _ string, _ []string, output io.Writer) (int, error) {
		dispatches.Add(1)
		fmt.Fprintln(output, "sidecar health check: context deadline exceeded")
		return 1, nil
	})

	var stdout bytes.Buffer
	err := runCampaign(context.Background(), configuration, executor, &stdout)
	if err == nil || !strings.Contains(err.Error(), "every device was quarantined") {
		t.Fatalf("a campaign with no device left must abort with a clear error, got %v", err)
	}
	if got := dispatches.Load(); got != int32(2*fastFailuresBeforeQuarantine) {
		t.Errorf("runs dispatched: got %d, want %d: the sweep must stop rather than spin through the queue",
			got, 2*fastFailuresBeforeQuarantine)
	}
	recorded := readManifest(t, directory)
	if !slices.Equal(recorded.UnrunSeeds, []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) {
		t.Errorf("every seed is either burned by a quarantined device or never dispatched: got %v", recorded.UnrunSeeds)
	}
}

func readManifest(t *testing.T, campaignDirectory string) manifest {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(campaignDirectory, manifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	var recorded manifest
	if err := json.Unmarshal(body, &recorded); err != nil {
		t.Fatal(err)
	}
	return recorded
}
