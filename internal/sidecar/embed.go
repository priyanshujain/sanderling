package sidecar

import (
	"crypto/sha256"
	"encoding/hex"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed assets/sidecar-all.jar
var embeddedJAR []byte

// PlaceholderMagic is the byte sequence written to the placeholder JAR that
// ships in the repository. A binary that still carries the placeholder is
// not runnable against a real sidecar — `make sidecar && make uatu` must
// have been invoked to embed the real fat JAR.
const PlaceholderMagic = "uatu-sidecar-placeholder"

// IsPlaceholder reports whether the embedded JAR is the development
// placeholder rather than a real sidecar fat JAR.
func IsPlaceholder() bool {
	return string(embeddedJAR) == PlaceholderMagic
}

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
		return "", errors.New("sidecar: embedded JAR is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	jarPath := filepath.Join(dir, "uatu-sidecar.jar")
	checksumPath := jarPath + ".sha256"
	checksum := EmbeddedSHA256()

	if existing, err := os.ReadFile(checksumPath); err == nil && string(existing) == checksum {
		if _, err := os.Stat(jarPath); err == nil {
			return jarPath, nil
		}
	}

	if err := os.WriteFile(jarPath, embeddedJAR, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", jarPath, err)
	}
	if err := os.WriteFile(checksumPath, []byte(checksum), 0o644); err != nil {
		return "", fmt.Errorf("write checksum: %w", err)
	}
	return jarPath, nil
}
