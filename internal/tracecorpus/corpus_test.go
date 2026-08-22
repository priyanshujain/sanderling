package tracecorpus

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/priyanshujain/sanderling/internal/testsupport"
	"github.com/priyanshujain/sanderling/internal/trace"
)

const oneScreen = `{"attributes": {"text": "Hi", "bounds": "[0,0,10,10]"}, "children": [
    {"attributes": {"text": "child"}, "children": []}
  ]}`

func TestLoadRebuildsTheTreeAStepStored(t *testing.T) {
	directory := writeRun(t, oneScreen)

	run, err := Load(directory)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(run.Steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(run.Steps))
	}
	root := run.Steps[0].Hierarchy.Root
	if root == nil {
		t.Fatal("stored hierarchy came back with no root, so no selector or hash reads it")
	}
	if len(root.Children) != 1 || root.Children[0].Text != "child" {
		t.Fatalf("rebuilt tree lost its child: %+v", root)
	}
}

func TestLoadRefusesAVersionZeroStepByVersion(t *testing.T) {
	directory := writeRun(t, oneScreen)
	downgrade(t, filepath.Join(directory, "trace.jsonl"))

	_, err := Load(directory)
	if err == nil {
		t.Fatal("a version 0 step must be refused, not measured")
	}
	if !strings.Contains(err.Error(), "trace_version 0") {
		t.Fatalf("refusal must name the version, got %q", err)
	}
}

// TestLoadRefusesElementsWithNoStoredShape covers the failure that would be
// silent: a tree that decodes with elements and no root hashes to the empty
// string, which reads as one state shared by every screen.
func TestLoadRefusesElementsWithNoStoredShape(t *testing.T) {
	directory := writeRun(t, oneScreen)
	path := filepath.Join(directory, "trace.jsonl")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var step map[string]any
	if err := json.Unmarshal(body, &step); err != nil {
		t.Fatal(err)
	}
	hierarchyField := step["hierarchy"].(map[string]any)
	delete(hierarchyField, "depths")
	rewrite(t, path, step)

	if _, err := Load(directory); err == nil ||
		!strings.Contains(err.Error(), "cannot be rebuilt") {
		t.Fatalf("a shapeless tree must be refused, got %v", err)
	}
}

func writeRun(t *testing.T, dumps ...string) string {
	t.Helper()
	return testsupport.WriteRunFromDumps(
		t, t.TempDir(), trace.Meta{Seed: 7, Platform: "web"}, dumps...)
}

func downgrade(t *testing.T, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var step map[string]any
	if err := json.Unmarshal(body, &step); err != nil {
		t.Fatal(err)
	}
	delete(step, "trace_version")
	rewrite(t, path, step)
}

func rewrite(t *testing.T, path string, step map[string]any) {
	t.Helper()
	body, err := json.Marshal(step)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
