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

// submitsOnTapDriver commits commitsPerTap transactions on every tap, shows the
// running total in the tree, and grows a row under the hierarchy read that
// follows the paired Snapshot: on one chosen step, on the run of steps from
// composingRead through composingThrough, or on every one of them.
type submitsOnTapDriver struct {
	*mockdriver.Driver
	commitsPerTap    int64
	composingRead    int64
	composingThrough int64
	everyRead        bool
	reads            atomic.Int64
	committed        atomic.Int64
}

func (d *submitsOnTapDriver) Tap(context.Context, int, int) error       { return d.commit() }
func (d *submitsOnTapDriver) TapSelector(context.Context, string) error { return d.commit() }

func (d *submitsOnTapDriver) commit() error {
	d.committed.Add(d.commitsPerTap)
	return nil
}

func (d *submitsOnTapDriver) Snapshot(context.Context) (string, driver.Image, error) {
	return fmt.Sprintf(homeWithTxnCount, d.committed.Load()), driver.Image{}, nil
}

func (d *submitsOnTapDriver) Hierarchy(context.Context) (string, error) {
	read := d.reads.Add(1)
	composing := d.everyRead || read == d.composingRead ||
		(read >= d.composingRead && read <= d.composingThrough)
	if composing {
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
		device := &submitsOnTapDriver{
			Driver:        state.mock,
			commitsPerTap: 1,
			composingRead: composingRead,
		}

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

// One skipped step is held; a run of them has to be held too. A bound that lets
// the runner act again while the verifier is still being skipped puts back the
// exact swallow the hold exists to prevent, only later: the action drawn on the
// step past the bound overwrites the one the hold was carrying, and the carried
// action is never reported to any spec.
//
// The screen composes on steps 3 through 5 of 6 and the device commits one
// transaction per tap throughout, so the property has a clean pair to judge
// (step 2 to step 6) and nothing in between it can be told about except the
// action step 2 applied.
func TestRunner_ARunOfSkippedStepsReportsEveryActionItApplied(t *testing.T) {
	spec := specWithFolioPredicates(t)

	run := func(t *testing.T, commitsPerTap int64) (Summary, int64) {
		t.Helper()
		state := newHarnessWithSpec(t, spec)
		device := &submitsOnTapDriver{
			Driver:           state.mock,
			commitsPerTap:    commitsPerTap,
			composingRead:    3,
			composingThrough: 5,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		summary, err := Run(ctx, Options{
			Duration:    time.Hour,
			IdleTimeout: 20 * time.Millisecond,
			MaxSteps:    6,
			Driver:      device,
			Verifier:    state.verifier,
			TraceWriter: state.writer,
		})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if summary.Steps != 6 {
			t.Fatalf("steps = %d, want 6", summary.Steps)
		}
		if summary.SkippedVerification != 3 {
			t.Fatalf("the run skipped %d step(s), want 3; the reread never fired across "+
				"the run this test is about", summary.SkippedVerification)
		}
		return summary, device.committed.Load()
	}

	t.Run("no submit is lost to the run of skipped steps", func(t *testing.T) {
		summary, committed := run(t, 1)
		if committed == 0 {
			t.Fatal("the device committed nothing; a runner that never acts passes this " +
				"test without meaning anything")
		}
		if len(summary.Violations) != 0 {
			t.Errorf("the counting property convicted a healthy app: %v\n"+
				"one transaction per submit rose, and a submit went unreported because "+
				"the runner acted on a step the verifier skipped", summary.Violations)
		}
		if committed != 3 {
			t.Errorf("the device committed %d transaction(s), want 3: one per verified "+
				"step (1, 2 and 6) and none from a step nothing would judge", committed)
		}
	})

	// The control. Without it a green above proves nothing: a property handed
	// no comparable pair is silently vacuous and reports the same empty list.
	t.Run("two transactions per tap still convicts across the same run", func(t *testing.T) {
		summary, _ := run(t, 2)
		if len(summary.Violations) == 0 {
			t.Fatal("the counting property missed a double submit; the skipped steps left " +
				"it with nothing to judge, so the case above proves nothing")
		}
		if got := summary.Violations[0].Properties[0]; got != "submitCommitsOneTransactionPerAction" {
			t.Errorf("violated %v, want submitCommitsOneTransactionPerAction",
				summary.Violations[0].Properties)
		}
	})
}

// A screen that changes shape under every pair of reads (a live list, a spinner
// mounting and unmounting) costs the run its actions: an action applied onto it
// would be the one the next verified step never hears about, and there is no
// next verified step. What the run must not do is come back green off that,
// which is what the "judged by nothing" count and the run's outcome are for.
func TestRunner_AScreenThatNeverSettlesActsOnNothingAndSaysSo(t *testing.T) {
	state := newHarnessWithSpec(t, specWithFolioPredicates(t))
	device := &submitsOnTapDriver{Driver: state.mock, commitsPerTap: 1, everyRead: true}

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
	if summary.Steps != 5 {
		t.Fatalf("steps = %d, want 5; the run stalled instead of finishing its budget",
			summary.Steps)
	}
	if summary.SkippedVerification != 5 {
		t.Fatalf("the run verified some step of a screen that never settled: skipped %d of 5",
			summary.SkippedVerification)
	}
	if got := device.committed.Load(); got != 0 {
		t.Errorf("the fuzzer applied %d action(s) onto a screen no property would judge; "+
			"each one is an action no spec will ever be told about", got)
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
