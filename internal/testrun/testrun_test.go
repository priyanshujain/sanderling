package testrun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/verifier"
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

func TestResolveRuntimeSibling(t *testing.T) {
	const filename = "goja-runtime.ts"

	siblingRoot := t.TempDir()
	siblingAPI := filepath.Join(siblingRoot, "pkg", "spec", "src", "index.ts")
	if err := os.MkdirAll(filepath.Dir(siblingAPI), 0o755); err != nil {
		t.Fatal(err)
	}
	siblingFile := filepath.Join(filepath.Dir(siblingAPI), filename)
	if err := os.WriteFile(siblingFile, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	nmRoot := t.TempDir()
	nmFile := filepath.Join(nmRoot, "node_modules", "@sanderling", "spec", "src", filename)
	if err := os.MkdirAll(filepath.Dir(nmFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nmFile, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	nmSpec := filepath.Join(nmRoot, "examples", "deep", "spec.ts")
	if err := os.MkdirAll(filepath.Dir(nmSpec), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nmSpec, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	missingSpec := filepath.Join(t.TempDir(), "spec.ts")
	if err := os.WriteFile(missingSpec, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		specAPIPath string
		userSpec    string
		want        string
	}{
		{"sibling next to spec-API wins", siblingAPI, missingSpec, siblingFile},
		{"node_modules fallback upward", "", nmSpec, nmFile},
		{"neither reachable returns empty", "", missingSpec, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveRuntimeSibling(tt.specAPIPath, tt.userSpec, filename); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
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

func TestBuildRunMeta_RecordsArmMembership(t *testing.T) {
	options := Options{
		Spec:      "spec.ts",
		BundleID:  "com.example",
		Platform:  "android",
		Duration:  3 * time.Minute,
		MaxSteps:  300,
		Arm:       "llm-visible-text",
		Generator: "llm",
	}
	meta := buildRunMeta(options, "deadbeef", 7, "farm-01",
		verifier.LLMConfig{Model: "claude-sonnet-5", Instructions: "exercise the outbox"}, true)

	if meta.Arm != "llm-visible-text" || meta.Generator != "llm" {
		t.Errorf("arm membership: got arm=%q generator=%q", meta.Arm, meta.Generator)
	}
	if meta.Model != "claude-sonnet-5" || meta.Instructions != "exercise the outbox" {
		t.Errorf("llm config: got model=%q instructions=%q", meta.Model, meta.Instructions)
	}
	if meta.MaxSteps != 300 || meta.DurationMillis != 180000 {
		t.Errorf("budget: got maxSteps=%d durationMillis=%d", meta.MaxSteps, meta.DurationMillis)
	}
	if meta.Host != "farm-01" || meta.Seed != 7 {
		t.Errorf("host and seed: got host=%q seed=%d", meta.Host, meta.Seed)
	}
}

func TestBuildRunMeta_OmitsModelWhenSeededPickerRuns(t *testing.T) {
	options := Options{Platform: "android", Generator: "seeded", Duration: time.Minute}
	meta := buildRunMeta(options, "deadbeef", 1, "farm-01",
		verifier.LLMConfig{Model: "claude-sonnet-5", Instructions: "hunt bugs"}, true)

	if meta.Model != "" || meta.Instructions != "" {
		t.Errorf("a seeded run must not be labelled with a model it never called: model=%q instructions=%q",
			meta.Model, meta.Instructions)
	}
}

func TestBuildRunMeta_OmitsModelWhenSpecDeclaresNoLLMGenerator(t *testing.T) {
	options := Options{Platform: "android", Generator: "llm", Duration: time.Minute}
	meta := buildRunMeta(options, "deadbeef", 1, "farm-01", verifier.LLMConfig{}, false)

	if meta.Model != "" {
		t.Errorf("model recorded without a spec-declared llm generator: %q", meta.Model)
	}
}
