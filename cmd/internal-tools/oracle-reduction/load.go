package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/priyanshujain/sanderling/internal/trace"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

const maxStepBytes = 64 * 1024 * 1024

// loadedRun is one run directory: its meta, the steps in file order, and the
// synthetic end-of-run record the runner writes when Finalize convicted a
// liveness obligation.
type loadedRun struct {
	Directory string
	Meta      trace.Meta
	Steps     []trace.Step
	Finalize  *trace.Step
}

// loadRun reads a run directory and refuses anything E3 cannot replay. A step
// written before the format change carries no element depths, so its hierarchy
// decodes with a nil root and every selector resolves to nothing: the refusal
// has to name the version rather than let the replay report an empty screen.
func loadRun(directory string) (loadedRun, error) {
	metaBody, err := os.ReadFile(filepath.Join(directory, "meta.json"))
	if err != nil {
		return loadedRun{}, fmt.Errorf("read meta: %w", err)
	}
	var meta trace.Meta
	if err := json.Unmarshal(metaBody, &meta); err != nil {
		return loadedRun{}, fmt.Errorf("decode meta: %w", err)
	}

	file, err := os.Open(filepath.Join(directory, "trace.jsonl"))
	if err != nil {
		return loadedRun{}, fmt.Errorf("open trace: %w", err)
	}
	defer file.Close()

	run := loadedRun{Directory: directory, Meta: meta}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), maxStepBytes)
	line := 0
	for scanner.Scan() {
		line++
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var step trace.Step
		if err := json.Unmarshal(scanner.Bytes(), &step); err != nil {
			return loadedRun{}, fmt.Errorf(
				"decode step on line %d: %w",
				line,
				err,
			)
		}
		if step.TraceVersion != trace.TraceVersion {
			return loadedRun{}, fmt.Errorf(
				"step %d is trace_version %d, and E3 replays version %d only: "+
					"an older step stores no element depths, so its hierarchy decodes "+
					"with a nil root and every selector resolves to nothing on replay",
				step.Index, step.TraceVersion, trace.TraceVersion)
		}
		run.Steps = append(run.Steps, step)
	}
	if err := scanner.Err(); err != nil {
		return loadedRun{}, fmt.Errorf("read trace: %w", err)
	}
	if len(run.Steps) == 0 {
		return loadedRun{}, fmt.Errorf("trace has no steps")
	}
	if last := run.Steps[len(run.Steps)-1]; len(run.Steps) > 1 &&
		last.Hierarchy == nil &&
		len(last.Violations) > 0 {
		run.Finalize = &last
		run.Steps = run.Steps[:len(run.Steps)-1]
	}
	if meta.Seed == 0 {
		return loadedRun{}, fmt.Errorf(
			"meta records no seed, so the run's bundle cannot be reproduced",
		)
	}
	return run, nil
}

// finalizeIndex is the step index the runner gives its end-of-run record: one
// past the last step it wrote.
func (r loadedRun) finalizeIndex() int {
	return r.Steps[len(r.Steps)-1].Index + 1
}

// extractorFold reconstructs every extractor's value at each step from the
// recorded per-step diffs. A run's trace stores what changed, so the value an
// extractor held at a step is the last change at or before it; an extractor
// that never changed held JSON null throughout, which is what an unwritten
// diff means.
func extractorFold(
	steps []trace.Step,
	names []string,
) ([]map[int]json.RawMessage, error) {
	index := make(map[string]int, len(names))
	for position, name := range names {
		index[name] = position
	}
	current := make(map[int]json.RawMessage, len(names))
	for position := range names {
		current[position] = json.RawMessage("null")
	}
	folded := make([]map[int]json.RawMessage, len(steps))
	for step := range steps {
		for name, change := range steps[step].ExtractorChanges {
			position, ok := index[name]
			if !ok {
				return nil, fmt.Errorf(
					"step %d records extractor %q, which the spec does not register; "+
						"the trace and the spec are not the same bundle",
					steps[step].Index,
					name,
				)
			}
			current[position] = change.Curr
		}
		snapshot := make(map[int]json.RawMessage, len(current))
		for position, value := range current {
			snapshot[position] = value
		}
		folded[step] = snapshot
	}
	return folded, nil
}

// lastActionFor rebuilds the action the runner had applied before the next
// step observed. An action the runner chose but never dispatched left
// state.lastAction null, and the recorded skip reason is what says so.
func lastActionFor(step trace.Step) *verifier.Action {
	if step.NextAction == nil || step.ActionSkipped != "" {
		return nil
	}
	recorded := *step.NextAction
	action := verifier.Action{
		Kind:           verifier.ActionKind(recorded.Kind),
		On:             recorded.Selector,
		Text:           recorded.Text,
		X:              recorded.X,
		Y:              recorded.Y,
		FromX:          recorded.FromX,
		FromY:          recorded.FromY,
		ToX:            recorded.ToX,
		ToY:            recorded.ToY,
		Key:            recorded.Key,
		DurationMillis: recorded.DurationMillis,
	}
	return &action
}

func traceLogs(entries []trace.LogEntry) []verifier.LogEntry {
	if len(entries) == 0 {
		return nil
	}
	logs := make([]verifier.LogEntry, 0, len(entries))
	for _, entry := range entries {
		logs = append(logs, verifier.LogEntry{
			UnixMillis: entry.UnixMillis,
			Level:      entry.Level,
			Tag:        entry.Tag,
			Message:    entry.Message,
		})
	}
	return logs
}

func traceExceptions(entries []trace.Exception) []verifier.Exception {
	if len(entries) == 0 {
		return nil
	}
	exceptions := make([]verifier.Exception, 0, len(entries))
	for _, entry := range entries {
		exceptions = append(exceptions, verifier.Exception{
			Class:      entry.Class,
			Message:    entry.Message,
			StackTrace: entry.StackTrace,
			UnixMillis: entry.UnixMillis,
		})
	}
	return exceptions
}

// discoverRuns finds every run directory at or below root, a run directory
// being one holding both meta.json and trace.jsonl.
func discoverRuns(root string) ([]string, error) {
	var directories []string
	err := filepath.Walk(
		root,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				return nil
			}
			if _, statErr := os.Stat(filepath.Join(path, "trace.jsonl")); statErr != nil {
				return nil
			}
			if _, statErr := os.Stat(filepath.Join(path, "meta.json")); statErr != nil {
				return nil
			}
			directories = append(directories, path)
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	sort.Strings(directories)
	return directories, nil
}
