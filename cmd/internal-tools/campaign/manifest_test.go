package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestBuildManifest_RecordsIntendedRunsAndTemplate(t *testing.T) {
	configuration, err := parseArguments(append(baseArguments(), "--devices", "a,b", "--duration", "90s"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	startedAt := time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)
	value := buildManifest(configuration, "worker-7", "/usr/local/bin/sanderling", "1.4.2", startedAt)

	if !slices.Equal(value.Seeds, []int64{1, 2, 3}) {
		t.Errorf("seeds: got %v", value.Seeds)
	}
	if value.Host != "worker-7" || value.SanderlingVersion != "1.4.2" || value.SanderlingPath != "/usr/local/bin/sanderling" {
		t.Errorf("provenance: %+v", value)
	}
	if value.DurationMillis != 90_000 || value.MaxSteps != 300 {
		t.Errorf("budget: duration=%d max_steps=%d", value.DurationMillis, value.MaxSteps)
	}
	if !value.StartedAt.Equal(startedAt) {
		t.Errorf("started_at: got %s", value.StartedAt)
	}
	if got := argumentValue(value.ArgumentTemplate, "--seed"); got != seedPlaceholder {
		t.Errorf("template seed: got %q, want %q", got, seedPlaceholder)
	}
	if got := argumentValue(value.ArgumentTemplate, "--device"); got != devicePlaceholder {
		t.Errorf("template device: got %q, want %q", got, devicePlaceholder)
	}
	if got := argumentValue(value.ArgumentTemplate, "--output"); got != "/campaigns/a/seed-"+seedPlaceholder {
		t.Errorf("template output: got %q", got)
	}
}

func TestWriteManifest_RecordsTheLabelSourceCell(t *testing.T) {
	configuration, err := parseArguments(append(baseArguments(), "--label-source", "resource-id"), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := writeManifest(directory, buildManifest(configuration, "host", "sanderling", "dev", time.Now())); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(directory, manifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		LabelSource string `json:"label_source"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.LabelSource != "resource-id" {
		t.Errorf("label_source: got %q, want resource-id", decoded.LabelSource)
	}
}

func TestBuildManifest_EmptyDeviceListSerializesAsArray(t *testing.T) {
	configuration, err := parseArguments(baseArguments(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := writeManifest(directory, buildManifest(configuration, "host", "sanderling", "dev", time.Now())); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(directory, manifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Devices []string `json:"devices"`
		Arm     string   `json:"arm"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Devices == nil || len(decoded.Devices) != 0 {
		t.Errorf("devices: got %v, want []", decoded.Devices)
	}
	if decoded.Arm != "seeded-baseline" {
		t.Errorf("arm: got %q", decoded.Arm)
	}
}
