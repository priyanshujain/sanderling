package runner

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mockdriver "github.com/priyanshujain/sanderling/internal/driver/mock"
)

// On web the spec's extractors run in the page, so state.lastAction has to be
// installed there by the runner. It used to be hardcoded null in
// pkg/spec/src/web-runtime.ts, which made every property gated on the last
// action (folio's submitMovesBalanceByTypedAmount, for one) vacuously true on
// web: no failure, no warning, just a green run that proved nothing.

const lastActionSpec = `
import { actions } from "@sanderling/spec";
globalThis.actions = actions(() => []);
globalThis.properties = {};
`

// tappingWebDriver is a web target whose V8 picker always taps one named
// control, so the runner has a real applied action to report on the next step.
type tappingWebDriver struct {
	*mockdriver.Driver
	installed []string
}

func (d *tappingWebDriver) InstallBundle(context.Context, []byte) error { return nil }

func (d *tappingWebDriver) EvaluateExtractors(context.Context) (map[int]json.RawMessage, error) {
	return nil, nil
}

func (d *tappingWebDriver) NextActionFromV8(context.Context) (json.RawMessage, error) {
	return json.RawMessage(`{"kind":"Tap","x":12,"y":34,"selector":"id:TxnSubmit"}`), nil
}

func (d *tappingWebDriver) SetLastAction(_ context.Context, encoded json.RawMessage) error {
	d.installed = append(d.installed, string(encoded))
	return nil
}

func TestRunner_WebInstallsLastActionInThePage(t *testing.T) {
	state := newHarnessWithSpec(t, lastActionSpec)
	web := &tappingWebDriver{Driver: state.mock}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Run(ctx, Options{
		Duration:    time.Hour,
		IdleTimeout: 20 * time.Millisecond,
		MaxSteps:    3,
		Driver:      web,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(web.installed) < 2 {
		t.Fatalf("the page was handed lastAction %d time(s); the web path never installed it",
			len(web.installed))
	}
	// Step 1 has no previous action, exactly as the goja host reports it.
	if web.installed[0] != "null" {
		t.Errorf("step 1 installed %s, want null", web.installed[0])
	}
	// Every later step carries what the runner actually applied. The shape is
	// the goja host's (internal/verifier/marshal.go lastActionFields), pinned
	// against it by TestLastAction_WebJSONMatchesTheGojaObject.
	const want = `{"kind":"Tap","on":"id:TxnSubmit"}`
	if web.installed[1] != want {
		t.Errorf("step 2 installed %s, want %s", web.installed[1], want)
	}
}
