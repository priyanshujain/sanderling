package mock

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/priyanshujain/uatu/internal/driver"
)

func TestNew_Defaults(t *testing.T) {
	mock := New()
	if !mock.HealthInfo.Ready {
		t.Errorf("default HealthInfo should be ready")
	}
	if mock.HealthInfo.Platform != "android" {
		t.Errorf("default platform: got %q", mock.HealthInfo.Platform)
	}
	if mock.HierarchyJSON == "" {
		t.Errorf("default hierarchy should be a non-empty JSON")
	}
	if len(mock.Actions()) != 0 {
		t.Errorf("fresh mock should have zero recorded actions")
	}
}

func TestRecordsAllActionsInOrder(t *testing.T) {
	mock := New()
	ctx := context.Background()

	if err := mock.Launch(ctx, "com.example", true); err != nil {
		t.Fatal(err)
	}
	if err := mock.Tap(ctx, 100, 200); err != nil {
		t.Fatal(err)
	}
	if err := mock.InputText(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := mock.Hierarchy(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := mock.Screenshot(ctx); err != nil {
		t.Fatal(err)
	}
	if err := mock.WaitForIdle(ctx, 500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if _, err := mock.Health(ctx); err != nil {
		t.Fatal(err)
	}
	if err := mock.Terminate(ctx); err != nil {
		t.Fatal(err)
	}

	actions := mock.Actions()
	expected := []ActionKind{
		ActionLaunch,
		ActionTap,
		ActionInputText,
		ActionHierarchy,
		ActionScreenshot,
		ActionWaitForIdle,
		ActionHealth,
		ActionTerminate,
	}
	if len(actions) != len(expected) {
		t.Fatalf("recorded %d actions, want %d", len(actions), len(expected))
	}
	for index, kind := range expected {
		if actions[index].Kind != kind {
			t.Errorf("action[%d]: got %q, want %q", index, actions[index].Kind, kind)
		}
	}
	if actions[0].BundleID != "com.example" || !actions[0].ClearState {
		t.Errorf("launch payload wrong: %+v", actions[0])
	}
	if actions[1].X != 100 || actions[1].Y != 200 {
		t.Errorf("tap payload wrong: %+v", actions[1])
	}
	if actions[2].Text != "hello" {
		t.Errorf("input payload wrong: %+v", actions[2])
	}
	if actions[5].Idle != 500*time.Millisecond {
		t.Errorf("idle payload wrong: %+v", actions[5])
	}
}

func TestProgrammableHierarchyIsReturned(t *testing.T) {
	mock := New()
	mock.HierarchyJSON = `{"children":[{"id":"login_button"}]}`

	got, err := mock.Hierarchy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != mock.HierarchyJSON {
		t.Errorf("hierarchy round-trip failed: got %q", got)
	}
}

func TestProgrammableScreenshotIsReturned(t *testing.T) {
	mock := New()
	mock.ImageData = driver.Image{PNG: []byte{0x89, 0x50, 0x4e, 0x47}, Width: 1080, Height: 2340}

	got, err := mock.Screenshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 1080 || got.Height != 2340 || len(got.PNG) != 4 {
		t.Errorf("screenshot round-trip failed: %+v", got)
	}
}

func TestFailureInjection(t *testing.T) {
	boom := errors.New("boom")
	mock := New()
	mock.Failures[ActionTap] = boom

	if err := mock.Tap(context.Background(), 0, 0); !errors.Is(err, boom) {
		t.Fatalf("expected boom, got %v", err)
	}
	if got := mock.Actions(); len(got) != 0 {
		t.Errorf("failed action should not be recorded, got %v", got)
	}
}

func TestActionsReturnsCopy(t *testing.T) {
	mock := New()
	_ = mock.Tap(context.Background(), 1, 1)
	snapshot := mock.Actions()
	snapshot[0].X = 99
	if mock.Actions()[0].X != 1 {
		t.Errorf("Actions() returned the internal slice; tests can mutate driver state")
	}
}

func TestSatisfiesDriverInterface(t *testing.T) {
	var _ driver.Driver = New()
}
