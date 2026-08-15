package testrun

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/driver"
	"github.com/priyanshujain/sanderling/internal/runner"
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

// TestRunOutcome_ReportsViolationsOnlyUnderTheFlag pins the CI contract: the
// typed error is what makes `sanderling test` exit 2, and it must appear only
// when the caller asked for it. A run that finds violations without the flag
// stays a successful run, which is what every existing invocation expects.
func TestRunOutcome_ReportsViolationsOnlyUnderTheFlag(t *testing.T) {
	violated := runner.Summary{
		Steps:      7,
		Violations: []runner.ViolationRecord{{StepIndex: 3, Properties: []string{"balanceMoves"}}},
	}
	clean := runner.Summary{Steps: 7}

	if err := runOutcome(Options{}, violated); err != nil {
		t.Errorf("without --exit-on-violation a violated run must succeed, got %v", err)
	}
	if err := runOutcome(Options{ExitOnViolation: true}, clean); err != nil {
		t.Errorf("a clean run must succeed under --exit-on-violation, got %v", err)
	}

	err := runOutcome(Options{ExitOnViolation: true}, violated)
	var violations ViolationsError
	if !errors.As(err, &violations) {
		t.Fatalf("expected a ViolationsError, got %v", err)
	}
	if violations.Count != 1 {
		t.Errorf("count: got %d, want 1", violations.Count)
	}
}

// A step the verifier skipped was judged by nothing, so a run whose every step
// was skipped holds no verdict at all: "no violations" there is the absence of
// an answer rather than a clean one. Reporting it as a successful run is the
// green and vacuous outcome structuralShape's own design notes call worse than
// the composition it catches, and the runner's hold is what makes a fully
// skipped run reachable.
func TestRunOutcome_ARunThatJudgedNothingIsNotASuccess(t *testing.T) {
	nothingJudged := runner.Summary{Steps: 6, SkippedVerification: 6}
	err := runOutcome(Options{}, nothingJudged)
	var vacuous VacuousRunError
	if !errors.As(err, &vacuous) {
		t.Fatalf("a run that judged none of its 6 steps came back %v, want a VacuousRunError", err)
	}
	if vacuous.Steps != 6 {
		t.Errorf("steps: got %d, want 6", vacuous.Steps)
	}

	// A screen that composes now and then costs a run steps, not its verdict. A
	// check that fired here would turn every healthy android run red.
	mostlyJudged := runner.Summary{Steps: 6, SkippedVerification: 5}
	if err := runOutcome(Options{}, mostlyJudged); err != nil {
		t.Errorf("a run that judged one of its 6 steps must succeed, got %v", err)
	}
}

// wedgedLaunchDriver never returns from Launch, standing in for a driver whose
// device-side session is stuck.
type wedgedLaunchDriver struct {
	driver.DeviceDriver
	release chan struct{}
}

func (w *wedgedLaunchDriver) Launch(ctx context.Context, _ string, _ bool, _ map[string]string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.release:
		return nil
	}
}

// TestLaunchAppBoundsWedgedDriver proves the pre-run launch carries a deadline.
// It runs before the runner starts, so --duration does not cover it and
// Execute's root context has no deadline: unbounded, a wedged driver hangs the
// run forever with no trace directory and no error.
func TestLaunchAppBoundsWedgedDriver(t *testing.T) {
	previous := launchTimeout
	launchTimeout = 100 * time.Millisecond
	defer func() { launchTimeout = previous }()

	wedged := &wedgedLaunchDriver{release: make(chan struct{})}
	defer close(wedged.release)

	done := make(chan error, 1)
	go func() { done <- launchApp(context.Background(), wedged, Options{BundleID: "com.example.app"}) }()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want a deadline-exceeded error", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("launchApp never returned: the pre-run launch is unbounded, so a wedged driver hangs the run forever")
	}
}

// repoFile walks up from the test's working directory and returns the absolute
// path of rel inside the sanderling checkout.
func repoFile(t *testing.T, rel string) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		candidate := filepath.Join(directory, rel)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatalf("%s not found above the test directory", rel)
		}
		directory = parent
	}
}

// publishedFiles returns the "files" entries of pkg/spec/package.json, the
// exact set npm ships in the @sanderling/spec tarball.
func publishedFiles(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(repoFile(t, "pkg/spec/package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest.Files
}

// installPublishedPackage reproduces what `npm install @sanderling/spec`
// unpacks into node_modules: only the paths package.json publishes.
func installPublishedPackage(t *testing.T, dest string) {
	t.Helper()
	specDir := filepath.Dir(repoFile(t, "pkg/spec/package.json"))
	for _, entry := range publishedFiles(t) {
		source := filepath.Join(specDir, entry)
		if _, err := os.Stat(source); err != nil {
			continue
		}
		copyTree(t, source, filepath.Join(dest, entry))
	}
}

func copyTree(t *testing.T, source, dest string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestResolveRuntimeSibling_PublishedPackageShipsTheRuntimes pins npm's "files"
// list against the resolver that consumes it. The tarball shipped dist/ alone
// while the node_modules fallback looks for src/goja-runtime.ts, so every
// `npm install @sanderling/spec` user hit "goja-runtime.ts not found".
func TestResolveRuntimeSibling_PublishedPackageShipsTheRuntimes(t *testing.T) {
	root := t.TempDir()
	installPublishedPackage(t, filepath.Join(root, "node_modules", "@sanderling", "spec"))
	specPath := filepath.Join(root, "spec.ts")
	if err := os.WriteFile(specPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, filename := range []string{"goja-runtime.ts", "web-runtime.ts"} {
		if resolveRuntimeSibling("", specPath, filename) == "" {
			t.Errorf("%s unreachable from a published install; package.json publishes %v",
				filename, publishedFiles(t))
		}
	}
}

// TestPrepareBundleInputs_InstalledPackageSharesOneModuleGraph pins the
// downstream case: with no sanderling checkout above the spec, the aliases and
// the runtime entry must name the SAME installed copy. An unset alias let
// esbuild resolve @sanderling/spec to dist/ while the runtime came from src/,
// which loads sampler-rng.ts twice; from(), strings(), integers() and emails()
// then read an rng the picker never set and collapse to a fixed default.
func TestPrepareBundleInputs_InstalledPackageSharesOneModuleGraph(t *testing.T) {
	root := t.TempDir()
	installed := filepath.Join(root, "node_modules", "@sanderling", "spec")
	installPublishedPackage(t, installed)
	specPath := filepath.Join(root, "sanderling", "spec.ts")
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		t.Fatal(err)
	}
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

	prep, err := prepareBundleInputs(Options{Spec: specPath})
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(installed, "src")
	want := map[string]string{
		"@sanderling/spec":                     filepath.Join(source, "index.ts"),
		"@sanderling/spec/defaults":            filepath.Join(source, "defaults/index.ts"),
		"@sanderling/spec/defaults/properties": filepath.Join(source, "defaults/properties.ts"),
	}
	for key, wantValue := range want {
		if prep.aliases[key] != wantValue {
			t.Errorf("alias %q = %q, want %q", key, prep.aliases[key], wantValue)
		}
	}
	if got := prep.gojaRuntimePath; got != filepath.Join(source, "goja-runtime.ts") {
		t.Errorf("gojaRuntimePath = %q, want it beside the aliased index.ts", got)
	}
	if got := resolveWebRuntimePath(prep.specAPIPath, specPath); got != filepath.Join(source, "web-runtime.ts") {
		t.Errorf("webRuntimePath = %q, want it beside the aliased index.ts", got)
	}
}
