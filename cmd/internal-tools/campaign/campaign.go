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
	"strconv"
	"strings"
	"sync"
	"time"
)

// commandExecutor runs one sanderling invocation and returns its exit code.
// A non-nil error means the process could not be run at all, which is a
// different failure from a run that started and exited non-zero.
type commandExecutor func(ctx context.Context, binary string, arguments []string, output io.Writer) (int, error)

func executeCommand(ctx context.Context, binary string, arguments []string, output io.Writer) (int, error) {
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	if err == nil {
		return 0, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode(), nil
	}
	return -1, err
}

// runRecord is one line of runs.jsonl.
type runRecord struct {
	Seed           int64     `json:"seed"`
	Device         string    `json:"device,omitempty"`
	ExitCode       int       `json:"exit_code"`
	LaunchError    string    `json:"launch_error,omitempty"`
	StartedAt      time.Time `json:"started_at"`
	DurationMillis int64     `json:"duration_millis"`
	RunDirectory   string    `json:"run_directory,omitempty"`
	TraceError     string    `json:"trace_error,omitempty"`
	traceSummary
}

type campaign struct {
	configuration config
	executor      commandExecutor
	stdout        io.Writer
	records       io.Writer
	mutex         sync.Mutex
	failures      int
	unreadable    int
}

func runCampaign(ctx context.Context, configuration config, executor commandExecutor, stdout io.Writer) error {
	if _, err := os.Stat(filepath.Join(configuration.outputDirectory, manifestFileName)); err == nil {
		return fmt.Errorf("%s already exists in %s: pick a fresh --output so two campaigns do not share a directory",
			manifestFileName, configuration.outputDirectory)
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
	if err := writeManifest(configuration.outputDirectory, buildManifest(configuration, host, binaryPath, version, time.Now().UTC())); err != nil {
		return fmt.Errorf("write %s: %w", manifestFileName, err)
	}

	recordsFile, err := os.OpenFile(filepath.Join(configuration.outputDirectory, recordsFileName),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", recordsFileName, err)
	}
	defer recordsFile.Close()

	sweep := &campaign{configuration: configuration, executor: executor, stdout: stdout, records: recordsFile}
	fmt.Fprintf(stdout, "campaign %s: %d seeds, %d worker(s), %s\n",
		configuration.arm, len(configuration.seeds), len(workerDevices(configuration.devices)), configuration.outputDirectory)
	sweep.sweep(ctx)

	fmt.Fprintf(stdout, "campaign complete: %d of %d runs failed, %d produced an unreadable trace\n",
		sweep.failures, len(configuration.seeds), sweep.unreadable)
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
			for seed := range queue {
				if ctx.Err() != nil {
					return
				}
				c.report(c.runSeed(ctx, seed, device))
			}
		}(device)
	}
	waitGroup.Wait()
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

	start := time.Now()
	exitCode, runErr := c.executor(ctx, c.configuration.sanderlingPath, runArguments(c.configuration, seedText, device), logFile)
	record.DurationMillis = time.Since(start).Milliseconds()
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
	fmt.Fprintf(c.stdout, "seed=%d device=%q outcome=%s steps=%d exit=%d duration=%s\n",
		record.Seed, record.Device, outcome(record), record.Steps, record.ExitCode,
		time.Duration(record.DurationMillis)*time.Millisecond)
}

func outcome(record runRecord) string {
	switch {
	case record.ExitCode != 0:
		return "failed"
	case record.FirstViolationOriginStep != nil:
		return fmt.Sprintf("violation@%d(%s)", *record.FirstViolationOriginStep,
			strings.Join(record.FirstViolationProperties, ","))
	default:
		return "clean"
	}
}
