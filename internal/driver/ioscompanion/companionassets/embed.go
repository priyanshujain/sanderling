// Package companionassets embeds the simulator companion binary and its
// frameworks, and extracts the bundle to disk at runtime.
package companionassets

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// companionBinaryName is the file name inside bin/ fixed by the vendored
// layout. The binary resolves its frameworks through @rpath relative to this
// path, so it must not be renamed.
const companionBinaryName = "idb_companion"

// EmbeddedSize returns the size in bytes of the embedded companion archive.
func EmbeddedSize() int { return len(embeddedArchive) }

// EmbeddedSHA256 returns the hex-encoded SHA-256 of the embedded archive.
func EmbeddedSHA256() string {
	sum := sha256.Sum256(embeddedArchive)
	return hex.EncodeToString(sum[:])
}

// Extract unpacks the embedded companion archive into dir, preserving the
// bin/ and Frameworks/ layout and any symlinks. A .sha256 marker next to the
// extracted tree gates re-extraction: if it already matches, no rewrite
// happens. Returns the absolute path to the companion binary.
func Extract(dir string) (string, error) {
	if len(embeddedArchive) == 0 {
		return "", errors.New("companion: binary built without -tags withcompanion; rebuild with `make sanderling`")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}

	binaryPath, err := filepath.Abs(filepath.Join(dir, "bin", companionBinaryName))
	if err != nil {
		return "", err
	}
	checksumPath := filepath.Join(dir, "companion.sha256")
	checksum := EmbeddedSHA256()

	if existing, err := os.ReadFile(checksumPath); err == nil && string(existing) == checksum {
		if _, err := os.Stat(binaryPath); err == nil {
			return binaryPath, nil
		}
	}

	if err := unpack(dir); err != nil {
		return "", err
	}
	if err := os.Chmod(binaryPath, 0o755); err != nil {
		return "", fmt.Errorf("chmod companion binary: %w", err)
	}
	if err := os.WriteFile(checksumPath, []byte(checksum), 0o644); err != nil {
		return "", fmt.Errorf("write checksum: %w", err)
	}
	return binaryPath, nil
}

func unpack(dir string) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(embeddedArchive))
	if err != nil {
		return fmt.Errorf("open companion archive: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read companion archive: %w", err)
		}

		if strings.HasPrefix(filepath.Base(header.Name), "._") {
			continue
		}

		target, err := safeJoin(dir, header.Name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			os.Remove(target)
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeFile(target, tarReader, os.FileMode(header.Mode)); err != nil {
				return err
			}
		}
	}
}

func writeFile(path string, src io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, src); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// safeJoin rejects archive entries that would escape dir.
func safeJoin(dir, name string) (string, error) {
	target := filepath.Join(dir, name)
	relative, err := filepath.Rel(dir, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry escapes destination: %s", name)
	}
	return target, nil
}
