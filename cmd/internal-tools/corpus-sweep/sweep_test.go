package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// Two binaries missing is one rerun, not two: the operator is told about both
// at once, in flag order, whatever order the check happened to walk.
func TestResolveBinaries_NamesEveryMissingBinaryInFlagOrder(t *testing.T) {
	configuration := config{
		campaignPath:   "campaign-that-is-not-installed",
		sanderlingPath: "sanderling-that-is-not-installed",
	}

	err := resolveBinaries(&configuration)
	if err == nil {
		t.Fatal("got no error, want both missing binaries named")
	}
	message := err.Error()
	campaign := strings.Index(message, "--campaign")
	sanderling := strings.Index(message, "--sanderling")
	if campaign < 0 || sanderling < 0 {
		t.Fatalf("got %q, want both --campaign and --sanderling named", message)
	}
	if campaign > sanderling {
		t.Errorf("got %q, want --campaign named before --sanderling", message)
	}

	resolved := config{
		campaignPath: writeScript(
			t,
			filepath.Join(t.TempDir(), "stub-campaign"),
			"#!/bin/sh\nexit 0\n",
		),
		sanderlingPath: "sanderling-that-is-not-installed",
	}
	err = resolveBinaries(&resolved)
	if err == nil {
		t.Fatal("got no error, want the missing sanderling named")
	}
	if strings.Contains(err.Error(), "--campaign") {
		t.Errorf("got %q, want the campaign that resolved left out", err.Error())
	}
}
