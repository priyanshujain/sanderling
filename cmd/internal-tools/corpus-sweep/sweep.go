package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// resolveBinaries turns campaign and sanderling into absolute paths before
// anything is served. Each campaign runs from the sweep's own directory, and a
// binary that is missing altogether has to stop the sweep here rather than fail
// once per implementation and seed. Every one that is missing is named
// together, in flag order: stopping at the first turns that single stop into
// one rerun per missing binary.
func resolveBinaries(configuration *config) error {
	var missing []error
	for _, binary := range []struct {
		name  string
		value *string
	}{
		{"--campaign", &configuration.campaignPath},
		{"--sanderling", &configuration.sanderlingPath},
	} {
		resolved, err := exec.LookPath(*binary.value)
		if err != nil {
			missing = append(missing, fmt.Errorf("%s: %w", binary.name, err))
			continue
		}
		absolute, err := filepath.Abs(resolved)
		if err != nil {
			missing = append(missing, fmt.Errorf("%s: %w", binary.name, err))
			continue
		}
		*binary.value = absolute
	}
	return errors.Join(missing...)
}

type sweep struct {
	configuration config
	stdout        io.Writer
	records       io.Writer
	servers       map[string]*staticServer
	mutex         sync.Mutex
	stalled       int
	failedRuns    int
	totalRuns     int
}

func runSweep(
	ctx context.Context,
	configuration config,
	stdout io.Writer,
) error {
	if _, err := os.Stat(filepath.Join(configuration.outputDirectory, manifestFileName)); err == nil {
		return fmt.Errorf(
			"%s already exists in %s: pick a fresh --output so two sweeps do not share a directory",
			manifestFileName,
			configuration.outputDirectory,
		)
	}
	if err := verifyCorpus(configuration.corpusRoot); err != nil {
		return err
	}
	implementations, err := planImplementations(
		configuration.corpusRoot,
		configuration.implementations,
		configuration.basePort,
	)
	if err != nil {
		return err
	}
	if err := resolveBinaries(&configuration); err != nil {
		return err
	}
	if _, err := os.Stat(configuration.specPath); err != nil {
		return fmt.Errorf("--spec: %w", err)
	}
	if err := os.MkdirAll(configuration.outputDirectory, 0o755); err != nil {
		return fmt.Errorf("create sweep dir: %w", err)
	}
	host, _ := os.Hostname()
	if err := writeManifest(configuration.outputDirectory, buildManifest(configuration, implementations, host, time.Now().UTC())); err != nil {
		return fmt.Errorf("write %s: %w", manifestFileName, err)
	}

	recordsFile, err := os.OpenFile(
		filepath.Join(configuration.outputDirectory, recordsFileName),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0o644,
	)
	if err != nil {
		return fmt.Errorf("open %s: %w", recordsFileName, err)
	}
	defer recordsFile.Close()

	running := &sweep{
		configuration: configuration,
		stdout:        stdout,
		records:       recordsFile,
	}
	// Every port is bound before any run starts. A port already taken means
	// that implementation cannot have an origin of its own, and continuing
	// without one is what the separate origins are there to prevent.
	if err := running.serveAll(implementations); err != nil {
		return err
	}
	defer running.stopAll()

	fmt.Fprintf(
		stdout,
		"sweep: %d implementations, %d seeds each, %d at a time, %s\n",
		len(
			implementations,
		),
		len(configuration.seeds),
		configuration.concurrency,
		configuration.outputDirectory,
	)
	running.work(ctx, implementations)

	fmt.Fprintf(
		stdout,
		"sweep complete: %d of %d implementations never ran, %d of %d campaigns failed\n",
		running.stalled,
		len(implementations),
		running.failedRuns,
		running.totalRuns,
	)
	if running.stalled > 0 || running.failedRuns > 0 {
		return fmt.Errorf(
			"%d of %d implementations never ran and %d of %d campaigns failed; see %s",
			running.stalled,
			len(implementations),
			running.failedRuns,
			running.totalRuns,
			recordsFileName,
		)
	}
	return nil
}

func (s *sweep) serveAll(implementations []implementation) error {
	s.servers = make(map[string]*staticServer, len(implementations))
	for _, target := range implementations {
		server, err := startStaticServer(s.configuration.corpusRoot, target)
		if err != nil {
			s.stopAll()
			return err
		}
		s.servers[target.Name] = server
	}
	return nil
}

func (s *sweep) stopAll() {
	for _, server := range s.servers {
		server.stop()
	}
}

func (s *sweep) work(ctx context.Context, implementations []implementation) {
	queue := make(chan implementation, len(implementations))
	for _, target := range implementations {
		queue <- target
	}
	close(queue)

	workers := min(s.configuration.concurrency, len(implementations))
	var waitGroup sync.WaitGroup
	for range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for target := range queue {
				if ctx.Err() != nil {
					return
				}
				s.report(s.runImplementation(ctx, target))
			}
		}()
	}
	waitGroup.Wait()
}

// runImplementation carries one implementation through all of its seeds. A
// document that does not answer is recorded and the sweep moves on: one
// implementation must not cost the other forty-two their runs.
func (s *sweep) runImplementation(
	ctx context.Context,
	target implementation,
) (record implementationRecord) {
	record = implementationRecord{
		Name:      target.Name,
		Document:  target.Document,
		Port:      target.Port,
		Origin:    target.Origin(),
		URL:       target.URL(),
		StartedAt: time.Now().UTC(),
	}
	started := time.Now()
	defer func() { record.MonotonicMillis = time.Since(started).Milliseconds() }()

	if err := os.MkdirAll(filepath.Join(s.configuration.outputDirectory, target.Name), 0o755); err != nil {
		record.FailedStage = stageServe
		record.Error = err.Error()
		return record
	}
	if err := s.servers[target.Name].waitReady(ctx); err != nil {
		record.FailedStage = stageServe
		record.Error = err.Error()
		return record
	}

	for _, seed := range s.configuration.seeds {
		if ctx.Err() != nil {
			return record
		}
		record.Runs = append(record.Runs, s.runSeed(ctx, target, seed))
	}
	return record
}

func (s *sweep) runSeed(
	ctx context.Context,
	target implementation,
	seed int64,
) (record runRecord) {
	seedText := strconv.FormatInt(seed, 10)
	directory := campaignDirectory(s.configuration, target, seedText)
	record = runRecord{Seed: seed, CampaignDirectory: directory}
	started := time.Now()
	defer func() { record.MonotonicMillis = time.Since(started).Milliseconds() }()

	if err := os.MkdirAll(directory, 0o755); err != nil {
		record.ExitCode = -1
		record.LaunchError = err.Error()
		return record
	}
	exitCode, err := runCommand(
		ctx,
		s.configuration.campaignPath,
		campaignArguments(
			s.configuration,
			target,
			seedText,
		),
		filepath.Join(directory, "campaign.log"),
	)
	record.ExitCode = exitCode
	if err != nil {
		record.LaunchError = err.Error()
	}
	return record
}

func runCommand(
	ctx context.Context,
	binary string,
	arguments []string,
	logPath string,
) (int, error) {
	logFile, err := os.Create(logPath)
	if err != nil {
		return -1, err
	}
	defer logFile.Close()
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Stdout = logFile
	command.Stderr = logFile
	err = command.Run()
	if err == nil {
		return 0, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode(), nil
	}
	return -1, err
}

func (s *sweep) report(record implementationRecord) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if record.FailedStage != "" {
		s.stalled++
	}
	s.totalRuns += len(record.Runs)
	failed := 0
	for _, run := range record.Runs {
		if run.ExitCode != 0 {
			failed++
		}
	}
	s.failedRuns += failed
	if err := json.NewEncoder(s.records).Encode(record); err != nil {
		fmt.Fprintf(s.stdout, "warning: %s record: %v\n", record.Name, err)
	}
	elapsed := time.Duration(record.MonotonicMillis) * time.Millisecond
	if record.FailedStage != "" {
		fmt.Fprintf(s.stdout, "%s port=%d failed at %s: %s (%s)\n",
			record.Name, record.Port, record.FailedStage, record.Error, elapsed)
		return
	}
	fmt.Fprintf(s.stdout, "%s origin=%s campaigns=%d failed=%d elapsed=%s\n",
		record.Name, record.Origin, len(record.Runs), failed, elapsed)
}
