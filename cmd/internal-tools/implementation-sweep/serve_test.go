package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// bunSpawningAServer serves from a child process and then waits, which is the
// shape of `bun run preview`: the port belongs to something below the process
// the sweep started.
const bunSpawningAServer = `#!/bin/sh
port=""
previous=""
for argument in "$@"; do
  if [ "$previous" = "--port" ]; then port="$argument"; fi
  previous="$argument"
done
SWEEP_TEST_SERVE_PORT="$port" "%[1]s" &
wait
`

func TestServerStop_TakesTheProcessBelowItWithTheServer(t *testing.T) {
	directory := t.TempDir()
	testBinary, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	bunPath := writeScript(
		t,
		filepath.Join(directory, "stub-bun"),
		fmt.Sprintf(bunSpawningAServer, testBinary),
	)
	port := freePortRange(t, 1)
	target := implementation{Name: "impl-01", Directory: directory, Port: port}

	// Background rather than the test context: only stop() may end this
	// server, or a leak would be hidden by the context being cancelled.
	running, err := startServer(
		context.Background(),
		config{bunPath: bunPath},
		target,
		filepath.Join(directory, "serve.log"),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(
		func() { syscall.Kill(-running.command.Process.Pid, syscall.SIGKILL) },
	)
	if err := running.waitReady(context.Background(), readinessURL(port)); err != nil {
		t.Fatal(err)
	}

	running.stop()

	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(readinessURL(port))
		if err != nil {
			return
		}
		response.Body.Close()
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf(
		"port %d is still served after stop(): the server below bun outlived the sweep and holds the port",
		port,
	)
}

func TestServerWaitReady_ReportsAServerThatExited(t *testing.T) {
	directory := t.TempDir()
	bunPath := writeScript(
		t,
		filepath.Join(directory, "stub-bun"),
		"#!/bin/sh\necho 'port is already in use' >&2\nexit 1\n",
	)
	port := freePortRange(t, 1)

	running, err := startServer(
		context.Background(),
		config{bunPath: bunPath},
		implementation{
			Name:      "impl-01",
			Directory: directory,
			Port:      port,
		},
		filepath.Join(directory, "serve.log"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer running.stop()

	started := time.Now()
	err = running.waitReady(context.Background(), readinessURL(port))
	if err == nil {
		t.Fatal(
			"a server that exited should not be waited on until the start timeout",
		)
	}
	if elapsed := time.Since(started); elapsed > 30*time.Second {
		t.Errorf("waited %s for a server that had already exited", elapsed)
	}
	log, err := os.ReadFile(filepath.Join(directory, "serve.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(log) == "" {
		t.Error("serve.log did not capture why the server exited")
	}
}
