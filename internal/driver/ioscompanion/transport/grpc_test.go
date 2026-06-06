package transport

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/priyanshujain/sanderling/internal/driver/ioscompanion/companionpb"
)

// grpcCompanion must satisfy Companion.
var _ Companion = (*grpcCompanion)(nil)

func TestDialReturnsCompanion(t *testing.T) {
	companion, err := Dial("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer companion.Close()
	if companion == nil {
		t.Fatal("Dial returned nil companion")
	}
}

func TestProcessStateFromProto(t *testing.T) {
	cases := []struct {
		in   pb.InstalledAppInfo_AppProcessState
		want ProcessState
	}{
		{pb.InstalledAppInfo_RUNNING, ProcessStateRunning},
		{pb.InstalledAppInfo_NOT_RUNNING, ProcessStateNotRunning},
		{pb.InstalledAppInfo_UNKNOWN, ProcessStateUnknown},
	}
	for _, c := range cases {
		if got := processStateFromProto(c.in); got != c.want {
			t.Errorf("processStateFromProto(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestScreenDescriptionFrom(t *testing.T) {
	resp := &pb.TargetDescriptionResponse{
		TargetDescription: &pb.TargetDescription{
			ScreenDimensions: &pb.ScreenDimensions{
				Width:        828,
				Height:       1792,
				Density:      2,
				WidthPoints:  414,
				HeightPoints: 896,
			},
		},
	}
	got := screenDescriptionFrom(resp)
	want := ScreenDescription{WidthPoints: 414, HeightPoints: 896, Scale: 2}
	if got != want {
		t.Errorf("screenDescriptionFrom = %+v, want %+v", got, want)
	}
}

func TestScreenDescriptionFromNilSafe(t *testing.T) {
	if got := screenDescriptionFrom(&pb.TargetDescriptionResponse{}); got != (ScreenDescription{}) {
		t.Errorf("screenDescriptionFrom(empty) = %+v, want zero", got)
	}
}

func TestTarGzipDirectory(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, "Sample.app")
	if err := os.MkdirAll(filepath.Join(bundle, "PlugIns"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "Info.plist"), []byte("plist"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "PlugIns", "ext"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	archive, err := tarGzipDirectory(bundle)
	if err != nil {
		t.Fatalf("tarGzipDirectory: %v", err)
	}

	names := tarEntryNames(t, archive)
	want := map[string]bool{
		"Sample.app":             true,
		"Sample.app/Info.plist":  true,
		"Sample.app/PlugIns":     true,
		"Sample.app/PlugIns/ext": true,
	}
	for name := range want {
		if !names[name] {
			t.Errorf("archive missing entry %q; got %v", name, names)
		}
	}
}

func tarEntryNames(t *testing.T, archive []byte) map[string]bool {
	t.Helper()
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	names := map[string]bool{}
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		names[filepath.ToSlash(filepath.Clean(header.Name))] = true
	}
	return names
}
