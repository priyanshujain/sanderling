package main

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/priyanshujain/uatu/internal/agent"
	"github.com/priyanshujain/uatu/internal/bundler"
	"github.com/priyanshujain/uatu/internal/driver/maestro"
	"github.com/priyanshujain/uatu/internal/runner"
	"github.com/priyanshujain/uatu/internal/sidecar"
	"github.com/priyanshujain/uatu/internal/trace"
	"github.com/priyanshujain/uatu/internal/verifier"
)

const (
	socketName            = "uatu-agent"
	sidecarStartupTimeout = 30 * time.Second
	sdkAcceptTimeout      = 60 * time.Second
)

func runTestPipeline(ctx context.Context, options testOptions, stdout io.Writer) error {
	aliases := map[string]string{}
	if specApiPath := resolveSpecAPIPath(options.spec); specApiPath != "" {
		aliases["@uatu/spec"] = specApiPath
	}
	bundle, err := bundler.Bundle(bundler.Options{
		EntryFile: options.spec,
		Defines: map[string]string{
			"UATU_TEST_PHONE": os.Getenv("UATU_TEST_PHONE"),
			"UATU_TEST_OTP":   os.Getenv("UATU_TEST_OTP"),
		},
		Aliases: aliases,
	})
	if err != nil {
		return fmt.Errorf("bundle spec: %w", err)
	}
	fmt.Fprintf(stdout, "bundled spec: %d bytes (sha256=%s)\n", len(bundle.JavaScript), bundle.SHA256[:12])

	sidecarDirectory := filepath.Join(os.TempDir(), "uatu-sidecar")
	jarPath, err := sidecar.Extract(sidecarDirectory)
	if err != nil {
		return fmt.Errorf("extract sidecar: %w", err)
	}
	fmt.Fprintf(stdout, "sidecar JAR: %s (size=%d)\n", jarPath, sidecar.EmbeddedSize())

	sidecarPort, err := pickFreePort()
	if err != nil {
		return err
	}
	sidecarCommand := exec.CommandContext(ctx, "java", "-jar", jarPath,
		"--port", strconv.Itoa(sidecarPort),
		"--platform", options.platform,
	)
	sidecarCommand.Stdout = stdout
	sidecarCommand.Stderr = stdout
	if err := sidecarCommand.Start(); err != nil {
		return fmt.Errorf("spawn sidecar: %w", err)
	}
	defer func() {
		if sidecarCommand.Process != nil {
			_ = sidecarCommand.Process.Kill()
		}
	}()
	fmt.Fprintf(stdout, "sidecar pid=%d listening on 127.0.0.1:%d\n", sidecarCommand.Process.Pid, sidecarPort)

	driverClient, err := maestro.Dial(fmt.Sprintf("127.0.0.1:%d", sidecarPort))
	if err != nil {
		return fmt.Errorf("dial sidecar: %w", err)
	}
	defer driverClient.Close()
	healthCtx, healthCancel := context.WithTimeout(ctx, sidecarStartupTimeout)
	if err := driverClient.WaitForHealth(healthCtx, 250*time.Millisecond); err != nil {
		healthCancel()
		return fmt.Errorf("sidecar health check: %w", err)
	}
	healthCancel()
	fmt.Fprintln(stdout, "sidecar is healthy")

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("agent listener: %w", err)
	}
	defer listener.Close()
	agentPort := listener.Addr().(*net.TCPAddr).Port

	if err := adbReverse(socketName, agentPort); err != nil {
		return fmt.Errorf("adb reverse: %w", err)
	}
	defer func() {
		if err := adbReverseRemove(socketName); err != nil {
			fmt.Fprintf(stdout, "warning: adb reverse cleanup: %v\n", err)
		}
	}()
	fmt.Fprintf(stdout, "forwarded localabstract:%s -> tcp:%d\n", socketName, agentPort)

	agentServer := agent.NewServer(listener)

	type acceptResult struct {
		connection *agent.Conn
		err        error
	}
	acceptChannel := make(chan acceptResult, 1)
	go func() {
		acceptCtx, cancel := context.WithTimeout(ctx, sdkAcceptTimeout)
		defer cancel()
		connection, acceptErr := agentServer.Accept(acceptCtx)
		acceptChannel <- acceptResult{connection: connection, err: acceptErr}
	}()

	if err := driverClient.Launch(ctx, options.bundleID, options.launcherActivity, false); err != nil {
		return fmt.Errorf("launch app: %w", err)
	}
	fmt.Fprintf(stdout, "launched %s; waiting for SDK to connect (%.0fs timeout)\n", options.bundleID, sdkAcceptTimeout.Seconds())

	result := <-acceptChannel
	if result.err != nil {
		return fmt.Errorf("accept SDK: %w", result.err)
	}
	connection := result.connection
	defer connection.Close()
	hello := connection.Hello()
	fmt.Fprintf(stdout, "SDK connected: platform=%s app=%s sdk=%s\n", hello.Platform, hello.AppPackage, hello.Version)

	seed := options.seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	verifierInstance, err := verifier.New(verifier.WithRand(rand.New(rand.NewPCG(uint64(seed), 0))))
	if err != nil {
		return fmt.Errorf("verifier: %w", err)
	}
	if err := verifierInstance.Load(string(bundle.JavaScript)); err != nil {
		return fmt.Errorf("load spec: %w", err)
	}
	fmt.Fprintln(stdout, "spec loaded into verifier")

	runDirectory := filepath.Join(options.output, time.Now().UTC().Format("20060102-150405"))
	traceWriter, err := trace.NewWriter(runDirectory)
	if err != nil {
		return fmt.Errorf("trace writer: %w", err)
	}
	defer traceWriter.Close()
	if err := traceWriter.WriteMeta(trace.Meta{
		Seed:         seed,
		SpecPath:     options.spec,
		BundleSHA256: bundle.SHA256,
		Platform:     options.platform,
		BundleID:     options.bundleID,
		StartedAt:    time.Now().UTC(),
		UatuVersion:  "0.0.1",
	}); err != nil {
		return fmt.Errorf("trace meta: %w", err)
	}
	fmt.Fprintf(stdout, "trace dir: %s\n", runDirectory)

	fmt.Fprintf(stdout, "running for %s (seed=%d)\n", options.duration, seed)
	summary, err := runner.Run(ctx, runner.Options{
		Duration:        options.duration,
		SnapshotTimeout: 5 * time.Second,
		IdleTimeout:     1 * time.Second,
		Connection:      connection,
		Driver:          driverClient,
		Verifier:        verifierInstance,
		TraceWriter:     traceWriter,
	})

	terminateCtx, terminateCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = driverClient.Terminate(terminateCtx)
	terminateCancel()

	if err != nil {
		return fmt.Errorf("runner: %w", err)
	}

	fmt.Fprintf(stdout, "\nrun complete: %d steps in %s\n", summary.Steps, summary.EndTime.Sub(summary.StartTime).Round(time.Millisecond))
	if len(summary.Violations) == 0 {
		fmt.Fprintln(stdout, "no violations.")
	} else {
		fmt.Fprintf(stdout, "%d violation record(s):\n", len(summary.Violations))
		for _, violation := range summary.Violations {
			fmt.Fprintf(stdout, "  step %d: %v\n", violation.StepIndex, violation.Properties)
		}
	}
	return nil
}

// resolveSpecAPIPath returns the path to pkg/spec-api/src/index.ts inside
// a uatu source checkout, searched upward from the spec file and the cwd.
// Returns "" when not found, in which case esbuild resolves @uatu/spec via
// node_modules the way a downstream user's project would.
func resolveSpecAPIPath(specPath string) string {
	candidates := []string{}
	if absoluteSpec, err := filepath.Abs(specPath); err == nil {
		directory := filepath.Dir(absoluteSpec)
		for {
			candidates = append(candidates, filepath.Join(directory, "pkg/spec-api/src/index.ts"))
			parent := filepath.Dir(directory)
			if parent == directory {
				break
			}
			directory = parent
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "pkg/spec-api/src/index.ts"))
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func pickFreePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func adbReverse(socket string, port int) error {
	command := exec.Command("adb", "reverse", "localabstract:"+socket, fmt.Sprintf("tcp:%d", port))
	return command.Run()
}

func adbReverseRemove(socket string) error {
	return exec.Command("adb", "reverse", "--remove", "localabstract:"+socket).Run()
}
