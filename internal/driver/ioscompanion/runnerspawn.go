package ioscompanion

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/priyanshujain/sanderling/internal/driver/ioscompanion/runnerassets"
)

// runnerPortPlaceholder is the marker the prepare script plants in the
// packaged test configuration's environment. The spawner substitutes the
// session's port before launching.
const runnerPortPlaceholder = "__COMPANION_PORT__"

// realSpawnRunner extracts the embedded runner bundle, writes a test
// configuration bound to the session's port, and starts the hosting test
// session against the configured simulator. Cancel sends SIGTERM so the
// session tears down its in-simulator children cleanly.
func (d *Driver) realSpawnRunner(ctx context.Context, address string) (*exec.Cmd, error) {
	extractDirectory := filepath.Join(os.TempDir(), "sanderling-runner")
	testRunPath, err := runnerassets.Extract(extractDirectory)
	if err != nil {
		return nil, fmt.Errorf("extract runner: %w", err)
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	boundTestRunPath, err := bindTestRunPort(testRunPath, port)
	if err != nil {
		return nil, err
	}

	logPath := filepath.Join(extractDirectory, "runner-session-"+port+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("create runner log: %w", err)
	}

	command := exec.CommandContext(ctx, "xcrun", "xcodebuild", "test-without-building",
		"-xctestrun", boundTestRunPath,
		"-destination", "platform=iOS Simulator,id="+d.udid)
	// The session log stays out of the run output: the build tool is noisy
	// and would pollute the run's error scan. The path is reported once so a
	// failed startup is diagnosable.
	command.Stdout = logFile
	command.Stderr = logFile
	command.Env = []string{
		"HOME=" + os.Getenv("HOME"),
		"PATH=/usr/bin:/bin",
		"TMPDIR=" + os.TempDir(),
	}
	command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
	command.WaitDelay = shutdownGrace
	startErr := command.Start()
	// The child holds its own descriptor after Start, so the parent's copy
	// closes either way.
	logFile.Close()
	if startErr != nil {
		return nil, fmt.Errorf("start runner session: %w", startErr)
	}
	fmt.Fprintf(d.output, "runner session pid=%d listening on %s (log: %s)\n", command.Process.Pid, address, logPath)
	return command, nil
}

// bindTestRunPort writes a copy of the extracted test configuration with the
// port placeholder substituted, next to the original so its relative paths
// keep resolving. Returns the bound copy's path.
func bindTestRunPort(testRunPath, port string) (string, error) {
	configuration, err := os.ReadFile(testRunPath)
	if err != nil {
		return "", fmt.Errorf("read test configuration: %w", err)
	}
	if !bytes.Contains(configuration, []byte(runnerPortPlaceholder)) {
		return "", fmt.Errorf("test configuration %s carries no %s placeholder", testRunPath, runnerPortPlaceholder)
	}
	bound := bytes.ReplaceAll(configuration, []byte(runnerPortPlaceholder), []byte(port))
	boundPath := filepath.Join(filepath.Dir(testRunPath), "runner-"+port+".xctestrun")
	if err := os.WriteFile(boundPath, bound, 0o644); err != nil {
		return "", fmt.Errorf("write bound test configuration: %w", err)
	}
	return boundPath, nil
}
