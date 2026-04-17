package sidecar

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedNonZero(t *testing.T) {
	if EmbeddedSize() == 0 {
		t.Errorf("expected embedded JAR to be non-empty")
	}
}

func TestExtract_WritesJARAndChecksum(t *testing.T) {
	directory := t.TempDir()
	path, err := Extract(directory)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(directory, "uatu-sidecar.jar") {
		t.Errorf("unexpected path: %s", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if EmbeddedSize() != len(body) {
		t.Errorf("size mismatch: embedded=%d, written=%d", EmbeddedSize(), len(body))
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != EmbeddedSHA256() {
		t.Errorf("hash mismatch")
	}
	checksum, err := os.ReadFile(path + ".sha256")
	if err != nil {
		t.Fatal(err)
	}
	if string(checksum) != EmbeddedSHA256() {
		t.Errorf("checksum file content wrong: %q", checksum)
	}
}

func TestExtract_ReusesIdenticalFile(t *testing.T) {
	directory := t.TempDir()
	path, err := Extract(directory)
	if err != nil {
		t.Fatal(err)
	}
	originalStat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	originalModTime := originalStat.ModTime()

	// Second extract should be a no-op (no rewrite).
	if _, err := Extract(directory); err != nil {
		t.Fatal(err)
	}
	secondStat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !secondStat.ModTime().Equal(originalModTime) {
		t.Errorf("second extract should not have rewritten the file")
	}
}

func TestExtract_RewritesIfChecksumMissing(t *testing.T) {
	directory := t.TempDir()
	path, err := Extract(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path + ".sha256"); err != nil {
		t.Fatal(err)
	}
	if _, err := Extract(directory); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".sha256"); err != nil {
		t.Errorf("checksum should have been written: %v", err)
	}
}

func TestIsPlaceholder_FlagsDevBuilds(t *testing.T) {
	// The repo ships with a placeholder so fresh clones build without
	// needing `make sidecar` first. The flag lets `uatu test` warn loudly
	// before trying to drive a real device.
	if !IsPlaceholder() {
		t.Logf("running against a real sidecar JAR (size=%d)", EmbeddedSize())
	}
}
