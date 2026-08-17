package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverImplementations_NameOrderAndOnePortEach(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"impl-03", "impl-01", "impl-10", "impl-02", "scaffold", ".DS_Store"} {
		if err := os.MkdirAll(filepath.Join(directory, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "impl-notes.md"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	found, err := discoverImplementations(directory, 5300)
	if err != nil {
		t.Fatal(err)
	}
	want := []implementation{
		{Name: "impl-01", Port: 5300},
		{Name: "impl-02", Port: 5301},
		{Name: "impl-03", Port: 5302},
		{Name: "impl-10", Port: 5303},
	}
	if len(found) != len(want) {
		t.Fatalf(
			"got %d implementations, want %d: %v",
			len(found),
			len(want),
			found,
		)
	}
	for index, target := range found {
		if target.Name != want[index].Name || target.Port != want[index].Port {
			t.Errorf(
				"position %d: got %s on %d, want %s on %d",
				index,
				target.Name,
				target.Port,
				want[index].Name,
				want[index].Port,
			)
		}
		if target.Directory != filepath.Join(directory, want[index].Name) {
			t.Errorf("%s directory: got %q", target.Name, target.Directory)
		}
	}
}

func TestDiscoverImplementations_EmptyDirectoryIsRefused(t *testing.T) {
	_, err := discoverImplementations(t.TempDir(), 5300)
	if err == nil || !strings.Contains(err.Error(), "no impl-* directories") {
		t.Fatalf("got %v, want a refusal naming impl-*", err)
	}
}

// A binary that is not there fails once, before anything is installed, rather
// than twenty-four times after the sweep has spent its build time.
func TestRunSweep_StopsBeforeItInstallsAnythingWhenABinaryIsMissing(
	t *testing.T,
) {
	root := t.TempDir()
	implementations := filepath.Join(root, "implementations")
	if err := os.MkdirAll(filepath.Join(implementations, "impl-01"), 0o755); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "campaigns")
	configuration := config{
		implementationsDirectory: implementations,
		outputDirectory:          output,
		basePort:                 5300,
		concurrency:              1,
		bunPath: writeScript(
			t,
			filepath.Join(root, "stub-bun"),
			"#!/bin/sh\nexit 0\n",
		),
		campaignPath: "campaign-that-is-not-installed",
		sanderlingPath: writeScript(
			t,
			filepath.Join(root, "stub-sanderling"),
			"#!/bin/sh\nexit 0\n",
		),
	}
	err := runSweep(t.Context(), configuration, os.Stdout)
	if err == nil || !strings.Contains(err.Error(), "--campaign") {
		t.Fatalf("got %v, want the missing campaign binary named", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Errorf(
			"the sweep created %s before it checked it could run: %v",
			output,
			err,
		)
	}
}

func TestRunSweep_RefusesADirectoryThatAlreadyHoldsASweep(t *testing.T) {
	implementations := t.TempDir()
	if err := os.MkdirAll(filepath.Join(implementations, "impl-01"), 0o755); err != nil {
		t.Fatal(err)
	}
	output := t.TempDir()
	if err := os.WriteFile(filepath.Join(output, manifestFileName), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	configuration := config{
		implementationsDirectory: implementations,
		outputDirectory:          output,
		basePort:                 5300,
		concurrency:              1,
	}
	if err := runSweep(t.Context(), configuration, os.Stdout); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Fatalf("got %v, want a refusal to reuse the directory", err)
	}
}
