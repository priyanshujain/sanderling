// Package tracecorpus loads recorded runs for measures that read stored
// traces with no device attached.
package tracecorpus

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/priyanshujain/sanderling/internal/trace"
)

const maxStepBytes = 64 * 1024 * 1024

// Run is one run directory: its meta and every step in file order, including
// the synthetic end-of-run record a finalised liveness obligation lands on.
type Run struct {
	Directory string
	Meta      trace.Meta
	Steps     []trace.Step
}

// Load reads a run directory and refuses anything an offline measure cannot
// read. A step written before the format change stores no element depths, so
// its hierarchy decodes with a nil root: a structural hash over it is the
// empty string for every screen, and every run would look identical to every
// other. The refusal has to name the version rather than report that number.
func Load(directory string) (Run, error) {
	metaBody, err := os.ReadFile(filepath.Join(directory, "meta.json"))
	if err != nil {
		return Run{}, fmt.Errorf("read meta: %w", err)
	}
	var meta trace.Meta
	if err := json.Unmarshal(metaBody, &meta); err != nil {
		return Run{}, fmt.Errorf("decode meta: %w", err)
	}

	file, err := os.Open(filepath.Join(directory, "trace.jsonl"))
	if err != nil {
		return Run{}, fmt.Errorf("open trace: %w", err)
	}
	defer file.Close()

	run := Run{Directory: directory, Meta: meta}
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
			return Run{}, fmt.Errorf("decode step on line %d: %w", line, err)
		}
		if step.TraceVersion != trace.TraceVersion {
			return Run{}, fmt.Errorf(
				"step %d is trace_version %d and an offline measure reads version %d only: "+
					"an older step stores no element depths, no logs and no exceptions, so "+
					"its hierarchy decodes with a nil root and nothing retrofits it",
				step.Index, step.TraceVersion, trace.TraceVersion)
		}
		if step.Hierarchy != nil && len(step.Hierarchy.Elements) > 0 &&
			step.Hierarchy.Root == nil {
			return Run{}, fmt.Errorf(
				"step %d stores %d elements and no tree shape, so its hierarchy "+
					"cannot be rebuilt",
				step.Index, len(step.Hierarchy.Elements))
		}
		run.Steps = append(run.Steps, step)
	}
	if err := scanner.Err(); err != nil {
		return Run{}, fmt.Errorf("read trace: %w", err)
	}
	if len(run.Steps) == 0 {
		return Run{}, fmt.Errorf("trace has no steps")
	}
	return run, nil
}

// Discover finds every run directory at or below root, a run directory being
// one holding both meta.json and trace.jsonl.
func Discover(root string) ([]string, error) {
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
