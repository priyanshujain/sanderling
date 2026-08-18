package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/priyanshujain/sanderling/internal/android"
)

// commandExecutor runs one sanderling invocation and returns its exit code.
// A non-nil error means the process could not be run at all, which is a
// different failure from a run that started and exited non-zero.
type commandExecutor func(ctx context.Context, binary string, arguments []string, output io.Writer) (int, error)

func executeCommand(ctx context.Context, binary string, arguments []string, output io.Writer) (int, error) {
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Stdout = output
	command.Stderr = output
	// SIGTERM rather than the default kill: a run killed outright never runs its
	// own shutdown, and its sidecar survives holding a port and a quarter
	// gigabyte. The run timeout exists for unattended hosts, which is exactly
	// where nobody is watching to reap what it leaves.
	command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
	command.WaitDelay = runShutdownGrace
	err := command.Run()
	if err == nil {
		return 0, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode(), nil
	}
	if ctx.Err() != nil {
		return -1, nil
	}
	return -1, err
}

// connectedDevices lists the devices the host currently has. A variable so a
// preflight test runs without a device farm attached.
var connectedDevices = android.ConnectedDevices

// A failure that came back in less than fastFailureThreshold never did the
// work the run was asked to do: a step-budgeted run takes tens of minutes,
// while a worker pointed at a device that is gone gives up in about half a
// minute. fastFailuresBeforeQuarantine of those in a row, with no run that
// worked in between to reset the count, is a property of the device rather
// than a flake, and it is where the cost of being wrong (one worker's
// throughput, since the seeds stay on the shared queue) is still smaller than
// the cost of being right one seed later.
const (
	fastFailureThreshold         = 2 * time.Minute
	fastFailuresBeforeQuarantine = 3
)

// runShutdownGrace bounds how long a signalled run gets to stop its sidecar
// before it is killed. It exceeds the sidecar's own 15s shutdown grace, or the
// escalation would land while the run was still doing what it was asked.
const runShutdownGrace = 30 * time.Second

// runRecord is one line of runs.jsonl. MonotonicMillis is how long the run
// worked and WallClockMillis is how much time passed; they answer different
// questions and differ by however long the host slept mid-run, which an
// unattended overnight sweep is exactly where to expect.
type runRecord struct {
	Seed            int64     `json:"seed"`
	Device          string    `json:"device,omitempty"`
	ExitCode        int       `json:"exit_code"`
	LaunchError     string    `json:"launch_error,omitempty"`
	TimedOut        bool      `json:"timed_out,omitempty"`
	StartedAt       time.Time `json:"started_at"`
	MonotonicMillis int64     `json:"monotonic_millis"`
	WallClockMillis int64     `json:"wall_clock_millis"`
	RunDirectory    string    `json:"run_directory,omitempty"`
	TraceError      string    `json:"trace_error,omitempty"`
	traceSummary
}

// clocks reads the two measures a run is timed on. The monotonic clock does
// not advance while the host is asleep, so on its own it reports a run that
// slept through a quarter of an hour as a quarter of an hour shorter than it
// was; the wall clock advances but can be stepped by the host.
type clocks struct {
	monotonicNow func() time.Time
	wallClockNow func() time.Time
}

func systemClocks() clocks {
	return clocks{
		monotonicNow: time.Now,
		// Round(0) drops the monotonic reading time.Now carries, so
		// subtracting two of these readings uses the wall clock.
		wallClockNow: func() time.Time { return time.Now().Round(0) },
	}
}

type campaign struct {
	configuration config
	executor      commandExecutor
	stdout        io.Writer
	records       io.Writer
	clocks        clocks
	mutex         sync.Mutex
	failures      int
	unreadable    int
	quarantined   []quarantinedDevice
	// unrunSeeds are the seeds left without a trustworthy result: those a
	// quarantined device consumed on its way out, and those still queued when
	// the last worker stopped.
	unrunSeeds []int64
}

// failureStreak is one worker's run of fast failures, and the seeds they cost.
type failureStreak struct {
	fastFailures  int
	consumedSeeds []int64
}

// failedFast reports a run that came back non-zero too quickly to have done the
// work it was given. A killed run is excluded: it outlived the run timeout,
// which is the opposite of failing fast.
func failedFast(record runRecord) bool {
	return record.ExitCode != 0 && !record.TimedOut &&
		time.Duration(record.MonotonicMillis)*time.Millisecond < fastFailureThreshold
}

// preflightDevices refuses to start until every serial in --devices is present.
// A worker aimed at a serial that is gone fails in seconds and pulls the next
// seed, so a handful of dead serials drain the queue while the healthy workers
// are still inside their first run. Discovering that on run 1 of 20 is already
// too late: the sweep is spent, and its output does not say why.
func preflightDevices(ctx context.Context, configuration config) error {
	// Android is the platform whose worker names a device this can enumerate: a
	// web worker is a label with nothing behind it, and an --ios-device is
	// resolved by simctl or by devicectl depending on whether it names a
	// simulator or a paired phone.
	if configuration.platform != "android" || len(configuration.devices) == 0 {
		return nil
	}
	present, err := connectedDevices(ctx)
	if err != nil {
		return fmt.Errorf("list android devices: %w", err)
	}
	var missing []string
	for _, device := range configuration.devices {
		if !slices.Contains(present, device) {
			missing = append(missing, device)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("not starting: %d of %d --devices not connected: %s (adb reports %s)",
		len(missing), len(configuration.devices), strings.Join(missing, ", "), presentDevices(present))
}

func presentDevices(present []string) string {
	if len(present) == 0 {
		return "no devices"
	}
	return strings.Join(present, ", ")
}

func runCampaign(ctx context.Context, configuration config, executor commandExecutor, stdout io.Writer) error {
	if _, err := os.Stat(filepath.Join(configuration.outputDirectory, manifestFileName)); err == nil {
		return fmt.Errorf("%s already exists in %s: pick a fresh --output so two campaigns do not share a directory",
			manifestFileName, configuration.outputDirectory)
	}
	if err := preflightDevices(ctx, configuration); err != nil {
		return err
	}
	if err := os.MkdirAll(configuration.outputDirectory, 0o755); err != nil {
		return fmt.Errorf("create campaign dir: %w", err)
	}
	binaryPath := configuration.sanderlingPath
	if resolved, err := exec.LookPath(binaryPath); err == nil {
		if absolute, err := filepath.Abs(resolved); err == nil {
			binaryPath = absolute
		}
	}
	version, err := probeVersion(ctx, configuration.sanderlingPath, executor)
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	intended := buildManifest(configuration, host, binaryPath, version, time.Now().UTC())
	if err := writeManifest(configuration.outputDirectory, intended); err != nil {
		return fmt.Errorf("write %s: %w", manifestFileName, err)
	}

	recordsFile, err := os.OpenFile(filepath.Join(configuration.outputDirectory, recordsFileName),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", recordsFileName, err)
	}
	defer recordsFile.Close()

	sweep := &campaign{configuration: configuration, executor: executor, stdout: stdout, records: recordsFile, clocks: systemClocks()}
	fmt.Fprintf(stdout, "campaign %s: %d seeds, %d worker(s), %s\n",
		configuration.arm, len(configuration.seeds), len(workerDevices(configuration.devices)), configuration.outputDirectory)
	sweep.sweep(ctx)

	fmt.Fprintf(stdout, "campaign complete: %d of %d runs failed, %d produced an unreadable trace\n",
		sweep.failures, len(configuration.seeds), sweep.unreadable)
	if len(sweep.quarantined) > 0 || len(sweep.unrunSeeds) > 0 {
		intended.Quarantined = sweep.quarantined
		intended.UnrunSeeds = sweep.unrunSeeds
		if err := writeManifest(configuration.outputDirectory, intended); err != nil {
			fmt.Fprintf(stdout, "warning: rewrite %s: %v\n", manifestFileName, err)
		}
		fmt.Fprintf(stdout, "%d of %d seeds have no result: %v\n",
			len(sweep.unrunSeeds), len(configuration.seeds), sweep.unrunSeeds)
	}
	if len(sweep.quarantined) > 0 && len(sweep.quarantined) == len(workerDevices(configuration.devices)) {
		return fmt.Errorf("every device was quarantined (%s); %d of %d seeds have no result",
			strings.Join(quarantinedNames(sweep.quarantined), ", "), len(sweep.unrunSeeds), len(configuration.seeds))
	}
	if sweep.failures > 0 {
		return fmt.Errorf("%d of %d runs failed", sweep.failures, len(configuration.seeds))
	}
	if sweep.unreadable > 0 {
		// A run that exits 0 and leaves a trace the analysis cannot read is a
		// lost cell, not a successful campaign, and an unattended sweep has to
		// say so rather than reporting no failures.
		return fmt.Errorf("%d of %d runs produced an unreadable trace", sweep.unreadable, len(configuration.seeds))
	}
	return nil
}

func probeVersion(ctx context.Context, binary string, executor commandExecutor) (string, error) {
	var output bytes.Buffer
	code, err := executor(ctx, binary, []string{"version"}, &output)
	if err != nil {
		return "", fmt.Errorf("run %s version: %w", binary, err)
	}
	if code != 0 {
		return "", fmt.Errorf("%s version exited %d: %s", binary, code, strings.TrimSpace(output.String()))
	}
	return strings.TrimSpace(output.String()), nil
}

// workerDevices returns one entry per concurrent worker. With no --devices
// there is a single worker and no device to name.
func workerDevices(devices []string) []string {
	if len(devices) == 0 {
		return []string{""}
	}
	return devices
}

func (c *campaign) sweep(ctx context.Context) {
	queue := make(chan int64, len(c.configuration.seeds))
	for _, seed := range c.configuration.seeds {
		queue <- seed
	}
	close(queue)

	var waitGroup sync.WaitGroup
	for _, device := range workerDevices(c.configuration.devices) {
		waitGroup.Add(1)
		go func(device string) {
			defer waitGroup.Done()
			c.work(ctx, device, queue)
		}(device)
	}
	waitGroup.Wait()

	for seed := range queue {
		c.unrunSeeds = append(c.unrunSeeds, seed)
	}
	slices.Sort(c.unrunSeeds)
}

// work runs seeds on one device until the queue is empty or the device has
// failed fast often enough in a row to be quarantined. The streak is worker
// local: one worker drives one device, and any run that did its work clears it.
func (c *campaign) work(ctx context.Context, device string, queue <-chan int64) {
	var streak failureStreak
	for seed := range queue {
		if ctx.Err() != nil {
			return
		}
		record := c.runSeed(ctx, seed, device)
		c.report(record)
		// A cancelled campaign fails every run in flight in seconds, which is
		// the shutdown and not the device.
		if ctx.Err() != nil {
			return
		}
		if !failedFast(record) {
			streak = failureStreak{}
			continue
		}
		streak.fastFailures++
		streak.consumedSeeds = append(streak.consumedSeeds, seed)
		if streak.fastFailures >= fastFailuresBeforeQuarantine {
			c.quarantine(device, streak)
			return
		}
	}
}

// quarantine takes a device out of the sweep. The seeds it burned are reported
// unrun rather than requeued: each already holds that attempt's log, and a
// second record for the same seed would make one seed count as two runs.
func (c *campaign) quarantine(device string, streak failureStreak) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.quarantined = append(c.quarantined, quarantinedDevice{
		Device:        device,
		FastFailures:  streak.fastFailures,
		ConsumedSeeds: streak.consumedSeeds,
	})
	c.unrunSeeds = append(c.unrunSeeds, streak.consumedSeeds...)
	fmt.Fprintf(c.stdout, "quarantined device %q: %d runs in a row failed in under %s; seeds %v have no result\n",
		device, streak.fastFailures, fastFailureThreshold, streak.consumedSeeds)
}

func quarantinedNames(devices []quarantinedDevice) []string {
	names := make([]string, 0, len(devices))
	for _, device := range devices {
		names = append(names, device.Device)
	}
	return names
}

func (c *campaign) runSeed(ctx context.Context, seed int64, device string) runRecord {
	seedText := strconv.FormatInt(seed, 10)
	directory := seedDirectory(c.configuration, seedText)
	record := runRecord{Seed: seed, Device: device, StartedAt: time.Now().UTC()}

	if err := os.MkdirAll(directory, 0o755); err != nil {
		record.ExitCode = -1
		record.LaunchError = err.Error()
		return record
	}
	logFile, err := os.Create(filepath.Join(directory, "sanderling.log"))
	if err != nil {
		record.ExitCode = -1
		record.LaunchError = err.Error()
		return record
	}
	defer logFile.Close()

	runCtx, cancelRun := context.WithTimeout(ctx, c.configuration.runTimeout)
	defer cancelRun()
	monotonicStart := c.clocks.monotonicNow()
	wallClockStart := c.clocks.wallClockNow()
	exitCode, runErr := c.executor(runCtx, c.configuration.sanderlingPath, runArguments(c.configuration, seedText, device), logFile)
	if runCtx.Err() != nil && ctx.Err() == nil {
		record.TimedOut = true
	}
	record.MonotonicMillis = c.clocks.monotonicNow().Sub(monotonicStart).Milliseconds()
	record.WallClockMillis = c.clocks.wallClockNow().Sub(wallClockStart).Milliseconds()
	record.ExitCode = exitCode
	if runErr != nil {
		record.LaunchError = runErr.Error()
	}

	name, summary, err := summarizeRun(directory)
	if name != "" {
		record.RunDirectory = filepath.Join(filepath.Base(directory), name)
	}
	if err != nil {
		record.TraceError = err.Error()
		return record
	}
	record.traceSummary = summary
	return record
}

func (c *campaign) report(record runRecord) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if record.ExitCode != 0 {
		c.failures++
	} else if record.TraceError != "" {
		c.unreadable++
	}
	if err := json.NewEncoder(c.records).Encode(record); err != nil {
		fmt.Fprintf(c.stdout, "warning: seed %d record: %v\n", record.Seed, err)
	}
	fmt.Fprintf(c.stdout, "seed=%d device=%q outcome=%s steps=%d exit=%d monotonic=%s wall_clock=%s\n",
		record.Seed, record.Device, outcome(record), record.Steps, record.ExitCode,
		time.Duration(record.MonotonicMillis)*time.Millisecond,
		time.Duration(record.WallClockMillis)*time.Millisecond)
}

func outcome(record runRecord) string {
	switch {
	case record.TimedOut:
		return "timed out"
	case record.ExitCode != 0:
		return "failed"
	case record.FirstViolationOriginStep != nil:
		return fmt.Sprintf("violation@%d(%s)", *record.FirstViolationOriginStep,
			strings.Join(record.FirstViolationProperties, ","))
	default:
		return "clean"
	}
}
