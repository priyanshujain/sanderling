package runner

import (
	"context"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/driver"
	mockdriver "github.com/priyanshujain/sanderling/internal/driver/mock"
)

// navigatingDriver reports one navigation per step, the way a page that submits
// a form or reloads does.
type navigatingDriver struct {
	*mockdriver.Driver
	url string
}

func (d *navigatingDriver) Navigations(context.Context) ([]driver.Navigation, error) {
	return []driver.Navigation{{URL: d.url, UnixMillis: 1700000000000}}, nil
}

// A run whose app replaced its own document has to say so on the step it
// happened. Without it the trace shows a generator repeating one action and no
// reason for it, and an analysis cannot tell that from a seed that chose badly.
func TestRunner_TheTraceRecordsThatThePageNavigated(t *testing.T) {
	state := newHarness(t)
	const url = "http://127.0.0.1/index.html?"
	navigating := &navigatingDriver{Driver: state.mock, url: url}

	state.run(t, Options{Duration: 100 * time.Millisecond, MaxSteps: 3, Driver: navigating})
	if err := state.writer.Close(); err != nil {
		t.Fatalf("close trace: %v", err)
	}

	recorded := 0
	for _, step := range readTraceLines(t, state.writer.Directory()) {
		for _, navigation := range step.Navigations {
			recorded++
			if navigation.URL != url {
				t.Errorf("step %d records navigation to %q, want %q", step.Step, navigation.URL, url)
			}
			if navigation.UnixMillis == 0 {
				t.Errorf("step %d records a navigation with no timestamp", step.Step)
			}
		}
	}
	if recorded == 0 {
		t.Fatal("the page navigated on every step and the trace holds no record of it")
	}
}
