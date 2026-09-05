package android

import (
	"archive/zip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unicode/utf16"
)

const (
	manifestEntry = "AndroidManifest.xml"

	chunkStringPool   = 0x0001
	chunkStartElement = 0x0102

	stringPoolUTF8 = 1 << 8
)

var errStringPoolRange = errors.New("string pool entry runs past the chunk")

// PackageName reads an APK's application id out of its compiled manifest,
// which carries the id the package manager will know the app by, suffixes and
// all. Parsing it here rather than shelling out to `aapt2 dump packagename`
// keeps the read working on the many hosts that install platform-tools for adb
// and never install the versioned build-tools directory aapt2 lives in.
func PackageName(apkPath string) (string, error) {
	manifest, err := manifestFromAPK(apkPath)
	if err != nil {
		return "", err
	}
	name, err := manifestPackage(manifest)
	if err != nil {
		return "", fmt.Errorf("%s: %w", apkPath, err)
	}
	return name, nil
}

func manifestFromAPK(apkPath string) ([]byte, error) {
	archive, err := zip.OpenReader(apkPath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", apkPath, err)
	}
	defer archive.Close()
	for _, file := range archive.File {
		if file.Name != manifestEntry {
			continue
		}
		entry, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open %s in %s: %w", manifestEntry, apkPath, err)
		}
		defer entry.Close()
		manifest, err := io.ReadAll(entry)
		if err != nil {
			return nil, fmt.Errorf("read %s in %s: %w", manifestEntry, apkPath, err)
		}
		return manifest, nil
	}
	return nil, fmt.Errorf("%s holds no %s", apkPath, manifestEntry)
}

// manifestPackage walks the compiled manifest's chunks for the <manifest>
// element and reports its package attribute. The string pool always precedes
// the elements that index into it.
func manifestPackage(manifest []byte) (string, error) {
	if len(manifest) < 8 {
		return "", errors.New("truncated binary xml")
	}
	var pool []string
	for offset := uint32(8); offset+8 <= uint32(len(manifest)); {
		size := binary.LittleEndian.Uint32(manifest[offset+4:])
		if size < 8 || uint64(offset)+uint64(size) > uint64(len(manifest)) {
			return "", errors.New("truncated chunk in binary xml")
		}
		chunk := manifest[offset : offset+size]
		switch binary.LittleEndian.Uint16(chunk) {
		case chunkStringPool:
			var err error
			if pool, err = poolStrings(chunk); err != nil {
				return "", err
			}
		case chunkStartElement:
			name, found, err := packageAttribute(chunk, pool)
			if err != nil || found {
				return name, err
			}
		}
		offset += size
	}
	return "", errors.New("no <manifest> element in binary xml")
}

// packageAttribute reports the package attribute of a start-element chunk, and
// whether that chunk was the <manifest> element at all.
func packageAttribute(chunk []byte, pool []string) (string, bool, error) {
	// The element's own fields start after the chunk header, line number and
	// comment index, and the attribute offsets are relative to there.
	const element = 16
	if len(chunk) < element+20 {
		return "", false, errors.New("truncated start-element chunk")
	}
	if poolString(pool, binary.LittleEndian.Uint32(chunk[element+4:])) != "manifest" {
		return "", false, nil
	}
	start := uint64(binary.LittleEndian.Uint16(chunk[element+8:]))
	stride := uint64(binary.LittleEndian.Uint16(chunk[element+10:]))
	count := uint64(binary.LittleEndian.Uint16(chunk[element+12:]))
	if stride < 20 {
		return "", false, fmt.Errorf("<manifest> attribute stride is %d bytes, want at least 20", stride)
	}
	for index := uint64(0); index < count; index++ {
		at := element + start + index*stride
		if at+20 > uint64(len(chunk)) {
			return "", false, errors.New("<manifest> attribute runs past the chunk")
		}
		if poolString(pool, binary.LittleEndian.Uint32(chunk[at+4:])) != "package" {
			continue
		}
		name := poolString(pool, binary.LittleEndian.Uint32(chunk[at+8:]))
		if name == "" {
			// aapt2 leaves the raw value unset and keeps the string in the
			// typed value's data word.
			name = poolString(pool, binary.LittleEndian.Uint32(chunk[at+16:]))
		}
		if name == "" {
			return "", false, errors.New("<manifest> package attribute is empty")
		}
		return name, true, nil
	}
	return "", false, errors.New("<manifest> has no package attribute")
}

func poolString(pool []string, index uint32) string {
	if uint64(index) >= uint64(len(pool)) {
		return ""
	}
	return pool[index]
}

func poolStrings(chunk []byte) ([]string, error) {
	const header = 28
	if len(chunk) < header {
		return nil, errors.New("truncated string pool")
	}
	count := uint64(binary.LittleEndian.Uint32(chunk[8:]))
	flags := binary.LittleEndian.Uint32(chunk[16:])
	start := uint64(binary.LittleEndian.Uint32(chunk[20:]))
	if header+4*count > uint64(len(chunk)) {
		return nil, errors.New("string pool offsets run past the chunk")
	}
	pool := make([]string, count)
	for index := uint64(0); index < count; index++ {
		at := start + uint64(binary.LittleEndian.Uint32(chunk[header+4*index:]))
		value, err := poolString8Or16(chunk, at, flags&stringPoolUTF8 != 0)
		if err != nil {
			return nil, err
		}
		pool[index] = value
	}
	return pool, nil
}

func poolString8Or16(chunk []byte, at uint64, utf8 bool) (string, error) {
	if utf8 {
		// The character count precedes the byte count; only the second governs
		// how far the string reaches.
		at, _, err := prefixLength8(chunk, at)
		if err != nil {
			return "", err
		}
		at, length, err := prefixLength8(chunk, at)
		if err != nil {
			return "", err
		}
		if at+length > uint64(len(chunk)) {
			return "", errStringPoolRange
		}
		return string(chunk[at : at+length]), nil
	}
	at, length, err := prefixLength16(chunk, at)
	if err != nil {
		return "", err
	}
	if at+2*length > uint64(len(chunk)) {
		return "", errStringPoolRange
	}
	units := make([]uint16, length)
	for index := range units {
		units[index] = binary.LittleEndian.Uint16(chunk[at+2*uint64(index):])
	}
	return string(utf16.Decode(units)), nil
}

// prefixLength8 reads a UTF-8 pool string's length prefix: one byte, or two
// when the high bit marks the value as wider. It returns the offset past the
// prefix.
func prefixLength8(chunk []byte, at uint64) (uint64, uint64, error) {
	if at >= uint64(len(chunk)) {
		return 0, 0, errStringPoolRange
	}
	length := uint64(chunk[at])
	if length&0x80 == 0 {
		return at + 1, length, nil
	}
	if at+1 >= uint64(len(chunk)) {
		return 0, 0, errStringPoolRange
	}
	return at + 2, (length&0x7f)<<8 | uint64(chunk[at+1]), nil
}

// prefixLength16 reads a UTF-16 pool string's length prefix, in units rather
// than bytes: one 16-bit word, or two when the high bit marks the value as
// wider.
func prefixLength16(chunk []byte, at uint64) (uint64, uint64, error) {
	if at+2 > uint64(len(chunk)) {
		return 0, 0, errStringPoolRange
	}
	length := uint64(binary.LittleEndian.Uint16(chunk[at:]))
	if length&0x8000 == 0 {
		return at + 2, length, nil
	}
	if at+4 > uint64(len(chunk)) {
		return 0, 0, errStringPoolRange
	}
	return at + 4, (length&0x7fff)<<16 | uint64(binary.LittleEndian.Uint16(chunk[at+2:])), nil
}
