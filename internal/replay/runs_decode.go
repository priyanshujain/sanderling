package replay

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/priyanshujain/sanderling/internal/trace"
)

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

// violationAttribution maps one recorded violation to the step the marker
// belongs on: the causing step its witness names, falling back to the step
// whose trace line carries it (the detection step) for witnesses written
// before the step field existed.
type violationAttribution struct {
	attributedStep int
	detectedStep   int
}

func decodeStepSummary(line []byte) (StepSummary, []violationAttribution, error) {
	var partial struct {
		Index      int       `json:"step"`
		Timestamp  time.Time `json:"timestamp"`
		Screen     string    `json:"screen,omitempty"`
		NextAction *struct {
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
		} `json:"next_action,omitempty"`
		Exceptions []json.RawMessage `json:"exceptions,omitempty"`
		Violations []string          `json:"violations,omitempty"`
		Witnesses  map[string]struct {
			Step int `json:"step"`
		} `json:"witnesses,omitempty"`
	}
	if err := json.Unmarshal(line, &partial); err != nil {
		return StepSummary{}, nil, fmt.Errorf("decode step: %w", err)
	}
	var attributions []violationAttribution
	for _, name := range partial.Violations {
		attribution := violationAttribution{
			attributedStep: partial.Index,
			detectedStep:   partial.Index,
		}
		if witness, ok := partial.Witnesses[name]; ok && witness.Step > 0 {
			attribution.attributedStep = witness.Step
		}
		attributions = append(attributions, attribution)
	}
	summary := StepSummary{
		Index:         partial.Index,
		Timestamp:     partial.Timestamp,
		Screen:        partial.Screen,
		HasExceptions: len(partial.Exceptions) > 0,
	}
	if partial.NextAction != nil {
		summary.ActionKind = partial.NextAction.Kind
		switch partial.NextAction.Kind {
		case "Tap", "DoubleTap":
			if partial.NextAction.Selector != "" {
				summary.ActionLabel = partial.NextAction.Selector
			} else if partial.NextAction.Text != "" {
				summary.ActionLabel = partial.NextAction.Text
			} else if partial.NextAction.X != 0 || partial.NextAction.Y != 0 {
				summary.ActionLabel = fmt.Sprintf("(%d,%d)", partial.NextAction.X, partial.NextAction.Y)
			}
		case "InputText":
			summary.ActionLabel = fmt.Sprintf("%q", partial.NextAction.Text)
		case "Swipe":
			summary.ActionLabel = swipeDirectionLabel(
				partial.NextAction.FromX, partial.NextAction.FromY,
				partial.NextAction.ToX, partial.NextAction.ToY,
			)
		case "PressKey":
			summary.ActionLabel = partial.NextAction.Key
		case "Wait":
			if partial.NextAction.DurationMillis > 0 {
				summary.ActionLabel = fmt.Sprintf("%dms", partial.NextAction.DurationMillis)
			}
		}
	}
	return summary, attributions, nil
}

// markViolations sets HasViolations on the step each attribution points at.
// The marker goes on the causing step; when that index is missing from the
// trace the detection step keeps it. Duplicate indices (a finalize line echoes
// the last step's index) resolve to the first occurrence, the real step.
func markViolations(steps []StepSummary, attributions []violationAttribution) {
	if len(attributions) == 0 {
		return
	}
	positionOf := make(map[int]int, len(steps))
	for position, step := range steps {
		if _, ok := positionOf[step.Index]; !ok {
			positionOf[step.Index] = position
		}
	}
	for _, attribution := range attributions {
		position, ok := positionOf[attribution.attributedStep]
		if !ok {
			position, ok = positionOf[attribution.detectedStep]
			if !ok {
				continue
			}
		}
		steps[position].HasViolations = true
	}
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
