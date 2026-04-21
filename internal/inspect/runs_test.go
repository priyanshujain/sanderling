package inspect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/trace"
)

func TestScan_OrdersByStartedAtDescendingAndCountsViolations(t *testing.T) {
	root := t.TempDir()
	older := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	writeRun(t, root, "older", trace.Meta{StartedAt: older, EndedAt: timePointer(older.Add(2 * time.Second))}, []trace.Step{
		{Index: 1, Timestamp: older, Violations: []string{"propA"}},
		{Index: 2, Timestamp: older.Add(time.Second)},
	})
	writeRun(t, root, "newer", trace.Meta{StartedAt: newer, EndedAt: timePointer(newer.Add(time.Second))}, []trace.Step{
		{Index: 1, Timestamp: newer, Violations: []string{"propA", "propB"}},
	})

	summaries, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("len(summaries) = %d, want 2", len(summaries))
	}
	if summaries[0].ID != "newer" {
		t.Errorf("first id = %q, want newer", summaries[0].ID)
	}
	if summaries[0].ViolationCount != 2 {
		t.Errorf("newer violations = %d, want 2", summaries[0].ViolationCount)
	}
	if summaries[1].ViolationCount != 1 {
		t.Errorf("older violations = %d, want 1", summaries[1].ViolationCount)
	}
	if summaries[1].StepCount != 2 {
		t.Errorf("older steps = %d, want 2", summaries[1].StepCount)
	}
	if summaries[0].DurationMillis != 1000 {
		t.Errorf("newer duration = %d, want 1000", summaries[0].DurationMillis)
	}
}

func TestScan_MissingDirectoryReturnsEmpty(t *testing.T) {
	summaries, err := Scan(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("len(summaries) = %d, want 0", len(summaries))
	}
}

func TestScan_MissingEndedAtSurfacesInProgress(t *testing.T) {
	root := t.TempDir()
	writeRun(t, root, "live", trace.Meta{StartedAt: time.Now().UTC()}, []trace.Step{
		{Index: 1, Timestamp: time.Now().UTC()},
	})
	summaries, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(summaries) != 1 || !summaries[0].InProgress {
		t.Errorf("expected in_progress=true, got %+v", summaries)
	}
	if summaries[0].EndedAt != nil {
		t.Errorf("ended_at should be nil, got %v", summaries[0].EndedAt)
	}
}

func TestScan_EmptyTraceTreatedAsZeroSteps(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "empty")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := trace.Meta{StartedAt: time.Now().UTC()}
	body, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(directory, "meta.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	summaries, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("len = %d", len(summaries))
	}
	if summaries[0].StepCount != 0 {
		t.Errorf("step_count = %d, want 0", summaries[0].StepCount)
	}
	if !summaries[0].InProgress {
		t.Error("expected in_progress")
	}
}

func TestCacheStep_LazyDecodeReturnsFullStep(t *testing.T) {
	root := t.TempDir()
	startedAt := time.Now().UTC()
	steps := []trace.Step{
		{Index: 1, Timestamp: startedAt, Screen: "A"},
		{Index: 2, Timestamp: startedAt.Add(time.Second), Screen: "B", Action: &trace.Action{Kind: "tap"}},
		{Index: 3, Timestamp: startedAt.Add(2 * time.Second), Screen: "C", Violations: []string{"prop1"}},
	}
	writeRun(t, root, "r1", trace.Meta{StartedAt: startedAt, EndedAt: timePointer(startedAt.Add(3 * time.Second))}, steps)

	cache := NewCache(root)
	run, err := cache.Open("r1")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(run.Steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(run.Steps))
	}
	if !run.Steps[2].HasViolations {
		t.Error("step 3 should HasViolations")
	}
	if run.Steps[1].ActionKind != "tap" {
		t.Errorf("step 2 action = %q, want tap", run.Steps[1].ActionKind)
	}

	for _, target := range []int{1, 2, 3} {
		step, err := cache.Step(run, target)
		if err != nil {
			t.Fatalf("Step(%d): %v", target, err)
		}
		if step.Index != target {
			t.Errorf("Step(%d).Index = %d", target, step.Index)
		}
	}
	if _, err := cache.Step(run, 99); err == nil {
		t.Error("expected error for out-of-range step")
	}
}

func TestDecodeStepSummary_ActionLabelPerKind(t *testing.T) {
	cases := []struct {
		line      string
		wantKind  string
		wantLabel string
	}{
		{`{"step":1,"timestamp":"2026-04-20T10:00:00Z","action":{"kind":"Tap","selector":"id:save"}}`, "Tap", "id:save"},
		{`{"step":2,"timestamp":"2026-04-20T10:00:01Z","action":{"kind":"Tap","x":140,"y":220}}`, "Tap", "(140,220)"},
		{`{"step":3,"timestamp":"2026-04-20T10:00:02Z","action":{"kind":"InputText","text":"alice"}}`, "InputText", `"alice"`},
		{`{"step":4,"timestamp":"2026-04-20T10:00:03Z","action":{"kind":"Swipe","from_x":10,"from_y":500,"to_x":10,"to_y":50}}`, "Swipe", "up"},
		{`{"step":5,"timestamp":"2026-04-20T10:00:04Z","action":{"kind":"Swipe","from_x":100,"from_y":50,"to_x":600,"to_y":50}}`, "Swipe", "right"},
		{`{"step":6,"timestamp":"2026-04-20T10:00:05Z","action":{"kind":"PressKey","key":"back"}}`, "PressKey", "back"},
		{`{"step":7,"timestamp":"2026-04-20T10:00:06Z","action":{"kind":"Wait","duration_millis":500}}`, "Wait", "500ms"},
	}
	for _, tc := range cases {
		summary, _, err := decodeStepSummary([]byte(tc.line))
		if err != nil {
			t.Fatalf("decode %s: %v", tc.line, err)
		}
		if summary.ActionKind != tc.wantKind {
			t.Errorf("kind = %q, want %q (line=%s)", summary.ActionKind, tc.wantKind, tc.line)
		}
		if summary.ActionLabel != tc.wantLabel {
			t.Errorf("label = %q, want %q (line=%s)", summary.ActionLabel, tc.wantLabel, tc.line)
		}
	}
}

func TestCacheOpen_RejectsTraversalIDs(t *testing.T) {
	cache := NewCache(t.TempDir())
	for _, id := range []string{"", ".", "..", "../etc", "a/b", "a\\b"} {
		if _, err := cache.Open(id); err == nil {
			t.Errorf("Open(%q) should fail", id)
		}
	}
}

func TestIsRunDirectory(t *testing.T) {
	root := t.TempDir()
	writeRun(t, root, "x", trace.Meta{StartedAt: time.Now().UTC()}, nil)
	if !IsRunDirectory(filepath.Join(root, "x")) {
		t.Error("expected true for run dir")
	}
	if IsRunDirectory(root) {
		t.Error("expected false for parent dir")
	}
}

func writeRun(t *testing.T, root, id string, meta trace.Meta, steps []trace.Step) {
	t.Helper()
	directory := filepath.Join(root, id)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	metaBody, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "meta.json"), metaBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if steps == nil {
		return
	}
	file, err := os.Create(filepath.Join(directory, "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	for _, step := range steps {
		if err := encoder.Encode(step); err != nil {
			t.Fatal(err)
		}
	}
}

func timePointer(t time.Time) *time.Time { return &t }
