//go:build withcompanion

package companionassets

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedNonZero(t *testing.T) {
	if EmbeddedSize() == 0 {
		t.Errorf("expected embedded companion archive to be non-empty")
	}
	if IsPlaceholder() {
		t.Errorf("withcompanion build should not be a placeholder")
	}
}

func TestEmbeddedSHA256Matches(t *testing.T) {
	sum := sha256.Sum256(embeddedArchive)
	if hex.EncodeToString(sum[:]) != EmbeddedSHA256() {
		t.Errorf("EmbeddedSHA256 does not match a fresh hash of the archive")
	}
}

func TestExtract_WritesBinaryAndChecksum(t *testing.T) {
	directory := t.TempDir()
	path, err := Extract(directory)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := filepath.Abs(filepath.Join(directory, "bin", companionBinaryName))
	if err != nil {
		t.Fatal(err)
	}
	if path != expected {
		t.Errorf("unexpected binary path: got %s want %s", path, expected)
	}
	checksum, err := os.ReadFile(filepath.Join(directory, "companion.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	if string(checksum) != EmbeddedSHA256() {
		t.Errorf("checksum file content wrong: %q", checksum)
	}
}

func TestExtract_LayoutAndSymlinks(t *testing.T) {
	directory := t.TempDir()
	binaryPath, err := Extract(directory)
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatalf("companion binary missing: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("companion binary is not executable, mode=%v", info.Mode())
	}

	frameworksDir := filepath.Join(directory, "Frameworks")
	if stat, err := os.Stat(frameworksDir); err != nil || !stat.IsDir() {
		t.Fatalf("Frameworks directory missing: %v", err)
	}

	expectedFrameworks := []string{
		"FBControlCore.framework",
		"FBDeviceControl.framework",
		"FBSimulatorControl.framework",
		"IDBCompanionUtilities.framework",
		"IDBGRPCSwift.framework",
		"XCTestBootstrap.framework",
	}
	for _, name := range expectedFrameworks {
		if stat, err := os.Stat(filepath.Join(frameworksDir, name)); err != nil || !stat.IsDir() {
			t.Errorf("expected framework %s missing: %v", name, err)
		}
	}

	// The Versions/Current symlink must resolve to a real directory, proving
	// symlinks survived extraction.
	current := filepath.Join(frameworksDir, "FBControlCore.framework", "Versions", "Current")
	linkInfo, err := os.Lstat(current)
	if err != nil {
		t.Fatalf("Versions/Current missing: %v", err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Errorf("Versions/Current is not a symlink")
	}
	resolved, err := os.Stat(current)
	if err != nil || !resolved.IsDir() {
		t.Errorf("Versions/Current does not resolve to a directory: %v", err)
	}

	// A runtime dylib that must be preserved inside FBControlCore Resources.
	dylib := filepath.Join(frameworksDir, "FBControlCore.framework", "Versions", "A", "Resources", "libMaculator.dylib")
	if _, err := os.Stat(dylib); err != nil {
		t.Errorf("expected runtime dylib preserved: %v", err)
	}
}

func TestExtract_ReusesWhenChecksumMatches(t *testing.T) {
	directory := t.TempDir()
	binaryPath, err := Extract(directory)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := []byte("SENTINEL-do-not-rewrite")
	if err := os.WriteFile(binaryPath, sentinel, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Extract(directory); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(sentinel) {
		t.Errorf("second extract rewrote the binary; reuse branch should have skipped extraction")
	}
}

func TestExtract_RewritesIfChecksumMissing(t *testing.T) {
	directory := t.TempDir()
	if _, err := Extract(directory); err != nil {
		t.Fatal(err)
	}
	checksumPath := filepath.Join(directory, "companion.sha256")
	if err := os.Remove(checksumPath); err != nil {
		t.Fatal(err)
	}
	if _, err := Extract(directory); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(checksumPath); err != nil {
		t.Errorf("checksum should have been rewritten: %v", err)
	}
}
