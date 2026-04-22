package testrun

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSpecAPIPath_FindsUpwardSibling(t *testing.T) {
	root := t.TempDir()
	apiPath := filepath.Join(root, "pkg", "spec", "src", "index.ts")
	if err := os.MkdirAll(filepath.Dir(apiPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(apiPath, []byte("export {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(root, "examples", "app", "spec.ts")
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolveSpecAPIPath(specPath)
	if got != apiPath {
		t.Fatalf("got %q, want %q", got, apiPath)
	}
}

func TestResolveSpecAPIPath_ReturnsEmptyWhenMissing(t *testing.T) {
	root := t.TempDir()
	specPath := filepath.Join(root, "spec.ts")
	if err := os.WriteFile(specPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	got := resolveSpecAPIPath(specPath)
	if got != "" {
		t.Fatalf("got %q, want empty (no sanderling source tree reachable)", got)
	}
}
