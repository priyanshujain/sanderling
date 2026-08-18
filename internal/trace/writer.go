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

// TraceVersion is stamped on every step this build writes. A step decoding to
// version 0 predates the fields introduced with version 1 (the tree's stored
// depths, per-step logs, per-step exceptions), which is what separates "this
// trace cannot answer the question" from "this step had nothing to report".
const TraceVersion = 1

type Step struct {
	Index        int                        `json:"step"`
	TraceVersion int                        `json:"trace_version,omitempty"`
	Timestamp    time.Time                  `json:"timestamp"`
	Screen       string                     `json:"screen,omitempty"`
	Snapshots    map[string]json.RawMessage `json:"snapshots,omitempty"`
	// NextAction is the action chosen for the next iteration based on observing this step.
	NextAction *Action `json:"next_action,omitempty"`
	// Logs are the platform log lines collected for this step, Exceptions the
	// uncaught errors read at verification time: the error surface behind
	// state.logs and state.exceptions, which the default properties read and
	// an offline oracle has no other source for.
	Logs       []LogEntry  `json:"logs,omitempty"`
	Exceptions []Exception `json:"exceptions,omitempty"`
	// Navigations are the document-replacing navigations seen since the
	// previous step: the app reloaded, submitted a form, or changed route.
	// Each one restarts the app's own runtime, so without them a reload and a
	// generator repeating itself read the same way in a trace.
	Navigations      []Navigation               `json:"navigations,omitempty"`
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
	// ActionSkipped names why NextAction was chosen but never dispatched, so a
	// count of executed actions cannot be inflated by steps whose action the
	// runner threw away. Empty when the action ran (or when none was chosen).
	ActionSkipped string `json:"action_skipped,omitempty"`
	// ObservationError names why this step's device read produced no tree,
	// empty when a tree was read. A screen with no elements on it is a tree,
	// so without this a step that observed nothing at all is indistinguishable
	// from a step that observed an app showing nothing.
	ObservationError string `json:"observation_error,omitempty"`
	// SkippedVerification is set true exactly when the verifier was skipped
	// for this step, so downstream tooling can tell a deliberately-skipped
	// step from one that was verified and came back clean.
	SkippedVerification bool `json:"skipped_verification,omitempty"`
	// PreconditionFailure names a precondition of the run that was not met, so
	// a step that never had the app under test in front of it cannot be counted
	// as one that explored the app. Index 0 carries the startup gate's verdict:
	// a trace holding that record and nothing else is a run that never started,
	// which is a different thing from a run that explored and found nothing.
	PreconditionFailure string `json:"precondition_failure,omitempty"`
	// Witnesses records the violation witness for each property that newly
	// violated at this step: the cause and the extractor values at onset.
	Witnesses map[string]Witness `json:"witnesses,omitempty"`
}

// Navigation is one document-replacing navigation the run observed.
type Navigation struct {
	URL        string `json:"url"`
	UnixMillis int64  `json:"unix_millis,omitempty"`
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

// LogEntry mirrors one platform log line the runner collected for this step,
// in the shape state.logs exposes to a spec.
type LogEntry struct {
	UnixMillis int64  `json:"unix_millis,omitempty"`
	Level      string `json:"level,omitempty"`
	Tag        string `json:"tag,omitempty"`
	Message    string `json:"message,omitempty"`
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
	// LabelSource records how candidates were named to the picker. It is written
	// for a seeded run too, even though that picker selects by index and never
	// reads a label: it is the cell the run was assigned to, and the pair of
	// seeded runs across the two label modes is the manipulation check that says
	// how much of any difference is just application nondeterminism.
	LabelSource string `json:"label_source,omitempty"`
	// MaxSteps and DurationMillis are the budget the run was given, which has
	// to be identical across arms for a comparison to mean anything.
	MaxSteps       int   `json:"max_steps,omitempty"`
	DurationMillis int64 `json:"duration_millis,omitempty"`
	// Host is the machine that produced the run. Campaigns are split across
	// several hosts, so a per-host effect has to be detectable rather than
	// invisible.
	Host string `json:"host,omitempty"`
	// Device is the target the run drove, from --device. One host drives
	// several emulators at different API levels, so without it a trace on its
	// own cannot say what produced it and a per-device split can only be
	// recovered by joining against the campaign manifest.
	Device string `json:"device,omitempty"`
}

type Writer struct {
	directory string
	mutex     sync.Mutex
	file      io.WriteCloser
	encoder   *json.Encoder
	// llmCallFile is opened on the first WriteLLMCall, so a run whose picker
	// never called a model leaves no llm-calls.jsonl behind at all.
	llmCallFile    io.WriteCloser
	llmCallEncoder *json.Encoder
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

// WriteStep stamps the format version itself so no caller can write a step
// that cannot be told apart from one written before the format changed.
func (w *Writer) WriteStep(step Step) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.file == nil {
		return fmt.Errorf("trace: writer is closed")
	}
	step.TraceVersion = TraceVersion
	return w.encoder.Encode(step)
}

// WriteScreenshot is lock-free: each call writes a distinct, uniquely-named
// file via os.WriteFile and touches no field of Writer, so concurrent calls
// never contend.
func (w *Writer) WriteScreenshot(stepIndex int, png []byte) error {
	return w.writePNG(screenshotName(stepIndex), png)
}

func screenshotName(stepIndex int) string {
	return fmt.Sprintf("step-%05d.png", stepIndex)
}

// ScreenshotReference is the run-relative path WriteScreenshot puts a step's
// screenshot at. Records that describe an image point at it instead of copying
// the bytes.
func ScreenshotReference(stepIndex int) string {
	return screenshotDirectory + "/" + screenshotName(stepIndex)
}

const screenshotDirectory = "screenshots"

func (w *Writer) writePNG(name string, png []byte) error {
	if len(png) == 0 {
		return nil
	}
	directory := filepath.Join(w.directory, screenshotDirectory)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("mkdir screenshots: %w", err)
	}
	return os.WriteFile(filepath.Join(directory, name), png, 0o644)
}

func (w *Writer) Close() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	var err error
	if w.llmCallFile != nil {
		err = w.llmCallFile.Close()
		w.llmCallFile = nil
		w.llmCallEncoder = nil
	}
	if w.file == nil {
		return err
	}
	if closeErr := w.file.Close(); closeErr != nil {
		err = closeErr
	}
	w.file = nil
	return err
}
