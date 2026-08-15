package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	manifestFileName  = "campaign.json"
	recordsFileName   = "runs.jsonl"
	seedPlaceholder   = "{seed}"
	devicePlaceholder = "{device}"
)

// manifest is campaign.json: what the campaign INTENDED to run, written before
// the first run so a host that dropped runs shows up as missing seeds rather
// than as a smaller sample.
type manifest struct {
	Arm               string    `json:"arm"`
	Generator         string    `json:"generator"`
	LabelSource       string    `json:"label_source"`
	Platform          string    `json:"platform"`
	SpecPath          string    `json:"spec_path"`
	BundleID          string    `json:"bundle_id"`
	MaxSteps          int       `json:"max_steps"`
	DurationMillis    int64     `json:"duration_millis"`
	RunTimeoutMillis  int64     `json:"run_timeout_millis"`
	Seeds             []int64   `json:"seeds"`
	Devices           []string  `json:"devices"`
	Host              string    `json:"host"`
	SanderlingPath    string    `json:"sanderling_path"`
	SanderlingVersion string    `json:"sanderling_version"`
	StartedAt         time.Time `json:"started_at"`
	ArgumentTemplate  []string  `json:"argument_template"`
}

func buildManifest(configuration config, host, binaryPath, version string, startedAt time.Time) manifest {
	devices := configuration.devices
	if devices == nil {
		devices = []string{}
	}
	return manifest{
		Arm:               configuration.arm,
		Generator:         configuration.generator,
		LabelSource:       configuration.labelSource,
		Platform:          configuration.platform,
		SpecPath:          configuration.specPath,
		BundleID:          configuration.bundleID,
		MaxSteps:          configuration.maxSteps,
		DurationMillis:    configuration.duration.Milliseconds(),
		RunTimeoutMillis:  configuration.runTimeout.Milliseconds(),
		Seeds:             configuration.seeds,
		Devices:           devices,
		Host:              host,
		SanderlingPath:    binaryPath,
		SanderlingVersion: version,
		StartedAt:         startedAt,
		ArgumentTemplate:  runArguments(configuration, seedPlaceholder, devicePlaceholder),
	}
}

func writeManifest(directory string, value manifest) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return os.WriteFile(filepath.Join(directory, manifestFileName), append(body, '\n'), 0o644)
}
