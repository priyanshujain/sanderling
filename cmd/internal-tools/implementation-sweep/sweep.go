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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// implementation is one directory under --implementations, with the port it
// owns for the whole sweep. The port comes from the implementation's position
// in name order rather than from a pool, so the manifest can name the URL every
// run was served from before anything has been served.
type implementation struct {
	Name      string
	Directory string
	Port      int
}

const implementationPrefix = "impl-"

func discoverImplementations(
	directory string,
	basePort int,
) ([]implementation, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	var found []implementation
	for _, entry := range entries {
		if !entry.IsDir() ||
			!strings.HasPrefix(entry.Name(), implementationPrefix) {
			continue
		}
		found = append(found, implementation{
			Name:      entry.Name(),
			Directory: filepath.Join(directory, entry.Name()),
		})
	}
	if len(found) == 0 {
		return nil, fmt.Errorf(
			"no %s* directories in %s",
			implementationPrefix,
			directory,
		)
	}
	sort.Slice(
		found,
		func(i, j int) bool { return found[i].Name < found[j].Name },
	)
	if basePort+len(found)-1 > 65535 {
		return nil, fmt.Errorf(
			"--base-port %d leaves no room for %d implementations",
			basePort,
			len(found),
		)
	}
	for index := range found {
		found[index].Port = basePort + index
	}
	return found, nil
}

// resolveBinaries turns bun, campaign and sanderling into absolute paths before
// anything is installed. Each campaign runs from the sweep's own directory
// rather than the implementation's, so a relative --sanderling would otherwise
// resolve against the wrong one, and a binary that is missing altogether has to
// stop the sweep here rather than fail once per implementation and seed.
func resolveBinaries(configuration *config) error {
	for name, value := range map[string]*string{
		"--bun":        &configuration.bunPath,
		"--campaign":   &configuration.campaignPath,
		"--sanderling": &configuration.sanderlingPath,
	} {
		resolved, err := exec.LookPath(*value)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		absolute, err := filepath.Abs(resolved)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		*value = absolute
	}
	return nil
}

type sweep struct {
	configuration config
	stdout        io.Writer
	records       io.Writer
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
	implementations, err := discoverImplementations(
		configuration.implementationsDirectory,
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

// runImplementation carries one implementation from install to its last seed.
// Every failure it can meet is returned in the record: one implementation that
// cannot install, build or serve must not cost the other twenty-three their
// runs.
func (s *sweep) runImplementation(
	ctx context.Context,
	target implementation,
) (record implementationRecord) {
	record = implementationRecord{
		Name:      target.Name,
		Directory: target.Directory,
		Port:      target.Port,
		StartedAt: time.Now().UTC(),
	}
	started := time.Now()
	defer func() { record.MonotonicMillis = time.Since(started).Milliseconds() }()

	directory := filepath.Join(s.configuration.outputDirectory, target.Name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		record.FailedStage = stageInstall
		record.Error = err.Error()
		return record
	}
	for _, step := range []struct {
		stage     string
		arguments []string
	}{
		{stageInstall, []string{"install"}},
		{stageBuild, []string{"run", "build"}},
	} {
		logPath := filepath.Join(directory, step.stage+".log")
		exitCode, err := runCommand(
			ctx,
			target.Directory,
			s.configuration.bunPath,
			step.arguments,
			logPath,
		)
		if err != nil {
			record.FailedStage = step.stage
			record.Error = err.Error()
			return record
		}
		if exitCode != 0 {
			record.FailedStage = step.stage
			record.Error = fmt.Sprintf(
				"bun %s exited %d, see %s",
				strings.Join(step.arguments, " "),
				exitCode,
				logPath,
			)
			return record
		}
	}

	running, err := startServer(
		ctx,
		s.configuration,
		target,
		filepath.Join(directory, "serve.log"),
	)
	if err != nil {
		record.FailedStage = stageServe
		record.Error = err.Error()
		return record
	}
	defer running.stop()
	if err := running.waitReady(ctx, readinessURL(target.Port)); err != nil {
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
	record = runRecord{
		Seed:              seed,
		URL:               servedURL(target.Port, seedText),
		CampaignDirectory: directory,
	}
	started := time.Now()
	defer func() { record.MonotonicMillis = time.Since(started).Milliseconds() }()

	if err := os.MkdirAll(directory, 0o755); err != nil {
		record.ExitCode = -1
		record.LaunchError = err.Error()
		return record
	}
	exitCode, err := runCommand(
		ctx,
		"",
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

// runCommand runs one step of the pipeline with its output in logPath. An
// empty directory keeps the sweep's own working directory, which is what the
// campaign tool gets: it has no reason to run inside an implementation.
func runCommand(
	ctx context.Context,
	directory, binary string,
	arguments []string,
	logPath string,
) (int, error) {
	logFile, err := os.Create(logPath)
	if err != nil {
		return -1, err
	}
	defer logFile.Close()
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Dir = directory
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
	for _, run := range record.Runs {
		if run.ExitCode != 0 {
			s.failedRuns++
		}
	}
	if err := json.NewEncoder(s.records).Encode(record); err != nil {
		fmt.Fprintf(s.stdout, "warning: %s record: %v\n", record.Name, err)
	}
	elapsed := time.Duration(record.MonotonicMillis) * time.Millisecond
	if record.FailedStage != "" {
		fmt.Fprintf(s.stdout, "%s port=%d failed at %s: %s (%s)\n",
			record.Name, record.Port, record.FailedStage, record.Error, elapsed)
		return
	}
	failed := 0
	for _, run := range record.Runs {
		if run.ExitCode != 0 {
			failed++
		}
	}
	fmt.Fprintf(s.stdout, "%s port=%d campaigns=%d failed=%d elapsed=%s\n",
		record.Name, record.Port, len(record.Runs), failed, elapsed)
}
