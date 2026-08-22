package runner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
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
	webDriverBase
	overrides map[int]json.RawMessage
}

func (d *webMockDriver) EvaluateExtractors(context.Context) (map[int]json.RawMessage, error) {
	return d.overrides, nil
}

// TestRunner_TraceRecordsTheValueTheVerdictUsed fails if the trace and the
// verdict disagree about an extractor. A witness is only an explanation of a
// violation if it holds the state the violated property was evaluated against.
func TestRunner_TraceRecordsTheValueTheVerdictUsed(t *testing.T) {
	state := newHarnessWithSpec(t, engineDisagreementSpec)
	const pageValue = `"v8"`
	web := &webMockDriver{
		webDriverBase: webDriverBase{Driver: state.mock},
		overrides:     map[int]json.RawMessage{0: json.RawMessage(pageValue)},
	}

	summary := state.run(t, Options{Duration: 100 * time.Millisecond, Driver: web})
	if !containsProperty(summary.Violations, "ranInGoja") {
		t.Fatalf("ranInGoja did not violate, so the page value never reached the verdict: %v",
			summary.Violations)
	}

	changes, witnesses := 0, 0
	for _, line := range readTraceLines(t, state.writer.Directory()) {
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
		webDriverBase: webDriverBase{Driver: state.mock},
		overrides:     map[int]json.RawMessage{0: json.RawMessage(`"v8"`)},
	}

	_, err := state.tryRun(t, Options{MaxSteps: 2, Driver: web})
	if err == nil {
		t.Fatal("the run completed on a page that reported 1 of 2 extractors; " +
			"the second extractor silently kept goja's value")
	}
	const want = "the page reported values for 1 of the spec's 2 extractors"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("Run failed with %q, want it to name the split: %q", err, want)
	}
}
