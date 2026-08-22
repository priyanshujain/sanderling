package runner

import (
	"context"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/driver"
	mockdriver "github.com/priyanshujain/sanderling/internal/driver/mock"
	"github.com/priyanshujain/sanderling/internal/trace"
)

// throwingDriver reports one captured uncaught error, the way the chrome
// driver reports the page's buffer.
type throwingDriver struct {
	*mockdriver.Driver
}

func (d *throwingDriver) Exceptions(
	context.Context,
) ([]driver.Exception, error) {
	return []driver.Exception{{
		Class:      "TypeError",
		Message:    "cannot read balance of null",
		StackTrace: "at render (app.js:12)",
		UnixMillis: 1700000000000,
	}}, nil
}

// TestRunner_LogsAndExceptionsLandInTheTrace covers the error surface an
// offline oracle has no other source for: the default properties read
// state.logs and state.exceptions, and neither used to survive the step.
func TestRunner_LogsAndExceptionsLandInTheTrace(t *testing.T) {
	state := newHarness(t)
	state.mock.LogEntries = []driver.LogEntry{{
		UnixMillis: 1700000000123,
		Level:      "E",
		Tag:        "AndroidRuntime",
		Message:    "FATAL EXCEPTION: main",
	}}

	state.run(t, Options{
		Duration: 100 * time.Millisecond,
		MaxSteps: 1,
		Driver:   &throwingDriver{Driver: state.mock},
	})

	stored := readTraceLines(t, state.writer.Directory())[0]
	if stored.TraceVersion != trace.TraceVersion {
		t.Errorf(
			"trace_version = %d, want %d; an old trace could not be told apart",
			stored.TraceVersion,
			trace.TraceVersion,
		)
	}
	want := trace.LogEntry{
		UnixMillis: 1700000000123,
		Level:      "E",
		Tag:        "AndroidRuntime",
		Message:    "FATAL EXCEPTION: main",
	}
	if len(stored.Logs) != 1 || stored.Logs[0] != want {
		t.Errorf("logs on disk = %+v, want [%+v]", stored.Logs, want)
	}
	if len(stored.Exceptions) != 1 ||
		stored.Exceptions[0].Class != "TypeError" ||
		stored.Exceptions[0].Message != "cannot read balance of null" ||
		stored.Exceptions[0].StackTrace != "at render (app.js:12)" {
		t.Errorf("exceptions on disk = %+v", stored.Exceptions)
	}
}
