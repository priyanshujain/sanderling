package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/priyanshujain/sanderling/internal/driver"
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
	webDriverBase
	installed     []string
	installedLogs []string
}

func (d *tappingWebDriver) NextActionFromV8(context.Context) (json.RawMessage, error) {
	return json.RawMessage(`{"kind":"Tap","x":12,"y":34,"selector":"id:TxnSubmit"}`), nil
}

func (d *tappingWebDriver) SetLastAction(_ context.Context, encoded json.RawMessage) error {
	d.installed = append(d.installed, string(encoded))
	return nil
}

func (d *tappingWebDriver) SetLogs(_ context.Context, encoded json.RawMessage) error {
	d.installedLogs = append(d.installedLogs, string(encoded))
	return nil
}

func TestRunner_WebInstallsLastActionInThePage(t *testing.T) {
	state := newHarnessWithSpec(t, lastActionSpec)
	web := &tappingWebDriver{webDriverBase: webDriverBase{Driver: state.mock}}

	state.run(t, Options{MaxSteps: 3, Driver: web})

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
	const want = `{"kind":"Tap","applied":true,"relaunched":null,"on":"id:TxnSubmit"}`
	if web.installed[1] != want {
		t.Errorf("step 2 installed %s, want %s", web.installed[1], want)
	}
}

// The same hole on the other channel: state.logs was hardcoded [] in
// pkg/spec/src/web-runtime.ts, and because the page's reading of an extractor
// replaces the host's on web, the driver's error-level entries never reached a
// property. The default noLogcatErrors counted an empty array on every run.
func TestRunner_WebInstallsTheStepsLogsInThePage(t *testing.T) {
	state := newHarnessWithSpec(t, lastActionSpec)
	state.mock.LogEntries = []driver.LogEntry{
		{UnixMillis: 1700000000123, Level: "E", Tag: "console", Message: "boom from the page"},
	}
	web := &tappingWebDriver{webDriverBase: webDriverBase{Driver: state.mock}}

	state.run(t, Options{MaxSteps: 2, Driver: web})

	if len(web.installedLogs) == 0 {
		t.Fatal("the page was never handed the step's logs; every property reading " +
			"state.logs evaluated against the empty array the page starts with")
	}
	// The shape is the goja host's (internal/verifier/marshal.go logFields),
	// pinned against it by TestLogs_WebJSONMatchesTheGojaObject.
	const want = `[{"unixMillis":1700000000123,"level":"E","tag":"console","message":"boom from the page"}]`
	if web.installedLogs[0] != want {
		t.Errorf("step 1 installed %s, want %s", web.installedLogs[0], want)
	}
}

// A log fetch that fails decides the verdict of every log property: they all
// evaluate against an empty slice and hold. That is not a fact about the app,
// so the step it happened on has to be visible in the run's output. It used to
// be dropped in silence, under a comment claiming it was warned about.
func TestRunner_ReportsALogFetchItCouldNotMake(t *testing.T) {
	state := newHarnessWithSpec(t, lastActionSpec)
	state.mock.Failures[mockdriver.ActionRecentLogs] = mockdriver.FailurePlan{Err: errors.New("adb: device offline")}

	var buffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelWarn}))

	state.run(t, Options{MaxSteps: 2, Logger: logger})

	if !strings.Contains(buffer.String(), "adb: device offline") {
		t.Errorf("the run never reported the failed log fetch, so noLogcatErrors "+
			"held on evidence nobody collected; log was %q", buffer.String())
	}
}

// failingTapWebDriver dispatches the tap and then fails the call, the shape an
// RPC deadline takes: the page has the click, the runner has an error.
type failingTapWebDriver struct {
	*tappingWebDriver
}

func (d *failingTapWebDriver) Tap(context.Context, int, int) error {
	return errors.New("rpc error: code = DeadlineExceeded desc = context deadline exceeded")
}

// The web leg of the same three states the goja host reports. "applied":null is
// not "no action": a property gated on the last action still sees the tap and
// decides for itself, which it cannot do if the page is handed a bare null.
func TestRunner_WebInstallsAnUnconfirmedActionWithItsFateUnknown(t *testing.T) {
	state := newHarnessWithSpec(t, lastActionSpec)
	web := &failingTapWebDriver{
		tappingWebDriver: &tappingWebDriver{webDriverBase: webDriverBase{Driver: state.mock}},
	}

	state.run(t, Options{MaxSteps: 2, Driver: web})

	if len(web.installed) < 2 {
		t.Fatalf("the page was handed lastAction %d time(s); the web path never installed it",
			len(web.installed))
	}
	const want = `{"kind":"Tap","applied":null,"relaunched":null,"on":"id:TxnSubmit"}`
	if web.installed[1] != want {
		t.Errorf("step 2 installed %s, want %s", web.installed[1], want)
	}
}
