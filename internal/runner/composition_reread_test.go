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

// submitsOnTapDriver commits a transaction on every tap, shows the running
// total in the tree, and grows a row under the hierarchy read that follows the
// paired Snapshot on one chosen step.
type submitsOnTapDriver struct {
	*mockdriver.Driver
	composingRead int64
	everyRead     bool
	reads         atomic.Int64
	committed     atomic.Int64
}

func (d *submitsOnTapDriver) Tap(context.Context, int, int) error       { return d.commit() }
func (d *submitsOnTapDriver) TapSelector(context.Context, string) error { return d.commit() }

func (d *submitsOnTapDriver) commit() error {
	d.committed.Add(1)
	return nil
}

func (d *submitsOnTapDriver) Snapshot(context.Context) (string, driver.Image, error) {
	return fmt.Sprintf(homeWithTxnCount, d.committed.Load()), driver.Image{}, nil
}

func (d *submitsOnTapDriver) Hierarchy(context.Context) (string, error) {
	if read := d.reads.Add(1); d.everyRead || read == d.composingRead {
		return fmt.Sprintf(homeWithTxnCountComposing, d.committed.Load()), nil
	}
	return fmt.Sprintf(homeWithTxnCount, d.committed.Load()), nil
}

// The same tree with one more row in it, which is what the reread sees while
// the screen is still filling in.
const homeWithTxnCountComposing = `{"attributes":{"resource-id":"HomeScreen"},"children":[
	{"attributes":{"resource-id":"TxnCount","text":"%d"},"children":[]},
	{"attributes":{"resource-id":"TxnSubmit","bounds":"[40,80,240,160]"},"children":[],"clickable":true,"enabled":true},
	{"attributes":{"resource-id":"TxnRowLate"},"children":[]}
]}`

// Skipping a step is only free if nothing the spec needs goes missing with it.
// The action a step applies is reported to the spec on the NEXT step the
// verifier accepts, so a skipped step in between swallows the action before it:
// the transaction it committed still turns up in the next reading, and
// submitCommitsOneTransactionPerAction sees a rise nothing in its window
// accounts for. That is the conviction #77 and #78 are about, arriving through
// the skip rather than through the runner's report.
//
// So a frame the verifier will not look at is not one to act on either, which
// is also what #75 asked for: the fuzzer must not tap into a screen that is
// still filling in.
func TestRunner_ASkippedStepDoesNotSwallowTheActionBeforeIt(t *testing.T) {
	spec := specWithFolioPredicates(t)

	run := func(t *testing.T, composingRead int64) (Summary, int64) {
		t.Helper()
		state := newHarnessWithSpec(t, spec)
		device := &submitsOnTapDriver{Driver: state.mock, composingRead: composingRead}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
		return summary, device.committed.Load()
	}

	t.Run("a submit is not lost to the step that follows it", func(t *testing.T) {
		summary, committed := run(t, 2)
		if summary.SkippedVerification != 1 {
			t.Fatalf("the run skipped %d step(s), want 1; the reread never fired, so this "+
				"proves nothing", summary.SkippedVerification)
		}
		if committed == 0 {
			t.Fatal("the device committed nothing; a runner that never acts passes this " +
				"test without meaning anything")
		}
		if len(summary.Violations) != 0 {
			t.Errorf("the counting property convicted a healthy app: %v\n"+
				"one transaction per submit rose, and a submit went unreported because "+
				"the step after it was skipped", summary.Violations)
		}
	})

	// The control: with nothing composing, every step is verified and the same
	// app is judged clean, so the case above is not just a runner that stopped
	// judging.
	t.Run("every step verified, same app, no violation", func(t *testing.T) {
		summary, committed := run(t, 0)
		if summary.SkippedVerification != 0 {
			t.Fatalf("the run skipped %d step(s), want 0", summary.SkippedVerification)
		}
		if committed != 3 {
			t.Fatalf("the device committed %d transaction(s), want 3", committed)
		}
		if len(summary.Violations) != 0 {
			t.Errorf("the counting property convicted a healthy app: %v", summary.Violations)
		}
	})
}

// Holding an action back is bounded. A screen that changes shape under every
// pair of reads (a live list, a spinner mounting and unmounting) would
// otherwise take the whole run: nothing verified, nothing tapped, and a green
// summary at the end of it.
func TestRunner_AScreenThatNeverSettlesDoesNotStallTheRun(t *testing.T) {
	state := newHarnessWithSpec(t, specWithFolioPredicates(t))
	device := &submitsOnTapDriver{Driver: state.mock, everyRead: true}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    time.Hour,
		IdleTimeout: 20 * time.Millisecond,
		MaxSteps:    5,
		Driver:      device,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.SkippedVerification != 5 {
		t.Fatalf("the run verified some step of a screen that never settled: skipped %d of 5",
			summary.SkippedVerification)
	}
	if device.committed.Load() == 0 {
		t.Error("the fuzzer never acted across 5 steps; a screen that keeps moving must " +
			"cost the run a step or two, not all of them")
	}
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
