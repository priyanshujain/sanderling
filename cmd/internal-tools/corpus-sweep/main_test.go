package main

import (
	"io"
	"strings"
	"testing"
)

// Three flags missing is one rerun, not three: the operator is told about all
// of them at once, in flag order, whatever order the check happened to walk.
func TestParseArguments_NamesEveryMissingRequiredFlagInFlagOrder(t *testing.T) {
	_, err := parseArguments(
		[]string{"--spec", "s", "--max-steps", "10"},
		io.Discard,
	)
	if err == nil {
		t.Fatal("got no error, want every missing flag named")
	}
	message := err.Error()
	previous := -1
	for _, name := range []string{"--corpus", "--seeds", "--output"} {
		at := strings.Index(message, name)
		if at < 0 {
			t.Fatalf("got %q, want %s named", message, name)
		}
		if at < previous {
			t.Errorf("got %q, want the flags named in flag order", message)
		}
		previous = at
	}
	if strings.Contains(message, "--spec") {
		t.Errorf("got %q, want the supplied --spec left out", message)
	}
}
