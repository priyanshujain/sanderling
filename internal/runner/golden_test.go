package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/verifier"
)

// mustSeededVerifier builds a verifier with a fixed seed so the JS picker's PRNG
// is reproducible across runs, making the trace byte-stable.
func mustSeededVerifier(t *testing.T, spec string, seed uint64) *verifier.Verifier {
	t.Helper()
	instance, err := verifier.New(verifier.WithSeed(seed))
	if err != nil {
		t.Fatal(err)
	}
	if err := instance.Load(bundleSpec(t, spec)); err != nil {
		t.Fatal(err)
	}
	return instance
}

// goldenSeed and goldenSteps fix the run so the mock-driven trace and summary
// are byte-reproducible. Set UPDATE_GOLDEN=1 to rewrite the committed goldens;
// the checked-in files are the asserted source of truth.
const (
	goldenSeed  = 0x5eed
	goldenSteps = 4
)

// canonicalizeTrace rewrites the sole non-deterministic field, "timestamp"
// (wall-clock at step start), to a fixed zero value so equality compares only
// the run's observable shape. Every other field is a pure function of the seed,
// the spec, and the programmed mock hierarchy.
func canonicalizeTrace(t *testing.T, raw []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var line map[string]json.RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("decode trace line: %v", err)
		}
		line["timestamp"] = json.RawMessage(`"0001-01-01T00:00:00Z"`)
		encoded, err := json.Marshal(line)
		if err != nil {
			t.Fatalf("encode trace line: %v", err)
		}
		out.Write(encoded)
		out.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	return out.Bytes()
}

func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", name, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with UPDATE_GOLDEN=1 to create): %v", name, err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("golden %s mismatch\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}

// TestGolden_TraceStreamIsReproducible drives a fixed-seed, fixed-step run
// against the mock driver and snapshots the canonicalized trace.jsonl stream.
// A drift here means the run is no longer deterministic or the trace shape
// changed; both must be reviewed, not papered over.
func TestGolden_TraceStreamIsReproducible(t *testing.T) {
	state := newHarnessWithSpec(t, fixtureSpec)
	state.verifier = mustSeededVerifier(t, fixtureSpec, goldenSeed)
	state.mock.HierarchyJSON = `{"attributes":{"resource-id":"HomeScreen"},"children":[{"attributes":{"resource-id":"next","bounds":"[40,80,240,160]"},"children":[],"clickable":true,"enabled":true}]}`

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := Run(ctx, Options{
		Duration:    time.Hour,
		IdleTimeout: 10 * time.Millisecond,
		MaxSteps:    goldenSteps,
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(state.writer.Directory(), "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "trace.jsonl", canonicalizeTrace(t, raw))
}

// TestGolden_ViolationSummaryIsReproducible drives a known violation and
// snapshots the rendered summary string produced by RenderSummary (the same
// function the CLI prints). It also asserts the violated property is named, so
// a regression that drops the property from the report cannot pass silently.
func TestGolden_ViolationSummaryIsReproducible(t *testing.T) {
	state := newHarnessWithSpec(t, violationSpec)
	state.verifier = mustSeededVerifier(t, violationSpec, goldenSeed)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	summary, err := Run(ctx, Options{
		Duration:    time.Hour,
		IdleTimeout: 10 * time.Millisecond,
		MaxSteps:    goldenSteps,
		Driver:      state.mock,
		Verifier:    state.verifier,
		TraceWriter: state.writer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var rendered bytes.Buffer
	RenderSummary(&rendered, summary, "android")

	if !strings.Contains(rendered.String(), "balanceNonNegative") {
		t.Errorf("summary must name the violated property balanceNonNegative, got:\n%s", rendered.String())
	}
	assertGolden(t, "violation-summary.txt", rendered.Bytes())
}
