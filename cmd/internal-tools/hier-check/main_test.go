package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

func TestHierCheck_ParseAndFind(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("testdata", "dump.json"))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := hierarchy.Parse(string(content))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := len(tree.Elements); got != 4 {
		t.Fatalf("element count: got %d, want 4", got)
	}

	cases := []struct {
		selector string
		want     int
	}{
		{"desc:row", 2},
		{"id:title", 1},
		{"text:Bob", 1},
		{"id:missing", 0},
	}
	for _, c := range cases {
		if got := len(tree.FindAll(c.selector)); got != c.want {
			t.Errorf("%s: got %d matches, want %d", c.selector, got, c.want)
		}
	}
}

func TestHierCheck_MalformedReturnsError(t *testing.T) {
	if _, err := hierarchy.Parse(`{"attributes": {`); err == nil {
		t.Fatal("expected error for malformed hierarchy, got nil")
	}
}
