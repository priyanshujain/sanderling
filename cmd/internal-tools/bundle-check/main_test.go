package main

import (
	"os"
	"path/filepath"
	"testing"
)

// repoSpecSrc walks up from the test's working directory to the repo root and
// returns pkg/spec/src. The bundle-check aliases once pointed at the
// non-existent pkg/spec-api/src, so esbuild resolved nothing and the tool
// silently emitted an empty bundle.
func repoSpecSrc(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		candidate := filepath.Join(dir, "pkg/spec/src")
		if _, err := os.Stat(filepath.Join(candidate, "index.ts")); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("pkg/spec/src/index.ts not found above test dir")
		}
		dir = parent
	}
}

func TestBundleSpec_ResolvesSpecAliases(t *testing.T) {
	specSrc := repoSpecSrc(t)
	entry, err := filepath.Abs(filepath.Join("testdata", "spec.ts"))
	if err != nil {
		t.Fatal(err)
	}

	result, err := bundleSpec(specSrc, entry)
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}
	if len(result.JavaScript) == 0 {
		t.Fatal("empty bundle: aliases resolved to nothing")
	}
	if result.SHA256 == "" {
		t.Fatal("missing bundle hash")
	}

	second, err := bundleSpec(specSrc, entry)
	if err != nil {
		t.Fatal(err)
	}
	if result.SHA256 != second.SHA256 {
		t.Errorf("unstable bundle hash: %s vs %s", result.SHA256, second.SHA256)
	}
}
