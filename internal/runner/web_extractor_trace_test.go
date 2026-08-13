package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
