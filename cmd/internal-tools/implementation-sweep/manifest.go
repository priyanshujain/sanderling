package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	manifestFileName = "sweep.json"
	recordsFileName  = "implementations.jsonl"
	seedPlaceholder  = "{seed}"
)

// plannedImplementation is one implementation the sweep intends to run, with
// the port it is served on and the URL every seed is served at.
type plannedImplementation struct {
	Name        string `json:"name"`
	Directory   string `json:"directory"`
	Port        int    `json:"port"`
	URLTemplate string `json:"url_template"`
}

// manifest is sweep.json: what the sweep INTENDED to run, written before the
// first install so a host that dropped an implementation or a seed shows up as
// a missing run rather than as a smaller sample.
type manifest struct {
	Generator       string                  `json:"generator"`
	Platform        string                  `json:"platform"`
	SpecPath        string                  `json:"spec_path"`
	MaxSteps        int                     `json:"max_steps"`
	DurationMillis  int64                   `json:"duration_millis"`
	Seeds           []int64                 `json:"seeds"`
	Implementations []plannedImplementation `json:"implementations"`
	Concurrency     int                     `json:"concurrency"`
	Host            string                  `json:"host"`
	BunPath         string                  `json:"bun_path"`
	CampaignPath    string                  `json:"campaign_path"`
	SanderlingPath  string                  `json:"sanderling_path"`
	StartedAt       time.Time               `json:"started_at"`
}

func buildManifest(
	configuration config,
	implementations []implementation,
	host string,
	startedAt time.Time,
) manifest {
	planned := make([]plannedImplementation, 0, len(implementations))
	for _, target := range implementations {
		planned = append(planned, plannedImplementation{
			Name:        target.Name,
			Directory:   target.Directory,
			Port:        target.Port,
			URLTemplate: servedURL(target.Port, seedPlaceholder),
		})
	}
	return manifest{
		Generator:       generator,
		Platform:        platform,
		SpecPath:        configuration.specPath,
		MaxSteps:        configuration.maxSteps,
		DurationMillis:  configuration.duration.Milliseconds(),
		Seeds:           configuration.seeds,
		Implementations: planned,
		Concurrency:     configuration.concurrency,
		Host:            host,
		BunPath:         configuration.bunPath,
		CampaignPath:    configuration.campaignPath,
		SanderlingPath:  configuration.sanderlingPath,
		StartedAt:       startedAt,
	}
}

func writeManifest(directory string, value manifest) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return os.WriteFile(
		filepath.Join(directory, manifestFileName),
		append(body, '\n'),
		0o644,
	)
}

// runRecord is one campaign, which is one implementation at one seed.
type runRecord struct {
	Seed              int64  `json:"seed"`
	URL               string `json:"url"`
	ExitCode          int    `json:"exit_code"`
	LaunchError       string `json:"launch_error,omitempty"`
	CampaignDirectory string `json:"campaign_directory"`
	MonotonicMillis   int64  `json:"monotonic_millis"`
}

// implementationRecord is one line of implementations.jsonl. FailedStage names
// the step that stopped this implementation, and an implementation that never
// got past install, build or serve carries no runs at all.
type implementationRecord struct {
	Name            string      `json:"implementation"`
	Directory       string      `json:"directory"`
	Port            int         `json:"port"`
	FailedStage     string      `json:"failed_stage,omitempty"`
	Error           string      `json:"error,omitempty"`
	StartedAt       time.Time   `json:"started_at"`
	MonotonicMillis int64       `json:"monotonic_millis"`
	Runs            []runRecord `json:"runs"`
}

const (
	stageInstall = "install"
	stageBuild   = "build"
	stageServe   = "serve"
)
