package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/driver"
	mockdriver "github.com/priyanshujain/sanderling/internal/driver/mock"
	"github.com/priyanshujain/sanderling/internal/trace"
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Run(ctx, Options{
		Duration:    100 * time.Millisecond,
		IdleTimeout: 20 * time.Millisecond,
		MaxSteps:    3,
		Driver:      navigating,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := state.writer.Close(); err != nil {
		t.Fatalf("close trace: %v", err)
	}

	file, err := os.Open(filepath.Join(state.writer.Directory(), "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	recorded := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var step struct {
			Index       int                `json:"step"`
			Navigations []trace.Navigation `json:"navigations"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &step); err != nil {
			t.Fatalf("trace line decode: %v", err)
		}
		for _, navigation := range step.Navigations {
			recorded++
			if navigation.URL != url {
				t.Errorf("step %d records navigation to %q, want %q", step.Index, navigation.URL, url)
			}
			if navigation.UnixMillis == 0 {
				t.Errorf("step %d records a navigation with no timestamp", step.Index)
			}
		}
	}
	if recorded == 0 {
		t.Fatal("the page navigated on every step and the trace holds no record of it")
	}
}
