package ioscompanion

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/driver/ioscompanion/transport"
)

// These tests cover the process-supervision logic (stopProcess, bringUp,
// respawnAndRedial) with real child processes and seamed transports, so the
// default suite exercises it without a simulator.

func TestStopProcessReapsChildOnSigterm(t *testing.T) {
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	start := time.Now()
	stopProcess(child)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("stopProcess took %v; SIGTERM on sleep should reap promptly", elapsed)
	}
	if child.ProcessState == nil {
		t.Fatal("child not reaped: ProcessState is nil")
	}
	if err := child.Process.Signal(syscall.Signal(0)); err == nil {
		t.Fatal("child still signalable after stopProcess")
	}
}

func TestStopProcessEscalatesToKillWhenSigtermIgnored(t *testing.T) {
	previousGrace := shutdownGrace
	shutdownGrace = 200 * time.Millisecond
	defer func() { shutdownGrace = previousGrace }()

	child := exec.Command("sh", "-c", `trap "" TERM; sleep 30`)
	if err := child.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	// Give the shell a beat to install its TERM trap.
	time.Sleep(100 * time.Millisecond)
	stopProcess(child)
	if child.ProcessState == nil {
		t.Fatal("child not reaped: ProcessState is nil")
	}
	if child.ProcessState.Success() {
		t.Fatal("child should have died by signal, not exited cleanly")
	}
}

func TestStopProcessHandlesNilChild(t *testing.T) {
	stopProcess(nil)
	stopProcess(&exec.Cmd{})
}

// TestRespawnAndRedialReplacesTransportAndChild drives the real restart path
// through the seams: the old transport must be closed, a fresh child spawned,
// and the fresh transport installed.
func TestRespawnAndRedialReplacesTransportAndChild(t *testing.T) {
	t.Setenv("SANDERLING_SIMULATOR_COMPANION", "legacy")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			_ = connection.Close()
		}
	}()

	spawns := 0
	dials := []*fakeCompanion{}
	options := Options{
		UniqueDeviceIdentifier: "FAKE-UDID",
		pickAddress:            func() (string, error) { return listener.Addr().String(), nil },
		spawnChild: func(context.Context, string) (*exec.Cmd, error) {
			spawns++
			return &exec.Cmd{}, nil
		},
		dialCompanion: func(string) (transport.Companion, error) {
			companion := &fakeCompanion{accessibilityJSON: "[]"}
			dials = append(dials, companion)
			return companion, nil
		},
	}
	d, err := New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	first := dials[0]
	if err := d.respawnAndRedial(context.Background()); err != nil {
		t.Fatalf("respawnAndRedial: %v", err)
	}
	if spawns != 2 {
		t.Fatalf("spawns = %d, want 2 (initial bring-up plus restart)", spawns)
	}
	if len(dials) != 2 || d.companion != transport.Companion(dials[1]) {
		t.Fatalf("restart must install the freshly dialed transport")
	}
	if indexOf(first.recorded(), "close") < 0 {
		t.Fatal("restart must close the dead transport")
	}
}

// TestBringUpStopsChildWhenDialFails proves a failed bring-up does not leak
// the child it spawned.
func TestBringUpStopsChildWhenDialFails(t *testing.T) {
	t.Setenv("SANDERLING_SIMULATOR_COMPANION", "legacy")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			_ = connection.Close()
		}
	}()

	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	options := Options{
		UniqueDeviceIdentifier: "FAKE-UDID",
		pickAddress:            func() (string, error) { return listener.Addr().String(), nil },
		spawnChild: func(context.Context, string) (*exec.Cmd, error) {
			return child, nil
		},
		dialCompanion: func(string) (transport.Companion, error) {
			return nil, errors.New("dial refused")
		},
	}
	if _, err := New(context.Background(), options); err == nil {
		t.Fatal("New should fail when dial fails")
	}
	if child.ProcessState == nil {
		t.Fatal("failed bring-up must reap the spawned child")
	}
}
