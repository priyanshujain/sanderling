package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/driver"
	mockdriver "github.com/priyanshujain/sanderling/internal/driver/mock"
	"github.com/priyanshujain/sanderling/internal/trace"
)

// carrierSpec registers one extractor whose value the page supplies. It stands
// in for every spec whose getters carry state across steps (folio's last-seen
// Home total, its submit counters): what matters is that ASKING the page for
// the value is what advances it.
const carrierSpec = `
import { actions, extract } from "@sanderling/spec";
const carrier = extract("carrier", () => 0);
globalThis.properties = {};
globalThis.actions = actions(() => []);
`

// carrierWebDriver is a web target that alternates between a cross-fading
// hierarchy (which the runner discards as transitional) and a settled one, and
// whose page-side extractor advances a counter on every evaluation - exactly
// what a spec-authored carrier does in V8.
type carrierWebDriver struct {
	*mockdriver.Driver
	transitional bool
	snapshots    int
	reads        int
}

func (d *carrierWebDriver) Snapshot(ctx context.Context) (string, driver.Image, error) {
	_, image, err := d.Driver.Snapshot(ctx)
	d.snapshots++
	if !d.transitional {
		return `{"attributes":{"resource-id":"HomeScreen"},"children":[]}`, image, err
	}
	// A genuine cross-fade: two live routes, and a tree that keeps changing
	// between retries so the runner spends its whole retry budget on it.
	return fmt.Sprintf(`{"attributes":{"resource-id":"root"},"children":[
	  {"attributes":{"resource-id":"HomeScreen","text":"frame-%d"},"children":[]},
	  {"attributes":{"resource-id":"LedgerScreen"},"children":[]}
	]}`, d.snapshots), image, err
}

func (d *carrierWebDriver) InstallBundle(context.Context, []byte) error { return nil }

func (d *carrierWebDriver) EvaluateExtractors(context.Context) (map[int]json.RawMessage, error) {
	d.reads++
	return map[int]json.RawMessage{0: json.RawMessage(strconv.Itoa(d.reads))}, nil
}

// NextActionFromV8 runs once per step, after the hierarchy fetch, so flipping
// here makes every other step a cross-fade.
func (d *carrierWebDriver) NextActionFromV8(context.Context) (json.RawMessage, error) {
	d.transitional = !d.transitional
	return json.RawMessage(`{"kind":"Tap","x":5,"y":5}`), nil
}

func (d *carrierWebDriver) SetLastAction(context.Context, json.RawMessage) error { return nil }

// TestRunner_TransitionalStepNeverAdvancesThePageCarrier pins the ordering the
// web path depends on. The page-side extractors must run only on steps the
// verifier accepts: their getters advance spec state every time they evaluate,
// so evaluating them on a step whose values are then discarded leaves the page
// one window ahead of the verifier. The next accepted pair then brackets two
// committed transactions while having counted one submit, and the property
// convicts an app that did nothing wrong.
func TestRunner_TransitionalStepNeverAdvancesThePageCarrier(t *testing.T) {
	state := newHarnessWithSpec(t, carrierSpec)
	web := &carrierWebDriver{Driver: state.mock}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    30 * time.Second,
		IdleTimeout: 20 * time.Millisecond,
		MaxSteps:    5,
		Driver:      web,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Steps != 5 {
		t.Fatalf("steps = %d, want 5", summary.Steps)
	}

	type traceLine struct {
		Step             int                              `json:"step"`
		Transitional     bool                             `json:"transitional"`
		ExtractorChanges map[string]trace.ExtractorChange `json:"extractor_changes"`
	}
	body, err := os.ReadFile(filepath.Join(state.writer.Directory(), "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	verified, transitional := 0, 0
	previous := 0
	for _, raw := range bytes.Split(bytes.TrimSpace(body), []byte("\n")) {
		var line traceLine
		if err := json.Unmarshal(raw, &line); err != nil {
			t.Fatalf("decode trace line: %v", err)
		}
		if line.Transitional {
			transitional++
			continue
		}
		verified++
		change, ok := line.ExtractorChanges["carrier"]
		if !ok {
			t.Fatalf("step %d: no carrier value reached the verifier", line.Step)
		}
		current, convErr := strconv.Atoi(string(change.Curr))
		if convErr != nil {
			t.Fatalf("step %d: carrier value %s: %v", line.Step, change.Curr, convErr)
		}
		if current != previous+1 {
			t.Errorf("step %d: carrier went %d -> %d; the page advanced it on a "+
				"step the verifier discarded, so the verifier's window is wider "+
				"than the one the spec counted actions over",
				line.Step, previous, current)
		}
		previous = current
	}
	if verified == 0 || transitional == 0 {
		t.Fatalf("need both kinds of step to prove anything: %d verified, %d transitional",
			verified, transitional)
	}
	if web.reads != verified {
		t.Errorf("the page evaluated its extractors %d time(s) across %d verified step(s); "+
			"every evaluation the verifier does not use still advances spec state",
			web.reads, verified)
	}
}

// installFailsWebDriver is a web target whose page cannot take the runner's
// lastAction: an older published @sanderling/spec runtime, a bundle that never
// installed, a tab that navigated away from it.
type installFailsWebDriver struct {
	*mockdriver.Driver
}

func (d *installFailsWebDriver) InstallBundle(context.Context, []byte) error { return nil }

func (d *installFailsWebDriver) EvaluateExtractors(context.Context) (map[int]json.RawMessage, error) {
	return map[int]json.RawMessage{0: json.RawMessage(`1`)}, nil
}

func (d *installFailsWebDriver) NextActionFromV8(context.Context) (json.RawMessage, error) {
	return json.RawMessage(`{"kind":"Tap","x":5,"y":5}`), nil
}

func (d *installFailsWebDriver) SetLastAction(context.Context, json.RawMessage) error {
	return errors.New("__sanderlingSetLastAction__ is not a function")
}

// TestRunner_LastActionInstallFailureFailsTheRun covers the other half of the
// same trust boundary. A run that cannot install lastAction in the page cannot
// apply the page's extractor values either, so the step keeps goja's
// dump-derived readings while the step before it holds the page's, and a delta
// property compares two producers and fires. Downgraded to a warning that is a
// green run reporting a violation nobody can reproduce.
func TestRunner_LastActionInstallFailureFailsTheRun(t *testing.T) {
	state := newHarnessWithSpec(t, carrierSpec)
	web := &installFailsWebDriver{Driver: state.mock}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := Run(ctx, Options{
		Duration:    2 * time.Second,
		IdleTimeout: 20 * time.Millisecond,
		MaxSteps:    3,
		Driver:      web,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err == nil {
		t.Fatal("Run succeeded with a page that cannot take lastAction; the run " +
			"reported green while its extractor values came from two engines")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("install last action")) {
		t.Errorf("Run error = %v, want it to name the failed lastAction install", err)
	}
}
