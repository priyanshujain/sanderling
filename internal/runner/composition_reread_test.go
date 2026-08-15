package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/driver"
	mockdriver "github.com/priyanshujain/sanderling/internal/driver/mock"
)

// homeWithRows is one settled route whose list holds rows. A row arriving
// between two reads is what a Compose lazy list mounting over several frames
// looks like from the runner's side.
func homeWithRows(rows int) string {
	var children strings.Builder
	for row := range rows {
		fmt.Fprintf(&children,
			`,{"attributes":{"resource-id":"TxnRow%d","class":"android.view.View"},"children":[]}`, row)
	}
	return fmt.Sprintf(
		`{"attributes":{"resource-id":"HomeScreen","class":"android.view.View"},"children":[
		  {"attributes":{"resource-id":"TxnList","class":"android.view.View"},"children":[]}%s
		]}`, children.String())
}

// composesLateDriver answers the paired Snapshot with the frame the step
// records and the hierarchy read that follows with a tree that has grown a row,
// for the first composingReads reads of the run. After that both reads describe
// the same screen.
type composesLateDriver struct {
	*mockdriver.Driver
	composingReads int64
	reads          atomic.Int64
}

func (d *composesLateDriver) Snapshot(context.Context) (string, driver.Image, error) {
	return homeWithRows(1), driver.Image{PNG: []byte("png"), Width: 1, Height: 1}, nil
}

func (d *composesLateDriver) Hierarchy(context.Context) (string, error) {
	if d.reads.Add(1) <= d.composingReads {
		return homeWithRows(2), nil
	}
	return homeWithRows(1), nil
}

// A route can settle before its content composes, so a tree read the moment the
// route arrives can describe a screen that is still filling in. Verifying that
// step compares a half-composed frame against a settled one and convicts an app
// that did nothing wrong. Two reads a read apart see it happening, and the step
// they disagree on is one the verifier must never be handed.
//
// The always-false property is the witness: it fires on the first step the
// verifier evaluates, so the step index of its violation says exactly which
// step reached the verifier.
func TestRunner_AStepWhoseTreeChangedBetweenReadsIsNotVerified(t *testing.T) {
	run := func(t *testing.T, composingReads int64) (Summary, string) {
		t.Helper()
		state := newHarnessWithSpec(t, violationSpec)
		device := &composesLateDriver{Driver: state.mock, composingReads: composingReads}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		summary, err := Run(ctx, Options{
			Duration:    time.Hour,
			IdleTimeout: 20 * time.Millisecond,
			MaxSteps:    3,
			Driver:      device,
			Verifier:    state.verifier,
			TraceWriter: state.writer,
		})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if summary.Steps != 3 {
			t.Fatalf("steps = %d, want 3", summary.Steps)
		}
		return summary, state.writer.Directory()
	}

	t.Run("the step it changed on is skipped, the next one is judged", func(t *testing.T) {
		summary, directory := run(t, 1)
		if len(summary.Violations) != 1 {
			t.Fatalf("violations = %v, want exactly one", summary.Violations)
		}
		violation := summary.Violations[0]
		if violation.Properties[0] != "balanceNonNegative" {
			t.Fatalf("violated %v, want balanceNonNegative", violation.Properties)
		}
		if violation.StepIndex != 2 {
			t.Errorf("the property first judged step %d, want 2; the verifier was handed "+
				"a screen that grew a row while the runner was reading it",
				violation.StepIndex)
		}
		if summary.SkippedVerification != 1 {
			t.Errorf("the run reports %d step(s) judged by nothing, want 1",
				summary.SkippedVerification)
		}
		// Skipped is not lost: the step is still recorded, screenshot and all,
		// so the run can be replayed over the frame nothing judged.
		steps := traceSteps(t, directory)
		if len(steps) != 3 {
			t.Fatalf("trace holds %d step(s), want 3", len(steps))
		}
		if len(steps[0].Violations) != 0 {
			t.Errorf("step 1 recorded violations %v; it was never verified", steps[0].Violations)
		}
		screenshot := filepath.Join(directory, "screenshots", "step-00001.png")
		if _, err := os.Stat(screenshot); err != nil {
			t.Errorf("expected the skipped step's screenshot at %s: %v", screenshot, err)
		}
	})

	// The control. Two reads that agree must verify as they always did,
	// otherwise the case above is just a runner that verifies nothing.
	t.Run("two reads that agree verify the step", func(t *testing.T) {
		summary, _ := run(t, 0)
		if len(summary.Violations) != 1 {
			t.Fatalf("violations = %v, want exactly one", summary.Violations)
		}
		if got := summary.Violations[0].StepIndex; got != 1 {
			t.Errorf("the property first judged step %d, want 1; a settled screen must be "+
				"verified on the step it was read", got)
		}
	})
}

type traceLine struct {
	Step       int      `json:"step"`
	Violations []string `json:"violations"`
}

func traceSteps(t *testing.T, directory string) []traceLine {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(directory, "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var steps []traceLine
	for _, raw := range bytes.Split(bytes.TrimSpace(body), []byte("\n")) {
		var line traceLine
		if err := json.Unmarshal(raw, &line); err != nil {
			t.Fatalf("decode trace line: %v", err)
		}
		steps = append(steps, line)
	}
	return steps
}
