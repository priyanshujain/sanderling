package verifier

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/priyanshujain/uatu/internal/bundler"
	"github.com/priyanshujain/uatu/internal/hierarchy"
	"github.com/priyanshujain/uatu/internal/ltl"
)

const sampleAppHierarchyXML = `<?xml version="1.0" encoding="UTF-8"?>
<hierarchy rotation="0">
  <node index="0" class="android.widget.FrameLayout" package="dev.uatu.sample" bounds="[0,0][1080,2400]">
    <node index="0" class="android.widget.LinearLayout" bounds="[64,96][1016,2336]">
      <node index="0" class="android.widget.TextView" text="Clicks: 0" bounds="[100,200][900,300]" />
      <node index="1" class="android.widget.Button" text="Click me" clickable="true" enabled="true" bounds="[400,800][680,920]" />
    </node>
  </node>
</hierarchy>`

// bundleSampleAppSpec bundles examples/sample-app/spec.ts via the real
// @uatu/spec API so the integration test exercises the same path the CLI uses.
func bundleSampleAppSpec(t *testing.T) string {
	t.Helper()
	specPath, err := filepath.Abs("../../examples/sample-app/spec.ts")
	if err != nil {
		t.Fatal(err)
	}
	apiPath, err := filepath.Abs("../../pkg/spec-api/src/index.ts")
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := bundler.Bundle(bundler.Options{
		EntryFile: specPath,
		Aliases:   map[string]string{"@uatu/spec": apiPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(bundle.JavaScript)
}

// TestSampleAppSpecTapsClickMe verifies the bundled sample-app spec emits a
// Tap on the "Click me" button when that button is present in the hierarchy.
func TestSampleAppSpecTapsClickMe(t *testing.T) {
	v := newVerifier(t)
	if err := v.Load(bundleSampleAppSpec(t)); err != nil {
		t.Fatal(err)
	}

	tree, err := hierarchy.Parse(sampleAppHierarchyXML)
	if err != nil {
		t.Fatal(err)
	}
	snapshots := Snapshots{
		"app_state":   json.RawMessage(`"running"`),
		"click_count": json.RawMessage(`0`),
	}
	if err := v.PushSnapshot(snapshots, tree); err != nil {
		t.Fatal(err)
	}

	tapHits := 0
	for range 200 {
		action, err := v.NextAction()
		if err != nil {
			continue
		}
		if action.Kind == ActionKindTap && action.On == "text:Click me" {
			tapHits++
		}
	}
	if tapHits == 0 {
		t.Fatal("tapClickMe never fired on sample-app hierarchy")
	}
}

// TestSampleAppSpecPropertiesHold checks the three properties declared in the
// sample-app spec evaluate correctly across a realistic snapshot sequence.
func TestSampleAppSpecPropertiesHold(t *testing.T) {
	v := newVerifier(t)
	if err := v.Load(bundleSampleAppSpec(t)); err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		appState   string
		clickCount int
		want       map[string]ltl.Verdict
	}{
		{"running", 0, map[string]ltl.Verdict{
			"appIsRunning":             ltl.VerdictHolds,
			"clickCountNonNegative":    ltl.VerdictHolds,
			"clickCountNeverDecreases": ltl.VerdictHolds,
		}},
		{"running", 5, map[string]ltl.Verdict{
			"appIsRunning":             ltl.VerdictHolds,
			"clickCountNonNegative":    ltl.VerdictHolds,
			"clickCountNeverDecreases": ltl.VerdictHolds,
		}},
		{"running", 3, map[string]ltl.Verdict{
			"appIsRunning":             ltl.VerdictHolds,
			"clickCountNonNegative":    ltl.VerdictHolds,
			"clickCountNeverDecreases": ltl.VerdictViolated,
		}},
	}

	for index, step := range steps {
		stateRaw, _ := json.Marshal(step.appState)
		countRaw, _ := json.Marshal(step.clickCount)
		if err := v.PushSnapshot(Snapshots{
			"app_state":   stateRaw,
			"click_count": countRaw,
		}, nil); err != nil {
			t.Fatalf("step %d: %v", index, err)
		}
		got := v.EvaluateProperties()
		for property, want := range step.want {
			if got[property] != want {
				t.Errorf("step %d %q: got %v, want %v", index, property, got[property], want)
			}
		}
	}
}
