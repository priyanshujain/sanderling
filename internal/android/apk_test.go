package android

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// folioAPK is the folio debug APK stripped to the one entry the reader looks
// at, so the parse runs against a manifest aapt2 really produced.
const folioAPK = "testdata/folio.apk"

func TestPackageName_ReadsCompiledManifest(t *testing.T) {
	name, err := PackageName(folioAPK)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "app.folio" {
		t.Fatalf("package name: got %q, want app.folio", name)
	}
}

func TestPackageName_UTF8StringPool(t *testing.T) {
	name, err := PackageName(apkWithManifest(t, utf8PooledManifest("com.example.utf8")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "com.example.utf8" {
		t.Fatalf("package name: got %q, want com.example.utf8", name)
	}
}

func TestPackageName_NotAnAPK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.apk")
	if err := os.WriteFile(path, []byte("this is not a zip"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	_, err := PackageName(path)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("expected an error naming %s, got %v", path, err)
	}
}

func TestPackageName_NoManifestEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.apk")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create apk: %v", err)
	}
	archive := zip.NewWriter(file)
	if _, err := archive.Create("classes.dex"); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close apk: %v", err)
	}
	_, err = PackageName(path)
	if err == nil || !strings.Contains(err.Error(), "AndroidManifest.xml") {
		t.Fatalf("expected a missing-manifest error, got %v", err)
	}
}

func TestPackageName_TruncatedManifest(t *testing.T) {
	manifest, err := manifestFromAPK(folioAPK)
	if err != nil {
		t.Fatalf("read fixture manifest: %v", err)
	}
	for _, keep := range []int{4, 40, 400} {
		if _, err := PackageName(apkWithManifest(t, manifest[:keep])); err == nil {
			t.Fatalf("expected an error from a manifest cut to %d bytes", keep)
		}
	}
}

func apkWithManifest(t *testing.T, manifest []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.apk")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create apk: %v", err)
	}
	archive := zip.NewWriter(file)
	entry, err := archive.Create(manifestEntry)
	if err != nil {
		t.Fatalf("create manifest entry: %v", err)
	}
	if _, err := entry.Write(manifest); err != nil {
		t.Fatalf("write manifest entry: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close apk: %v", err)
	}
	return path
}

// utf8PooledManifest encodes the smallest manifest that carries a package
// name, with the UTF-8 string pool that aapt2 does not emit and older builders
// do. Without it the UTF-8 decoding path is never exercised: the compiled
// manifests aapt2 writes all pool their strings as UTF-16.
func utf8PooledManifest(packageName string) []byte {
	pool := []string{"manifest", "package", packageName}
	var pooled bytes.Buffer
	offsets := make([]uint32, len(pool))
	for index, value := range pool {
		offsets[index] = uint32(pooled.Len())
		pooled.WriteByte(byte(len(value)))
		pooled.WriteByte(byte(len(value)))
		pooled.WriteString(value)
		pooled.WriteByte(0)
	}
	stringsStart := uint32(28 + 4*len(pool))

	var poolChunk bytes.Buffer
	write16(&poolChunk, chunkStringPool)
	write16(&poolChunk, 28)
	write32(&poolChunk, stringsStart+uint32(pooled.Len()))
	write32(&poolChunk, uint32(len(pool)))
	write32(&poolChunk, 0)
	write32(&poolChunk, stringPoolUTF8)
	write32(&poolChunk, stringsStart)
	write32(&poolChunk, 0)
	for _, offset := range offsets {
		write32(&poolChunk, offset)
	}
	poolChunk.Write(pooled.Bytes())

	var element bytes.Buffer
	write16(&element, chunkStartElement)
	write16(&element, 16)
	write32(&element, 36+20)
	write32(&element, 1)          // line number
	write32(&element, 0xFFFFFFFF) // comment
	write32(&element, 0xFFFFFFFF) // namespace
	write32(&element, 0)          // name: "manifest"
	write16(&element, 20)         // attributes start, past this header
	write16(&element, 20)         // attribute stride
	write16(&element, 1)          // attribute count
	write16(&element, 0)          // id index
	write16(&element, 0)          // class index
	write16(&element, 0)          // style index
	write32(&element, 0xFFFFFFFF) // attribute namespace
	write32(&element, 1)          // attribute name: "package"
	write32(&element, 2)          // attribute raw value: the package name
	write16(&element, 8)          // typed value size
	write16(&element, 3<<8)       // res0, and TYPE_STRING
	write32(&element, 2)

	var manifest bytes.Buffer
	write16(&manifest, 3)
	write16(&manifest, 8)
	write32(&manifest, uint32(8+poolChunk.Len()+element.Len()))
	manifest.Write(poolChunk.Bytes())
	manifest.Write(element.Bytes())
	return manifest.Bytes()
}

func write16(buffer *bytes.Buffer, value uint16) {
	_ = binary.Write(buffer, binary.LittleEndian, value)
}

func write32(buffer *bytes.Buffer, value uint32) {
	_ = binary.Write(buffer, binary.LittleEndian, value)
}
