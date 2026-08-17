package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	mockdriver "github.com/priyanshujain/sanderling/internal/driver/mock"
	"github.com/priyanshujain/sanderling/internal/trace"
)

// slowToDrawDriver is an app that is the resumed activity immediately and whose
// window takes drawsAfter focus polls to appear: the shape of a real cold start,
// where ResumedActivity flips before the first frame. WaitForIdle returns at
// once, as it does on a device whose UI thread is quiet between frames.
type slowToDrawDriver struct {
	*mockdriver.Driver
	drawsAfter int
	polls      int
}

func (d *slowToDrawDriver) ForegroundApp(context.Context) (string, error) {
	return guardedBundleID, nil
}

func (d *slowToDrawDriver) FocusedWindowApp(context.Context) (string, error) {
	d.polls++
	if d.polls > d.drawsAfter {
		return guardedBundleID, nil
	}
	return "", nil
}

func (d *slowToDrawDriver) WaitForIdle(context.Context, time.Duration) error { return nil }

// The gate's budget has to be a duration, not a count of polls. A count is not a
// budget: each poll costs whatever the driver's idle wait happens to take, so the
// same launch clears the gate on one device and exhausts it on another. That is
// what an 80-run campaign against one app measured: on API 34 the idle wait
// returned in ~100ms, eight polls gave up 1.2s in, and the app's window drew at
// ~1.9s; on API 36 the same eight polls spanned 3s and cleared the same launch.
// Same app, same harness, a verdict that came from the device.
func TestAwaitForeground_BudgetIsTimeNotPolls(t *testing.T) {
	fastForegroundGate(t)
	const drawsAfter = 40
	device := &slowToDrawDriver{Driver: mockdriver.New(), drawsAfter: drawsAfter}
	options := Options{
		BundleID:    guardedBundleID,
		Driver:      device,
		IdleTimeout: time.Millisecond,
	}

	ready := awaitForeground(context.Background(), options, discardLogger(), 0)

	if !ready {
		t.Fatalf("the gate gave up after %d poll(s) while the app was the resumed activity "+
			"and its window drew on poll %d; a budget counted in polls expires at a "+
			"different wall-clock time on every device", device.polls, drawsAfter+1)
	}
	if device.polls <= drawsAfter {
		t.Fatalf("the gate polled %d time(s), want more than %d: it has to keep looking "+
			"until its budget runs out, not stop at a fixed count", device.polls, drawsAfter)
	}
}

// neverDrawsDriver never brings the app forward: the app under test is not the
// foreground app and no relaunch changes that, which is what a genuinely unmet
// precondition looks like.
type neverDrawsDriver struct {
	*mockdriver.Driver
}

func (d *neverDrawsDriver) ForegroundApp(context.Context) (string, error) {
	return "com.android.launcher", nil
}

func (d *neverDrawsDriver) FocusedWindowApp(context.Context) (string, error) {
	return "com.android.launcher", nil
}

func (d *neverDrawsDriver) WaitForIdle(context.Context, time.Duration) error { return nil }

// A run whose app never came to the foreground explored nothing, and reporting it
// as a run that found no violations counts a harness failure as evidence about
// the app. It has to end the run and say so where a campaign can count it, not
// warn once and carry on into steps that observe some other app.
func TestRun_AppNeverReachesForegroundEndsTheRun(t *testing.T) {
	fastForegroundGate(t)
	state := newHarness(t)
	device := &neverDrawsDriver{Driver: state.mock}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    time.Hour,
		IdleTimeout: time.Millisecond,
		MaxSteps:    3,
		BundleID:    guardedBundleID,
		Driver:      device,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})

	var notReached ForegroundNotReachedError
	if !errors.As(err, &notReached) {
		t.Fatalf("Run returned %v, want a ForegroundNotReachedError: a run that never got "+
			"the app on screen is not a run that explored it", err)
	}
	if notReached.BundleID != guardedBundleID {
		t.Errorf("the error names %q, want %q", notReached.BundleID, guardedBundleID)
	}
	if summary.Steps != 0 {
		t.Errorf("the run took %d step(s) after its precondition failed, want 0", summary.Steps)
	}

	records := preconditionRecords(t, state.writer.Directory())
	if len(records) != 1 {
		t.Fatalf("the trace holds %d precondition record(s), want 1: a campaign has to be "+
			"able to count this without grepping logs", len(records))
	}
	if records[0].Index != 0 {
		t.Errorf("the record is on step %d, want 0 (no step ever ran)", records[0].Index)
	}
	if records[0].PreconditionFailure != preconditionAppNotForeground {
		t.Errorf("the trace records %q, want %q",
			records[0].PreconditionFailure, preconditionAppNotForeground)
	}
}

// leavesForegroundForeverDriver walks out of the app after the first step and
// never comes back, so the per-step scope guard exhausts its budget mid-run.
type leavesForegroundForeverDriver struct {
	*mockdriver.Driver
	checks int
}

func (d *leavesForegroundForeverDriver) ForegroundApp(context.Context) (string, error) {
	d.checks++
	if d.checks <= 2 {
		return guardedBundleID, nil
	}
	return "com.android.launcher", nil
}

func (d *leavesForegroundForeverDriver) FocusedWindowApp(ctx context.Context) (string, error) {
	return d.ForegroundApp(ctx)
}

func (d *leavesForegroundForeverDriver) WaitForIdle(context.Context, time.Duration) error {
	return nil
}

// The same fact mid-run: a step the guard could not return to the app observed
// something that is not the app under test, and until now nothing in the trace
// said so.
func TestRun_StepsOutsideTheAppAreRecordedInTheTrace(t *testing.T) {
	fastForegroundGate(t)
	state := newHarness(t)
	device := &leavesForegroundForeverDriver{Driver: state.mock}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := Run(ctx, Options{
		Duration:    time.Hour,
		IdleTimeout: time.Millisecond,
		MaxSteps:    2,
		BundleID:    guardedBundleID,
		Driver:      device,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	records := preconditionRecords(t, state.writer.Directory())
	if len(records) == 0 {
		t.Fatal("no step recorded that the guard never got the app back, so a run that " +
			"spent its steps outside the app reads exactly like one that explored it")
	}
	for _, record := range records {
		if record.Index == 0 {
			t.Error("a mid-run failure was recorded as the startup gate's verdict (step 0)")
		}
		if record.PreconditionFailure != preconditionAppNotForeground {
			t.Errorf("step %d records %q, want %q",
				record.Index, record.PreconditionFailure, preconditionAppNotForeground)
		}
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// preconditionRecords reads the trace back off disk and returns the steps naming
// an unmet precondition, decoded through trace.Step so the test reads the same
// field a campaign would.
func preconditionRecords(t *testing.T, directory string) []trace.Step {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(directory, "trace.jsonl"))
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	var records []trace.Step
	for _, raw := range bytes.Split(bytes.TrimSpace(body), []byte("\n")) {
		if len(raw) == 0 {
			continue
		}
		var step trace.Step
		if err := json.Unmarshal(raw, &step); err != nil {
			t.Fatalf("decode trace line: %v", err)
		}
		if step.PreconditionFailure != "" {
			records = append(records, step)
		}
	}
	return records
}
