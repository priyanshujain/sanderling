package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

func testdataSpec(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCheck_RejectsSpecThatRegistersNoProperties(t *testing.T) {
	var stdout bytes.Buffer
	err := check(repoSpecSrc(t), testdataSpec(t, "no-properties.ts"), &stdout)
	if err == nil {
		t.Fatal("a spec registering zero properties passed the gate: a run against it reports no violations while checking nothing")
	}
	if !strings.Contains(err.Error(), "registers no properties") {
		t.Errorf("failure must name the missing registration, got: %v", err)
	}
	if !strings.Contains(stdout.String(), "bundled: ") {
		t.Errorf("bundle size and hash must still be reported for a spec awaiting its properties, got: %q", stdout.String())
	}
}

func TestCheck_ReportsRegisteredPropertyCountAndNames(t *testing.T) {
	var stdout bytes.Buffer
	if err := check(repoSpecSrc(t), testdataSpec(t, "spec.ts"), &stdout); err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(stdout.String(), "properties registered: 1 (noUncaughtExceptions)") {
		t.Errorf("expected the registered count and name, got: %q", stdout.String())
	}
}

// TestCheck_ReportsUnchangedBundleHash pins the reported bundle of a fixture
// that imports nothing, so the hash moves only when the bundler itself does.
// Frozen pre-registrations record these hashes, so bundle-check reporting a
// different bundle (the runtime-entry one, say) silently invalidates them.
func TestCheck_ReportsUnchangedBundleHash(t *testing.T) {
	const (
		plainBytes  = 57
		plainSHA256 = "bd757084b3f29c04c68ba2aa4c7b93e63b6dc6dd08ea3f726e0d1878be845888"
	)
	result, err := bundleSpec(repoSpecSrc(t), testdataSpec(t, "plain.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.JavaScript) != plainBytes || result.SHA256 != plainSHA256 {
		t.Errorf("reported bundle changed: %d bytes sha256=%s, want %d bytes sha256=%s",
			len(result.JavaScript), result.SHA256, plainBytes, plainSHA256)
	}
}
