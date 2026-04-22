package inspect

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/priyanshujain/sanderling/internal/trace"
)

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
