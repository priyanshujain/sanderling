package inspect

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/priyanshujain/sanderling/internal/trace"
)

// Maximum size of a single trace.jsonl line. Hierarchies and snapshots
// can be large; 16 MiB is enough headroom for realistic traces.
const maxScanTokenSize = 16 * 1024 * 1024

// RunSummary is the lightweight per-run record returned by Scan and the
// /api/runs handler. Keep this in lockstep with the JSON shape consumed
// by the SPA's run list view.
type RunSummary struct {
	ID             string     `json:"id"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	SpecPath       string     `json:"spec_path"`
	Seed           int64      `json:"seed"`
	Platform       string     `json:"platform"`
	BundleID       string     `json:"bundle_id"`
	DurationMillis int64      `json:"duration_millis"`
	StepCount      int        `json:"step_count"`
	ViolationCount int        `json:"violation_count"`
	InProgress     bool       `json:"in_progress"`
}

// StepSummary is the slim per-step record used to render the step list
// and timeline. Heavy fields (hierarchy, snapshots, residuals) are sent
// only via the per-step endpoint.
type StepSummary struct {
	Index         int       `json:"index"`
	Timestamp     time.Time `json:"timestamp"`
	Screen        string    `json:"screen,omitempty"`
	ActionKind    string    `json:"action_kind,omitempty"`
	ActionLabel   string    `json:"action_label,omitempty"`
	HasViolations bool      `json:"has_violations"`
	HasExceptions bool      `json:"has_exceptions"`
}

// RunDetail is the full /api/runs/{id} payload: meta + slim step list.
type RunDetail struct {
	RunSummary
	Meta  trace.Meta    `json:"meta"`
	Steps []StepSummary `json:"steps"`
}

// Run is a cached parse of one run directory. Step lookups re-read the
// JSONL file from disk; only the line offsets are kept in memory.
type Run struct {
	ID         string
	Directory  string
	Meta       trace.Meta
	Summary    RunSummary
	Steps      []StepSummary
	tracePath  string
	traceMtime time.Time
	offsets    []int64
}

// IsRunDirectory reports whether dir looks like a single run (has meta.json).
func IsRunDirectory(directory string) bool {
	_, err := os.Stat(filepath.Join(directory, "meta.json"))
	return err == nil
}

// Scan walks runsDirectory and returns one RunSummary per child directory
// that contains meta.json. Results are sorted by StartedAt descending.
func Scan(runsDirectory string) ([]RunSummary, error) {
	entries, err := os.ReadDir(runsDirectory)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []RunSummary{}, nil
		}
		return nil, fmt.Errorf("read runs dir: %w", err)
	}
	summaries := make([]RunSummary, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		runDirectory := filepath.Join(runsDirectory, entry.Name())
		summary, err := summarize(runDirectory, entry.Name())
		if err != nil {
			continue
		}
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].StartedAt.After(summaries[j].StartedAt)
	})
	return summaries, nil
}

func summarize(runDirectory, id string) (RunSummary, error) {
	meta, err := readMeta(runDirectory)
	if err != nil {
		return RunSummary{}, err
	}
	stepCount, violationCount, err := tallyTrace(filepath.Join(runDirectory, "trace.jsonl"))
	if err != nil {
		return RunSummary{}, err
	}
	return buildSummary(id, meta, stepCount, violationCount), nil
}

func buildSummary(id string, meta trace.Meta, stepCount, violationCount int) RunSummary {
	summary := RunSummary{
		ID:             id,
		StartedAt:      meta.StartedAt,
		EndedAt:        meta.EndedAt,
		SpecPath:       meta.SpecPath,
		Seed:           meta.Seed,
		Platform:       meta.Platform,
		BundleID:       meta.BundleID,
		StepCount:      stepCount,
		ViolationCount: violationCount,
		InProgress:     meta.EndedAt == nil,
	}
	if meta.EndedAt != nil {
		summary.DurationMillis = meta.EndedAt.Sub(meta.StartedAt).Milliseconds()
	}
	return summary
}
