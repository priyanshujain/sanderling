// Package sidecarassets embeds the native sidecar JAR and extracts it to disk at runtime.
package sidecarassets

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// EmbeddedSize returns the size in bytes of the embedded JAR.
func EmbeddedSize() int { return len(embeddedJAR) }

// EmbeddedSHA256 returns the hex-encoded SHA-256 of the embedded JAR.
func EmbeddedSHA256() string {
	sum := sha256.Sum256(embeddedJAR)
	return hex.EncodeToString(sum[:])
}

// Extract writes the embedded JAR to a deterministic path inside dir,
// alongside a .sha256 file. If the destination already exists with a
// matching checksum, no rewrite happens. Returns the JAR path.
func Extract(dir string) (string, error) {
	if len(embeddedJAR) == 0 {
		return "", errors.New("sidecar: binary built without -tags withsidecar; rebuild with `make sanderling`")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	jarPath := filepath.Join(dir, "sanderling-sidecar.jar")
	checksumPath := jarPath + ".sha256"
	checksum := EmbeddedSHA256()

	if existing, err := os.ReadFile(checksumPath); err == nil && string(existing) == checksum {
		if _, err := os.Stat(jarPath); err == nil {
			return jarPath, nil
		}
	}

	if err := writeAtomic(jarPath, embeddedJAR); err != nil {
		return "", fmt.Errorf("write %s: %w", jarPath, err)
	}
	if err := writeAtomic(checksumPath, []byte(checksum)); err != nil {
		return "", fmt.Errorf("write checksum: %w", err)
	}
	return jarPath, nil
}

// writeAtomic publishes content at path through a rename, so a reader never
// observes a partial file. dir is shared between every sanderling process on
// the host, so a campaign running one worker per device has several of them
// extracting the same JAR at once on a cold host; a plain write let one
// process spawn a JVM against another's half-written file.
func writeAtomic(path string, content []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.partial")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temporary.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}
