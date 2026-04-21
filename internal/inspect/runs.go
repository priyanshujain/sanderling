package inspect

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/priyanshujain/uatu/internal/trace"
)

// Maximum size of a single trace.jsonl line. Hierarchies and snapshots
// can be large; 16 MiB is enough headroom for realistic traces.
const maxScanTokenSize = 16 * 1024 * 1024

// RunSummary is the lightweight per-run record returned by Scan and the
// /api/runs handler. Keep this in lockstep with the JSON shape consumed
// by the SPA's run list view.
type RunSummary struct {
	ID              string     `json:"id"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	SpecPath        string     `json:"spec_path"`
	Seed            int64      `json:"seed"`
	Platform        string     `json:"platform"`
	BundleID        string     `json:"bundle_id"`
	DurationMillis  int64      `json:"duration_millis"`
	StepCount       int        `json:"step_count"`
	ViolationCount  int        `json:"violation_count"`
	InProgress      bool       `json:"in_progress"`
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

func readMeta(runDirectory string) (trace.Meta, error) {
	body, err := os.ReadFile(filepath.Join(runDirectory, "meta.json"))
	if err != nil {
		return trace.Meta{}, fmt.Errorf("read meta: %w", err)
	}
	var meta trace.Meta
	if err := json.Unmarshal(body, &meta); err != nil {
		return trace.Meta{}, fmt.Errorf("decode meta: %w", err)
	}
	return meta, nil
}

func tallyTrace(tracePath string) (steps, violations int, err error) {
	file, err := os.Open(tracePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("open trace: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxScanTokenSize)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var partial struct {
			Violations []string `json:"violations,omitempty"`
		}
		if err := json.Unmarshal(line, &partial); err != nil {
			return 0, 0, fmt.Errorf("decode step: %w", err)
		}
		steps++
		violations += len(partial.Violations)
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, fmt.Errorf("scan trace: %w", err)
	}
	return steps, violations, nil
}

// Cache holds parsed Run records keyed by id. Open returns a fresh parse
// when the underlying trace.jsonl mtime changes.
type Cache struct {
	root  string
	mutex sync.Mutex
	runs  map[string]*Run
}

func NewCache(runsDirectory string) *Cache {
	return &Cache{root: runsDirectory, runs: map[string]*Run{}}
}

func (c *Cache) Root() string { return c.root }

// Open parses (or returns a cached parse of) the run named id.
func (c *Cache) Open(id string) (*Run, error) {
	if !validRunID(id) {
		return nil, fs.ErrNotExist
	}
	runDirectory := filepath.Join(c.root, id)
	tracePath := filepath.Join(runDirectory, "trace.jsonl")
	traceInfo, traceErr := os.Stat(tracePath)

	c.mutex.Lock()
	defer c.mutex.Unlock()
	if cached, ok := c.runs[id]; ok {
		if traceErr == nil && cached.traceMtime.Equal(traceInfo.ModTime()) {
			return cached, nil
		}
	}
	run, err := parseRun(runDirectory, id)
	if err != nil {
		return nil, err
	}
	c.runs[id] = run
	return run, nil
}

func parseRun(runDirectory, id string) (*Run, error) {
	meta, err := readMeta(runDirectory)
	if err != nil {
		return nil, err
	}
	tracePath := filepath.Join(runDirectory, "trace.jsonl")
	steps, offsets, violationCount, traceMtime, err := scanSteps(tracePath)
	if err != nil {
		return nil, err
	}
	summary := buildSummary(id, meta, len(steps), violationCount)
	return &Run{
		ID:         id,
		Directory:  runDirectory,
		Meta:       meta,
		Summary:    summary,
		Steps:      steps,
		tracePath:  tracePath,
		traceMtime: traceMtime,
		offsets:    offsets,
	}, nil
}

func scanSteps(tracePath string) ([]StepSummary, []int64, int, time.Time, error) {
	file, err := os.Open(tracePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []StepSummary{}, nil, 0, time.Time{}, nil
		}
		return nil, nil, 0, time.Time{}, fmt.Errorf("open trace: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, nil, 0, time.Time{}, fmt.Errorf("stat trace: %w", err)
	}
	reader := bufio.NewReaderSize(file, 64*1024)
	steps := []StepSummary{}
	offsets := []int64{}
	violationCount := 0
	var offset int64
	for {
		lineStart := offset
		line, err := reader.ReadBytes('\n')
		offset += int64(len(line))
		trimmed := line
		if len(trimmed) > 0 && trimmed[len(trimmed)-1] == '\n' {
			trimmed = trimmed[:len(trimmed)-1]
		}
		if len(trimmed) > 0 {
			summary, partial, decodeErr := decodeStepSummary(trimmed)
			if decodeErr != nil {
				return nil, nil, 0, time.Time{}, decodeErr
			}
			steps = append(steps, summary)
			offsets = append(offsets, lineStart)
			violationCount += partial
		}
		if err != nil {
			break
		}
	}
	return steps, offsets, violationCount, info.ModTime(), nil
}

func decodeStepSummary(line []byte) (StepSummary, int, error) {
	var partial struct {
		Index     int       `json:"step"`
		Timestamp time.Time `json:"timestamp"`
		Screen    string    `json:"screen,omitempty"`
		Action    *struct {
			Kind           string `json:"kind"`
			X              int    `json:"x,omitempty"`
			Y              int    `json:"y,omitempty"`
			FromX          int    `json:"from_x,omitempty"`
			FromY          int    `json:"from_y,omitempty"`
			ToX            int    `json:"to_x,omitempty"`
			ToY            int    `json:"to_y,omitempty"`
			Key            string `json:"key,omitempty"`
			Text           string `json:"text,omitempty"`
			Selector       string `json:"selector,omitempty"`
			DurationMillis int    `json:"duration_millis,omitempty"`
		} `json:"action,omitempty"`
		Exceptions []json.RawMessage `json:"exceptions,omitempty"`
		Violations []string          `json:"violations,omitempty"`
	}
	if err := json.Unmarshal(line, &partial); err != nil {
		return StepSummary{}, 0, fmt.Errorf("decode step: %w", err)
	}
	summary := StepSummary{
		Index:         partial.Index,
		Timestamp:     partial.Timestamp,
		Screen:        partial.Screen,
		HasViolations: len(partial.Violations) > 0,
		HasExceptions: len(partial.Exceptions) > 0,
	}
	if partial.Action != nil {
		summary.ActionKind = partial.Action.Kind
		switch partial.Action.Kind {
		case "Tap":
			if partial.Action.Selector != "" {
				summary.ActionLabel = partial.Action.Selector
			} else if partial.Action.Text != "" {
				summary.ActionLabel = partial.Action.Text
			} else if partial.Action.X != 0 || partial.Action.Y != 0 {
				summary.ActionLabel = fmt.Sprintf("(%d,%d)", partial.Action.X, partial.Action.Y)
			}
		case "InputText":
			summary.ActionLabel = fmt.Sprintf("%q", partial.Action.Text)
		case "Swipe":
			summary.ActionLabel = swipeDirectionLabel(
				partial.Action.FromX, partial.Action.FromY,
				partial.Action.ToX, partial.Action.ToY,
			)
		case "PressKey":
			summary.ActionLabel = partial.Action.Key
		case "Wait":
			if partial.Action.DurationMillis > 0 {
				summary.ActionLabel = fmt.Sprintf("%dms", partial.Action.DurationMillis)
			}
		}
	}
	return summary, len(partial.Violations), nil
}

func swipeDirectionLabel(fromX, fromY, toX, toY int) string {
	dx := toX - fromX
	dy := toY - fromY
	absX := dx
	if absX < 0 {
		absX = -absX
	}
	absY := dy
	if absY < 0 {
		absY = -absY
	}
	if absY >= absX {
		if dy < 0 {
			return "up"
		}
		return "down"
	}
	if dx < 0 {
		return "left"
	}
	return "right"
}

// Step decodes the full Step record at index n (1-based, matching trace.Step.Index).
func (c *Cache) Step(run *Run, index int) (trace.Step, error) {
	position := -1
	for i, summary := range run.Steps {
		if summary.Index == index {
			position = i
			break
		}
	}
	if position == -1 {
		return trace.Step{}, fs.ErrNotExist
	}
	file, err := os.Open(run.tracePath)
	if err != nil {
		return trace.Step{}, fmt.Errorf("open trace: %w", err)
	}
	defer file.Close()
	if _, err := file.Seek(run.offsets[position], 0); err != nil {
		return trace.Step{}, fmt.Errorf("seek trace: %w", err)
	}
	reader := bufio.NewReaderSize(file, 64*1024)
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return trace.Step{}, fmt.Errorf("read step line: %w", err)
	}
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	var step trace.Step
	if err := json.Unmarshal(line, &step); err != nil {
		return trace.Step{}, fmt.Errorf("decode step: %w", err)
	}
	return step, nil
}

// Detail returns the /api/runs/{id} payload.
func (c *Cache) Detail(id string) (RunDetail, error) {
	run, err := c.Open(id)
	if err != nil {
		return RunDetail{}, err
	}
	return RunDetail{
		RunSummary: run.Summary,
		Meta:       run.Meta,
		Steps:      run.Steps,
	}, nil
}

func validRunID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}
