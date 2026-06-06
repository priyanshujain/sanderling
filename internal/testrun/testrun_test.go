package testrun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSeed_UsesConfiguredWhenNonZero(t *testing.T) {
	if got := resolveSeed(42); got != 42 {
		t.Fatalf("got %d, want 42", got)
	}
}

func TestResolveSeed_DerivesWhenZero(t *testing.T) {
	if got := resolveSeed(0); got == 0 {
		t.Fatal("expected a non-zero time-derived seed")
	}
}

func TestPrepareBundleInputs_MissingGojaRuntime(t *testing.T) {
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

	_, err = prepareBundleInputs(Options{Spec: specPath})
	if err == nil || !strings.Contains(err.Error(), "goja-runtime.ts not found") ||
		!strings.Contains(err.Error(), "checkout pkg/spec or set @sanderling/spec alias") {
		t.Fatalf("got %v, want documented goja-runtime error", err)
	}
}

func TestPrepareBundleInputs_DerivesSpecAliases(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "pkg", "spec", "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	apiPath := filepath.Join(srcDir, "index.ts")
	gojaPath := filepath.Join(srcDir, "goja-runtime.ts")
	for _, p := range []string{apiPath, gojaPath} {
		if err := os.WriteFile(p, []byte("export {}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	specPath := filepath.Join(root, "examples", "spec.ts")
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	prep, err := prepareBundleInputs(Options{Spec: specPath})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"@sanderling/spec":                     apiPath,
		"@sanderling/spec/defaults":            filepath.Join(srcDir, "defaults/index.ts"),
		"@sanderling/spec/defaults/properties": filepath.Join(srcDir, "defaults/properties.ts"),
	}
	for key, wantValue := range want {
		if prep.aliases[key] != wantValue {
			t.Errorf("alias %q = %q, want %q", key, prep.aliases[key], wantValue)
		}
	}
	if len(prep.aliases) != len(want) {
		t.Errorf("got %d aliases, want %d: %v", len(prep.aliases), len(want), prep.aliases)
	}
	if prep.gojaRuntimePath != gojaPath {
		t.Errorf("gojaRuntimePath = %q, want %q", prep.gojaRuntimePath, gojaPath)
	}
}

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
