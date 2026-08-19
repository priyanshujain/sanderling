package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	sweepManifestFileName    = "sweep.json"
	sweepRecordsFileName     = "implementations.jsonl"
	campaignManifestFileName = "campaign.json"
	campaignRecordsFileName  = "runs.jsonl"
	traceFileName            = "trace.jsonl"
	surfacesExtractor        = "locatableSurfaces"
	maxRecordBytes           = 4 * 1024 * 1024
	maxTraceLineBytes        = 16 * 1024 * 1024
)

// Run exclusion reasons, kept in the vocabulary analyze already uses so the two
// tools describe the same run the same way.
const (
	reasonLaunchError = "launch error"
	reasonTimedOut    = "timed out"
	reasonNonzeroExit = "nonzero exit"
	reasonTraceError  = "unreadable trace"
)

type sweepManifest struct {
	SpecPath        string `json:"spec_path"`
	Implementations []struct {
		Name string `json:"name"`
	} `json:"implementations"`
}

type sweepRunRecord struct {
	Seed              int64  `json:"seed"`
	ExitCode          int    `json:"exit_code"`
	LaunchError       string `json:"launch_error"`
	CampaignDirectory string `json:"campaign_directory"`
}

type sweepImplementationRecord struct {
	Name        string           `json:"implementation"`
	FailedStage string           `json:"failed_stage"`
	Error       string           `json:"error"`
	Runs        []sweepRunRecord `json:"runs"`
}

type campaignRunRecord struct {
	Seed               int64    `json:"seed"`
	ExitCode           int      `json:"exit_code"`
	LaunchError        string   `json:"launch_error"`
	TimedOut           bool     `json:"timed_out"`
	TraceError         string   `json:"trace_error"`
	RunDirectory       string   `json:"run_directory"`
	ViolatedProperties []string `json:"violated_properties"`
}

// checkerVerdict is one implementation's whole checker side, pooled across the
// seeds it was swept at.
type checkerVerdict struct {
	Implementation   string
	FailedStage      string
	FailedError      string
	RunsRecorded     int
	RunsUsable       int
	ExcludedByReason map[string]int
	FiredProperties  []string
	// SurfacesObserved holds every locatable surface seen true on at least one
	// step of at least one usable run. A surface missing from it was never
	// located across the whole sweep of this implementation.
	SurfacesObserved map[string]bool
	SurfacesKnown    bool
	TraceErrors      []string
}

func (v checkerVerdict) fired() bool { return len(v.FiredProperties) > 0 }

type checkerSide struct {
	Directory string
	SpecPath  string
	Planned   []string
	Verdicts  []checkerVerdict
}

func loadChecker(directory string) (checkerSide, error) {
	body, err := os.ReadFile(filepath.Join(directory, sweepManifestFileName))
	if err != nil {
		return checkerSide{}, fmt.Errorf("read %s: %w", sweepManifestFileName, err)
	}
	var declared sweepManifest
	if err := json.Unmarshal(body, &declared); err != nil {
		return checkerSide{}, fmt.Errorf("parse %s in %s: %w", sweepManifestFileName, directory, err)
	}
	side := checkerSide{Directory: directory, SpecPath: declared.SpecPath}
	for _, planned := range declared.Implementations {
		side.Planned = append(side.Planned, planned.Name)
	}

	records, err := readSweepRecords(filepath.Join(directory, sweepRecordsFileName))
	if err != nil {
		return checkerSide{}, err
	}
	for _, record := range records {
		verdict, err := readImplementation(directory, record)
		if err != nil {
			return checkerSide{}, err
		}
		side.Verdicts = append(side.Verdicts, verdict)
	}
	slices.SortFunc(side.Verdicts, func(a, b checkerVerdict) int {
		return strings.Compare(a.Implementation, b.Implementation)
	})
	return side, nil
}

func readSweepRecords(path string) ([]sweepImplementationRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", sweepRecordsFileName, err)
	}
	defer file.Close()

	var records []sweepImplementationRecord
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxRecordBytes)
	lineNumber := 0
	seen := map[string]bool{}
	for scanner.Scan() {
		lineNumber++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		var record sweepImplementationRecord
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", sweepRecordsFileName, lineNumber, err)
		}
		if record.Name == "" {
			return nil, fmt.Errorf("%s line %d names no implementation", sweepRecordsFileName, lineNumber)
		}
		if seen[record.Name] {
			return nil, fmt.Errorf("%s line %d records %s a second time: its runs would be pooled twice",
				sweepRecordsFileName, lineNumber, record.Name)
		}
		seen[record.Name] = true
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", sweepRecordsFileName, err)
	}
	return records, nil
}

func readImplementation(sweepDirectory string, record sweepImplementationRecord) (checkerVerdict, error) {
	verdict := checkerVerdict{
		Implementation:   record.Name,
		FailedStage:      record.FailedStage,
		FailedError:      record.Error,
		ExcludedByReason: map[string]int{},
		SurfacesObserved: map[string]bool{},
	}
	fired := map[string]bool{}
	for _, run := range record.Runs {
		verdict.RunsRecorded++
		if reason := sweepRunExcludedBecause(run); reason != "" {
			verdict.ExcludedByReason[reason]++
			continue
		}
		directory := resolveCampaignDirectory(sweepDirectory, record.Name, run)
		campaignRuns, err := readCampaignRuns(directory)
		if err != nil {
			return checkerVerdict{}, err
		}
		for _, campaignRun := range campaignRuns {
			if reason := excludedBecause(campaignRun); reason != "" {
				verdict.ExcludedByReason[reason]++
				continue
			}
			verdict.RunsUsable++
			for _, property := range campaignRun.ViolatedProperties {
				fired[property] = true
			}
			observed, err := readObservedSurfaces(filepath.Join(directory, campaignRun.RunDirectory))
			if err != nil {
				verdict.TraceErrors = append(verdict.TraceErrors, err.Error())
				continue
			}
			verdict.SurfacesKnown = true
			for surface, seen := range observed {
				if seen {
					verdict.SurfacesObserved[surface] = true
				}
			}
		}
	}
	if len(fired) > 0 {
		verdict.FiredProperties = slices.Sorted(maps.Keys(fired))
	}
	if len(verdict.ExcludedByReason) == 0 {
		verdict.ExcludedByReason = nil
	}
	return verdict, nil
}

// resolveCampaignDirectory prefers the path the sweep recorded and falls back to
// the layout it names, so a sweep directory read on another machine than the one
// that wrote its absolute paths still resolves.
func resolveCampaignDirectory(sweepDirectory, name string, run sweepRunRecord) string {
	if run.CampaignDirectory != "" {
		if _, err := os.Stat(run.CampaignDirectory); err == nil {
			return run.CampaignDirectory
		}
	}
	return filepath.Join(sweepDirectory, name, "seed-"+strconv.FormatInt(run.Seed, 10))
}

func readCampaignRuns(directory string) ([]campaignRunRecord, error) {
	if _, err := os.Stat(filepath.Join(directory, campaignManifestFileName)); err != nil {
		return nil, fmt.Errorf("read %s in %s: %w", campaignManifestFileName, directory, err)
	}
	file, err := os.Open(filepath.Join(directory, campaignRecordsFileName))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", campaignRecordsFileName, err)
	}
	defer file.Close()

	var records []campaignRunRecord
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxRecordBytes)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		var record campaignRunRecord
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return nil, fmt.Errorf("%s line %d in %s: %w", campaignRecordsFileName, lineNumber, directory, err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s in %s: %w", campaignRecordsFileName, directory, err)
	}
	return records, nil
}

// sweepRunExcludedBecause reads the outcome of the campaign process itself,
// which the records inside its directory cannot report. One sweep run is one
// campaign of one seed, so a campaign that died left a runs.jsonl that is
// partial or empty, and scoring the runs it did write reads the seeds it never
// reached as agreement.
func sweepRunExcludedBecause(record sweepRunRecord) string {
	switch {
	case record.LaunchError != "":
		return reasonLaunchError
	case record.ExitCode != 0:
		return reasonNonzeroExit
	default:
		return ""
	}
}

func excludedBecause(record campaignRunRecord) string {
	switch {
	case record.LaunchError != "":
		return reasonLaunchError
	case record.TimedOut:
		return reasonTimedOut
	case record.ExitCode != 0:
		return reasonNonzeroExit
	case record.TraceError != "":
		return reasonTraceError
	default:
		return ""
	}
}

type traceLine struct {
	ExtractorChanges map[string]struct {
		Curr json.RawMessage `json:"curr"`
	} `json:"extractor_changes"`
}

// readObservedSurfaces replays one run's locatableSurfaces readings. The
// verifier emits every extractor as a change on the first snapshot and only on
// a difference afterwards, so a surface true on any recorded change was located
// at least once, and one absent from every change was never located at all.
func readObservedSurfaces(runDirectory string) (map[string]bool, error) {
	path := filepath.Join(runDirectory, traceFileName)
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer file.Close()

	observed := map[string]bool{}
	found := false
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxTraceLineBytes)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		var line traceLine
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, lineNumber, err)
		}
		change, present := line.ExtractorChanges[surfacesExtractor]
		if !present || len(change.Curr) == 0 {
			continue
		}
		var reading map[string]bool
		if err := json.Unmarshal(change.Curr, &reading); err != nil {
			return nil, fmt.Errorf("%s line %d: %s is not an object of booleans: %w",
				path, lineNumber, surfacesExtractor, err)
		}
		found = true
		for surface, located := range reading {
			if located {
				observed[surface] = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if !found {
		return nil, fmt.Errorf("%s records no %s reading: this run cannot say whether a surface was located",
			path, surfacesExtractor)
	}
	return observed, nil
}
