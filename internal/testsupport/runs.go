// Package testsupport holds test helpers that more than one package needs.
// It exists because the suite's helper layer grew by copying: a helper written
// inside one package's tests is invisible to the next package that needs it, so
// the next package writes it again. Anything here has at least two callers in
// different packages; a helper with one belongs in that package's own tests.
package testsupport

import (
	"testing"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
	"github.com/priyanshujain/sanderling/internal/trace"
)

// WriteRunFromDumps writes a complete run directory: meta.json, then one step
// per hierarchy dump numbered from 1. It goes through trace.NewWriter rather
// than composing the files by hand so a fixture cannot claim a trace_version
// the writer would not have produced.
func WriteRunFromDumps(t *testing.T, directory string, meta trace.Meta, dumps ...string) string {
	t.Helper()
	writer, err := trace.NewWriter(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteMeta(meta); err != nil {
		t.Fatal(err)
	}
	for index, dump := range dumps {
		tree, err := hierarchy.Parse(dump)
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteStep(trace.Step{Index: index + 1, Hierarchy: tree}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return directory
}
