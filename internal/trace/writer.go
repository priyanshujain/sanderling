// Package trace records each run's steps, snapshots, and violations to disk for later inspection.
package trace

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

type Step struct {
	Index     int                        `json:"step"`
	Timestamp time.Time                  `json:"timestamp"`
	Screen    string                     `json:"screen,omitempty"`
	Snapshots map[string]json.RawMessage `json:"snapshots,omitempty"`
	// NextAction is the action chosen for the next iteration based on observing this step.
	NextAction       *Action                    `json:"next_action,omitempty"`
	Exceptions       []Exception                `json:"exceptions,omitempty"`
	Violations       []string                   `json:"violations,omitempty"`
	Hierarchy        *hierarchy.Tree            `json:"hierarchy,omitempty"`
	Residuals        map[string]json.RawMessage `json:"residuals,omitempty"`
	Metrics          *Metrics                   `json:"metrics,omitempty"`
	ExtractorChanges map[string]ExtractorChange `json:"extractor_changes,omitempty"`
	// Transitional marks a step whose hierarchy still showed a NavHost
	// cross-fade (multiple route-level *Screen ids) after the runner's
	// retry budget. The verifier is skipped for these steps so transient
	// state does not poison the previous/current extractor advance.
	Transitional bool `json:"transitional,omitempty"`
	// SkippedVerification is set true exactly when the verifier was skipped
	// for this step, so downstream tooling can tell a deliberately-skipped
	// step from one that was verified and came back clean.
	SkippedVerification bool `json:"skipped_verification,omitempty"`
	// Witnesses records the violation witness for each property that newly
	// violated at this step: the cause and the extractor values at onset.
	Witnesses map[string]Witness `json:"witnesses,omitempty"`
}

// Witness is the trace-side record of a property violation: why it fired, the
// two steps a deferred obligation spans, and the extractor values behind it.
type Witness struct {
	Reason  string `json:"reason,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
	// Step is the step the failed obligation originated at: the step that
	// armed it. For a deferred obligation (a next, an eventually) this is
	// earlier than the step at which the failure was detected.
	Step int `json:"step,omitempty"`
	// DetectedStep is the observation whose evaluation produced the violation.
	// Extractors is that step's state, not Step's.
	DetectedStep int                        `json:"detected_step,omitempty"`
	Extractors   map[string]json.RawMessage `json:"extractors,omitempty"`
}

// ExtractorChange records the prev/curr JSON values of an extractor whose
// observation differed between two consecutive steps. Surfaced under
// violation rows in the replay UI as a "what changed at this step"
// breadcrumb.
type ExtractorChange struct {
	Prev json.RawMessage `json:"prev"`
	Curr json.RawMessage `json:"curr"`
}

type Metrics struct {
	CPUPercent       float64 `json:"cpu_percent"`
	HeapBytes        int64   `json:"heap_bytes,omitempty"`
	TotalMemoryBytes int64   `json:"total_memory_bytes,omitempty"`
}

type Action struct {
	Kind           string        `json:"kind"`
	X              int           `json:"x,omitempty"`
	Y              int           `json:"y,omitempty"`
	FromX          int           `json:"from_x,omitempty"`
	FromY          int           `json:"from_y,omitempty"`
	ToX            int           `json:"to_x,omitempty"`
	ToY            int           `json:"to_y,omitempty"`
	Key            string        `json:"key,omitempty"`
	Text           string        `json:"text,omitempty"`
	DurationMillis int           `json:"duration_millis,omitempty"`
	Selector       string        `json:"selector,omitempty"`
	ResolvedBounds *BoundsRecord `json:"resolved_bounds,omitempty"`
	TapPoint       *PointRecord  `json:"tap_point,omitempty"`
	// Source names the backend that chose this action: "llm" when the LLM
	// action backend selected it, empty for the seeded picker. LLMReasoning is
	// the model's short rationale, shown by the replay UI to explain the pick.
	Source       string `json:"source,omitempty"`
	LLMReasoning string `json:"llm_reasoning,omitempty"`
	// LLMChoice is the 1-based number the model picked from the candidate list;
	// LLMChosenAction is the action description it echoed for that number. The
	// runner strict-skips when the echo disagrees with the numbered entry, so on
	// a recorded action the two always agree; the replay UI shows them to
	// confirm the reasoning matched the executed action.
	LLMChoice       int    `json:"llm_choice,omitempty"`
	LLMChosenAction string `json:"llm_chosen_action,omitempty"`
}

type BoundsRecord struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type PointRecord struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type Exception struct {
	Class      string `json:"class"`
	Message    string `json:"message,omitempty"`
	StackTrace string `json:"stack_trace,omitempty"`
	UnixMillis int64  `json:"unix_millis,omitempty"`
}

type Meta struct {
	Seed              int64      `json:"seed"`
	SpecPath          string     `json:"spec_path"`
	BundleSHA256      string     `json:"bundle_sha256"`
	Platform          string     `json:"platform"`
	BundleID          string     `json:"bundle_id"`
	StartedAt         time.Time  `json:"started_at"`
	EndedAt           *time.Time `json:"ended_at,omitempty"`
	SanderlingVersion string     `json:"sanderling_version"`

	// Arm labels the experiment cell this run belongs to, set from --arm. A
	// directory of runs cannot be attributed to a cell after the fact without
	// it, which makes any factorial computed from such a directory unanalysable.
	Arm string `json:"arm,omitempty"`
	// Generator, Model and Instructions record which picker ran and how it was
	// configured. Two runs that differ in any of these are different arms.
	Generator    string `json:"generator,omitempty"`
	Model        string `json:"model,omitempty"`
	Instructions string `json:"instructions,omitempty"`
	// MaxSteps and DurationMillis are the budget the run was given, which has
	// to be identical across arms for a comparison to mean anything.
	MaxSteps       int   `json:"max_steps,omitempty"`
	DurationMillis int64 `json:"duration_millis,omitempty"`
	// Host is the machine that produced the run. Campaigns are split across
	// several hosts, so a per-host effect has to be detectable rather than
	// invisible.
	Host string `json:"host,omitempty"`
}

type Writer struct {
	directory string
	mutex     sync.Mutex
	file      io.WriteCloser
	encoder   *json.Encoder
}

// NewWriter ensures `directory` exists and opens trace.jsonl for append.
// meta.json is written separately via WriteMeta. Caller must Close.
func NewWriter(directory string) (*Writer, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	file, err := os.OpenFile(
		filepath.Join(directory, "trace.jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0o644,
	)
	if err != nil {
		return nil, fmt.Errorf("open trace.jsonl: %w", err)
	}
	encoder := json.NewEncoder(file)
	return &Writer{directory: directory, file: file, encoder: encoder}, nil
}

func (w *Writer) Directory() string { return w.directory }

func (w *Writer) WriteMeta(meta Meta) error {
	body, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	return os.WriteFile(filepath.Join(w.directory, "meta.json"), body, 0o644)
}

func (w *Writer) WriteStep(step Step) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.file == nil {
		return fmt.Errorf("trace: writer is closed")
	}
	return w.encoder.Encode(step)
}

// WriteScreenshot is lock-free: each call writes a distinct, uniquely-named
// file via os.WriteFile and touches no field of Writer, so concurrent calls
// never contend.
func (w *Writer) WriteScreenshot(stepIndex int, png []byte) error {
	return w.writePNG(fmt.Sprintf("step-%05d.png", stepIndex), png)
}

func (w *Writer) writePNG(name string, png []byte) error {
	if len(png) == 0 {
		return nil
	}
	directory := filepath.Join(w.directory, "screenshots")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("mkdir screenshots: %w", err)
	}
	return os.WriteFile(filepath.Join(directory, name), png, 0o644)
}

func (w *Writer) Close() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}
