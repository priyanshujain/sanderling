package transport

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestIntegrationAccessibilityInfo spawns a local companion against a booted
// simulator and exercises a real AccessibilityInfo call. It is gated behind
// SANDERLING_IOS_INTEGRATION so it never runs by default.
func TestIntegrationAccessibilityInfo(t *testing.T) {
	if os.Getenv("SANDERLING_IOS_INTEGRATION") == "" {
		t.Skip("set SANDERLING_IOS_INTEGRATION=1 to run the integration smoke test")
	}

	udid := bootedSimulatorUDID(t)
	port := freePort(t)

	companionBinary := "/opt/homebrew/bin/idb_companion"
	command := exec.Command(companionBinary, "--udid", udid, "--grpc-port", strconv.Itoa(port))
	if err := command.Start(); err != nil {
		t.Fatalf("start companion: %v", err)
	}
	defer func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	}()

	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	waitForListener(t, address)

	companion, err := Dial(address)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer companion.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	info, err := companion.AccessibilityInfo(ctx)
	if err != nil {
		t.Fatalf("AccessibilityInfo: %v", err)
	}
	if strings.TrimSpace(info) == "" {
		t.Fatal("AccessibilityInfo returned empty json")
	}
}

func bootedSimulatorUDID(t *testing.T) string {
	t.Helper()
	output, err := exec.Command("xcrun", "simctl", "list", "devices", "booted", "--json").Output()
	if err != nil {
		t.Fatalf("simctl list: %v", err)
	}
	var parsed struct {
		Devices map[string][]struct {
			UDID  string `json:"udid"`
			State string `json:"state"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(output, &parsed); err != nil {
		t.Fatalf("parse simctl json: %v", err)
	}
	for _, devices := range parsed.Devices {
		for _, device := range devices {
			if device.State == "Booted" {
				return device.UDID
			}
		}
	}
	t.Skip("no booted simulator available")
	return ""
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func waitForListener(t *testing.T, address string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, time.Second)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("companion did not listen on %s", address)
}
