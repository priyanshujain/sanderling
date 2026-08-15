package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mockdriver "github.com/priyanshujain/sanderling/internal/driver/mock"
	"github.com/priyanshujain/sanderling/internal/trace"
)

// engineDisagreementSpec makes the two runtimes disagree on purpose: the
// extractor body returns "goja" when it runs in-process, and the web driver
// below reports "v8" for the same extractor. The property is true of one value
// and false of the other, so the verdict names the engine the verdict used.
const engineDisagreementSpec = `
import { actions, always, extract } from "@sanderling/spec";
const engine = extract("engine", () => "goja");
globalThis.properties = {
  ranInGoja: always(() => engine.current === "goja"),
};
globalThis.actions = actions(() => []);
`

// webMockDriver presents the mock device driver as a web target so the runner
// takes the V8 path, where extractor values come from the page rather than
// from goja.
type webMockDriver struct {
	*mockdriver.Driver
	overrides map[int]json.RawMessage
}

func (d *webMockDriver) InstallBundle(context.Context, []byte) error { return nil }

func (d *webMockDriver) EvaluateExtractors(context.Context) (map[int]json.RawMessage, error) {
	return d.overrides, nil
}

func (d *webMockDriver) NextActionFromV8(context.Context) (json.RawMessage, error) {
	return nil, nil
}

func (d *webMockDriver) SetLastAction(context.Context, json.RawMessage) error { return nil }

// TestRunner_TraceRecordsTheValueTheVerdictUsed fails if the trace and the
// verdict disagree about an extractor. A witness is only an explanation of a
// violation if it holds the state the violated property was evaluated against.
func TestRunner_TraceRecordsTheValueTheVerdictUsed(t *testing.T) {
	state := newHarnessWithSpec(t, engineDisagreementSpec)
	const pageValue = `"v8"`
	web := &webMockDriver{
		Driver:    state.mock,
		overrides: map[int]json.RawMessage{0: json.RawMessage(pageValue)},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    100 * time.Millisecond,
		IdleTimeout: 20 * time.Millisecond,
		Driver:      web,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !containsProperty(summary.Violations, "ranInGoja") {
		t.Fatalf("ranInGoja did not violate, so the page value never reached the verdict: %v",
			summary.Violations)
	}

	file, err := os.Open(filepath.Join(state.writer.Directory(), "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	type traceLine struct {
		Step             int                              `json:"step"`
		ExtractorChanges map[string]trace.ExtractorChange `json:"extractor_changes"`
		Witnesses        map[string]trace.Witness         `json:"witnesses"`
	}
	changes, witnesses := 0, 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var line traceLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("trace line decode: %v", err)
		}
		if change, ok := line.ExtractorChanges["engine"]; ok {
			changes++
			if got := string(change.Curr); got != pageValue {
				t.Errorf("step %d: trace records engine=%s, verdict used %s",
					line.Step, got, pageValue)
			}
		}
		for name, witness := range line.Witnesses {
			witnesses++
			if got := string(witness.Extractors["engine"]); got != pageValue {
				t.Errorf("step %d: %s witness records engine=%s, verdict used %s",
					line.Step, name, got, pageValue)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	if changes == 0 {
		t.Error("no extractor change reached the trace; nothing was compared")
	}
	if witnesses == 0 {
		t.Error("no witness reached the trace; nothing was compared")
	}
}

// splitTableSpec registers two extractors whose goja bodies both answer "goja".
// The page below reports only the first, so index 1 keeps goja's dump-derived
// reading while index 0 holds the page's.
const splitTableSpec = `
import { actions, extract } from "@sanderling/spec";
extract("first", () => "goja");
extract("second", () => "goja");
globalThis.properties = {};
globalThis.actions = actions(() => []);
`

// TestRunner_PartialExtractorTableIsFatal pins the failure the runner used to
// let through. JSON.stringify drops an undefined-valued key, so a page whose
// extractors are mostly undefined off their own screen reported a table with
// holes in it, and the run completed with half the extractors reading from V8
// and half from goja. A delta property spanning that split convicts an app that
// did nothing wrong, which is worse than a crash: it is a green report of a bug
// that is not there, or a red one for a bug nobody can reproduce.
func TestRunner_PartialExtractorTableIsFatal(t *testing.T) {
	state := newHarnessWithSpec(t, splitTableSpec)
	web := &webMockDriver{
		Driver:    state.mock,
		overrides: map[int]json.RawMessage{0: json.RawMessage(`"v8"`)},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Run(ctx, Options{
		Duration:    time.Hour,
		IdleTimeout: 20 * time.Millisecond,
		MaxSteps:    2,
		Driver:      web,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err == nil {
		t.Fatal("the run completed on a page that reported 1 of 2 extractors; " +
			"the second extractor silently kept goja's value")
	}
	const want = "the page reported values for 1 of the spec's 2 extractors"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("Run failed with %q, want it to name the split: %q", err, want)
	}
}
