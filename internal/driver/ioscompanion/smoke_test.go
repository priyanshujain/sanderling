//go:build withcompanion

package ioscompanion

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestSmokeNewHierarchyScreenshot brings up the embedded companion against the
// booted simulator, reads one hierarchy and screenshot, closes, and asserts the
// companion child left no orphan behind. It is gated behind both the
// withcompanion build tag (so the binary is embedded) and an environment
// variable, so it never runs in the default suite.
func TestSmokeNewHierarchyScreenshot(t *testing.T) {
	if os.Getenv("SANDERLING_IOS_INTEGRATION") == "" {
		t.Skip("set SANDERLING_IOS_INTEGRATION=1 to run the companion smoke test")
	}
	udid := bootedUDID(t)

	before := companionProcessCount(t, udid)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	d, err := New(ctx, Options{UniqueDeviceIdentifier: udid, Output: os.Stderr})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := d.Hierarchy(ctx); err != nil {
		d.Close()
		t.Fatalf("Hierarchy: %v", err)
	}
	if _, err := d.Screenshot(ctx); err != nil {
		d.Close()
		t.Fatalf("Screenshot: %v", err)
	}
	d.Close()

	// Give the child a moment to exit after SIGTERM, then confirm no orphan.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if companionProcessCount(t, udid) <= before {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("companion process for %s outlived Close (orphan)", udid)
}

func bootedUDID(t *testing.T) string {
	t.Helper()
	output, err := exec.Command("xcrun", "simctl", "list", "devices", "booted").Output()
	if err != nil {
		t.Fatalf("simctl list: %v", err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		if start := strings.Index(line, "("); start >= 0 {
			if end := strings.Index(line[start:], ")"); end > 0 {
				candidate := line[start+1 : start+end]
				if len(candidate) == 36 {
					return candidate
				}
			}
		}
	}
	t.Skip("no booted simulator available")
	return ""
}

func companionProcessCount(t *testing.T, udid string) int {
	t.Helper()
	output, _ := exec.Command("pgrep", "-f", udid).Output()
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}
