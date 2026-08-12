//go:build withsidecar

package sidecarassets

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestEmbeddedNonZero(t *testing.T) {
	if EmbeddedSize() == 0 {
		t.Errorf("expected embedded JAR to be non-empty")
	}
	if IsPlaceholder() {
		t.Errorf("withsidecar build should not be a placeholder")
	}
}

func TestExtract_WritesJARAndChecksum(t *testing.T) {
	directory := t.TempDir()
	path, err := Extract(directory)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(directory, "sanderling-sidecar.jar") {
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
	sentinel := []byte("SENTINEL-do-not-rewrite")
	if err := os.WriteFile(path, sentinel, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Extract(directory); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, sentinel) {
		t.Errorf("second extract rewrote the existing JAR; reuse branch corrupted on-disk file")
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

// TestExtract_ReadersNeverSeeAPartialJAR covers the cold-host case a parallel
// campaign hits: every worker process extracts into the same shared directory
// under os.TempDir, and one of them spawns a JVM against the JAR while another
// is still writing it. A plain write truncates the file in place, so the
// reader gets a short JAR and the JVM dies on a corrupt archive.
func TestExtract_ReadersNeverSeeAPartialJAR(t *testing.T) {
	directory := t.TempDir()
	want := EmbeddedSHA256()
	jarPath, err := Extract(directory)
	if err != nil {
		t.Fatal(err)
	}
	checksumPath := jarPath + ".sha256"

	stop := make(chan struct{})
	torn := make(chan string, 1)
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				content, err := os.ReadFile(jarPath)
				if err != nil {
					continue
				}
				sum := sha256.Sum256(content)
				if got := hex.EncodeToString(sum[:]); got != want {
					select {
					case torn <- fmt.Sprintf("read %d bytes, sha256 %s", len(content), got):
					default:
					}
					return
				}
			}
		}()
	}

	for range 8 {
		// Dropping the checksum is what a cold host looks like: no extraction
		// has been recorded, so every caller writes the JAR again.
		if err := os.Remove(checksumPath); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if _, err := Extract(directory); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	readers.Wait()

	select {
	case detail := <-torn:
		t.Fatalf("a reader observed a partial JAR while another caller extracted: %s", detail)
	default:
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".partial") {
			t.Errorf("Extract left a staging file behind: %s", entry.Name())
		}
	}
}
