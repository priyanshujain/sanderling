//go:build withcompanion

package runnerassets

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedNonZero(t *testing.T) {
	if EmbeddedSize() == 0 {
		t.Errorf("expected embedded runner archive to be non-empty")
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

func TestExtract_WritesXctestrunAndChecksum(t *testing.T) {
	directory := t.TempDir()
	path, err := Extract(directory)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := filepath.Abs(filepath.Join(directory, xctestrunName))
	if err != nil {
		t.Fatal(err)
	}
	if path != expected {
		t.Errorf("unexpected xctestrun path: got %s want %s", path, expected)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "__COMPANION_PORT__") {
		t.Errorf("extracted xctestrun is missing the __COMPANION_PORT__ placeholder")
	}

	checksum, err := os.ReadFile(filepath.Join(directory, "runner.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	if string(checksum) != EmbeddedSHA256() {
		t.Errorf("checksum file content wrong: %q", checksum)
	}
}

func TestExtract_Layout(t *testing.T) {
	directory := t.TempDir()
	if _, err := Extract(directory); err != nil {
		t.Fatal(err)
	}

	runnerApp := filepath.Join(directory, "Debug-iphonesimulator", "CompanionRunnerUITests-Runner.app")
	if stat, err := os.Stat(runnerApp); err != nil || !stat.IsDir() {
		t.Fatalf("runner app directory missing: %v", err)
	}

	binary := filepath.Join(runnerApp, "CompanionRunnerUITests-Runner")
	if _, err := os.Stat(binary); err != nil {
		t.Errorf("runner app binary missing: %v", err)
	}
}

func TestExtract_SymlinksResolve(t *testing.T) {
	directory := t.TempDir()
	if _, err := Extract(directory); err != nil {
		t.Fatal(err)
	}

	// Simulator bundles are flat, so the runner app may contain no symlinks at
	// all. Any symlink that is present must resolve to a real target, proving
	// the unpack path restores links rather than copying broken stubs.
	root := filepath.Join(directory, "Debug-iphonesimulator")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if _, statErr := os.Stat(path); statErr != nil {
				t.Errorf("symlink %s does not resolve: %v", path, statErr)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestExtract_ReusesWhenChecksumMatches(t *testing.T) {
	directory := t.TempDir()
	xctestrunPath, err := Extract(directory)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := []byte("SENTINEL-do-not-rewrite")
	if err := os.WriteFile(xctestrunPath, sentinel, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Extract(directory); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(xctestrunPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(sentinel) {
		t.Errorf("second extract rewrote the xctestrun; reuse branch should have skipped extraction")
	}
}

func TestExtract_RewritesIfChecksumMissing(t *testing.T) {
	directory := t.TempDir()
	if _, err := Extract(directory); err != nil {
		t.Fatal(err)
	}
	checksumPath := filepath.Join(directory, "runner.sha256")
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
