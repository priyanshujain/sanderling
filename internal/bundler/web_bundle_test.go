package bundler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeRuntime = `
(globalThis as Record<string, unknown>).__sanderling__ = {
  extract: (g: () => unknown) => ({ current: g(), previous: undefined }),
  always: () => ({ __sanderlingFormula: true }),
  now: () => ({ __sanderlingFormula: true }),
  next: () => ({ __sanderlingFormula: true }),
  eventually: () => ({ __sanderlingFormula: true }),
  actions: (g: () => unknown) => ({ __sanderlingActionGenerator: true, generate: g }),
  weighted: () => ({ __sanderlingActionGenerator: true }),
  from: () => ({ generate: () => null }),
  tap: () => null,
  inputText: () => null,
  swipe: () => null,
  pressKey: () => null,
  wait: () => null,
  taps: {},
  swipes: {},
  waitOnce: {},
  pressKeys: {},
};
(globalThis as Record<string, unknown>).__sanderlingExtractors__ = () => ({});
(globalThis as Record<string, unknown>).__sanderlingNextAction__ = () => null;
export {};
`

const fakeSpec = `
const handle = (globalThis as { __sanderling__: { extract: (g: () => unknown) => unknown } }).__sanderling__.extract(() => 42);
(globalThis as { actions?: unknown }).actions = handle;
export {};
`

func TestBundleWeb_RegistersExpectedGlobals(t *testing.T) {
	directory := t.TempDir()
	runtimePath := filepath.Join(directory, "web-runtime.ts")
	specPath := filepath.Join(directory, "spec.ts")
	if err := os.WriteFile(runtimePath, []byte(fakeRuntime), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specPath, []byte(fakeSpec), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := BundleWeb(WebOptions{
		EntryFile:      specPath,
		WebRuntimeFile: runtimePath,
	})
	if err != nil {
		t.Fatalf("BundleWeb: %v", err)
	}
	source := string(result.JavaScript)
	for _, expected := range []string{
		"__sanderlingExtractors__",
		"__sanderlingNextAction__",
		"__sanderling__",
	} {
		if !strings.Contains(source, expected) {
			t.Errorf("bundle missing %q\nsource head:\n%s", expected, head(source, 500))
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(source), "(()") {
		t.Errorf("expected IIFE format, head:\n%s", head(source, 200))
	}
	if result.SHA256 == "" {
		t.Errorf("expected non-empty sha256")
	}
}

func TestBundleWeb_RejectsMissingEntry(t *testing.T) {
	if _, err := BundleWeb(WebOptions{}); err == nil {
		t.Error("expected error for empty options")
	}
	if _, err := BundleWeb(WebOptions{EntryFile: "x.ts"}); err == nil {
		t.Error("expected error for missing runtime")
	}
}

func head(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max] + "..."
}
