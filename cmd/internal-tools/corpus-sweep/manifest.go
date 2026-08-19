package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	manifestFileName = "sweep.json"
	recordsFileName  = "implementations.jsonl"
)

// plannedImplementation is one implementation the sweep intends to run, with
// the origin that keeps its stored state its own and the URL every seed is
// driven at.
type plannedImplementation struct {
	Name     string `json:"name"`
	Document string `json:"document"`
	Port     int    `json:"port"`
	Origin   string `json:"origin"`
	URL      string `json:"url"`
}

// manifest is sweep.json: what the sweep INTENDED to run, written before the
// first run so a host that dropped an implementation or a seed shows up as a
// missing run rather than as a smaller sample.
type manifest struct {
	Generator       string                  `json:"generator"`
	Platform        string                  `json:"platform"`
	SpecPath        string                  `json:"spec_path"`
	CorpusRoot      string                  `json:"corpus_root"`
	CorpusCommit    string                  `json:"corpus_commit"`
	MaxSteps        int                     `json:"max_steps"`
	DurationMillis  int64                   `json:"duration_millis"`
	Seeds           []int64                 `json:"seeds"`
	Implementations []plannedImplementation `json:"implementations"`
	Concurrency     int                     `json:"concurrency"`
	Host            string                  `json:"host"`
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
			Name:     target.Name,
			Document: target.Document,
			Port:     target.Port,
			Origin:   target.Origin(),
			URL:      target.URL(),
		})
	}
	return manifest{
		Generator:       generator,
		Platform:        platform,
		SpecPath:        configuration.specPath,
		CorpusRoot:      configuration.corpusRoot,
		CorpusCommit:    corpusCommit(configuration.corpusRoot),
		MaxSteps:        configuration.maxSteps,
		DurationMillis:  configuration.duration.Milliseconds(),
		Seeds:           configuration.seeds,
		Implementations: planned,
		Concurrency:     configuration.concurrency,
		Host:            host,
		CampaignPath:    configuration.campaignPath,
		SanderlingPath:  configuration.sanderlingPath,
		StartedAt:       startedAt,
	}
}

// corpusCommit records which checkout was swept. It is empty rather than fatal
// for a corpus that is not a git working tree, because the population check has
// already established the corpus holds the right examples.
func corpusCommit(corpusRoot string) string {
	command := exec.Command("git", "-C", corpusRoot, "rev-parse", "HEAD")
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
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
	ExitCode          int    `json:"exit_code"`
	LaunchError       string `json:"launch_error,omitempty"`
	CampaignDirectory string `json:"campaign_directory"`
	MonotonicMillis   int64  `json:"monotonic_millis"`
}

// implementationRecord is one line of implementations.jsonl. FailedStage names
// the step that stopped this implementation, and one that never got served
// carries no runs at all.
type implementationRecord struct {
	Name            string      `json:"implementation"`
	Document        string      `json:"document"`
	Port            int         `json:"port"`
	Origin          string      `json:"origin"`
	URL             string      `json:"url"`
	FailedStage     string      `json:"failed_stage,omitempty"`
	Error           string      `json:"error,omitempty"`
	StartedAt       time.Time   `json:"started_at"`
	MonotonicMillis int64       `json:"monotonic_millis"`
	Runs            []runRecord `json:"runs"`
}

const stageServe = "serve"
