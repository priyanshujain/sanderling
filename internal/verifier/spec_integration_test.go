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
      <node index="0" class="android.widget.TextView" text="Sign in" bounds="[100,200][900,300]" />
      <node index="1" class="android.widget.EditText" content-desc="phone_field" clickable="true" enabled="true" bounds="[100,400][900,520]" />
      <node index="2" class="android.widget.Button" text="Continue" clickable="true" enabled="true" bounds="[400,800][680,920]" />
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
	defaultsPath, err := filepath.Abs("../../pkg/spec-api/src/defaults/properties.ts")
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := bundler.Bundle(bundler.Options{
		EntryFile: specPath,
		Aliases: map[string]string{
			"@uatu/spec":                    apiPath,
			"@uatu/spec/defaults/properties": defaultsPath,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(bundle.JavaScript)
}

// TestSampleAppSpecFiresLoginActions verifies the bundled sample-app spec
// emits Tap/InputText actions targeting the login screen elements when they
// are present in the hierarchy.
func TestSampleAppSpecFiresLoginActions(t *testing.T) {
	v := newVerifier(t)
	if err := v.Load(bundleSampleAppSpec(t)); err != nil {
		t.Fatal(err)
	}

	tree, err := hierarchy.Parse(sampleAppHierarchyXML)
	if err != nil {
		t.Fatal(err)
	}
	snapshots := Snapshots{
		"route":         json.RawMessage(`"login"`),
		"logged_in":     json.RawMessage(`false`),
		"account_count": json.RawMessage(`0`),
	}
	if err := v.PushSnapshot(SnapshotInput{Snapshots: snapshots, Tree: tree}); err != nil {
		t.Fatal(err)
	}

	tapContinueHits := 0
	typePhoneHits := 0
	for range 400 {
		action, err := v.NextAction()
		if err != nil {
			continue
		}
		switch {
		case action.Kind == ActionKindTap && action.On == "text:Continue":
			tapContinueHits++
		case action.Kind == ActionKindInputText && action.On == "desc:phone_field":
			typePhoneHits++
		}
	}
	if tapContinueHits == 0 {
		t.Fatal("tapContinue never fired on sample-app hierarchy")
	}
	if typePhoneHits == 0 {
		t.Fatal("typePhone never fired on sample-app hierarchy")
	}
}

// TestSampleAppSpecPropertiesEvaluate checks the properties declared in the
// sample-app spec evaluate sensibly across a small snapshot sequence. The
// spec mixes safety and liveness properties; Pending verdicts are expected
// for liveness properties that haven't had time to resolve yet.
func TestSampleAppSpecPropertiesEvaluate(t *testing.T) {
	v := newVerifier(t)
	if err := v.Load(bundleSampleAppSpec(t)); err != nil {
		t.Fatal(err)
	}

	tree, err := hierarchy.Parse(sampleAppHierarchyXML)
	if err != nil {
		t.Fatal(err)
	}
	snapshots := Snapshots{
		"route":         json.RawMessage(`"login"`),
		"logged_in":     json.RawMessage(`false`),
		"account_count": json.RawMessage(`0`),
	}
	if err := v.PushSnapshot(SnapshotInput{Snapshots: snapshots, Tree: tree}); err != nil {
		t.Fatal(err)
	}
	verdicts := v.EvaluateProperties()
	if verdicts["accountCountNonNegative"] != ltl.VerdictHolds {
		t.Errorf("accountCountNonNegative: got %v, want holds", verdicts["accountCountNonNegative"])
	}
	if verdicts["noUncaughtExceptions"] != ltl.VerdictHolds {
		t.Errorf("noUncaughtExceptions: got %v, want holds", verdicts["noUncaughtExceptions"])
	}
	// Liveness: eventuallyLoggedIn hasn't resolved yet.
	if verdicts["eventuallyLoggedIn"] != ltl.VerdictPending {
		t.Errorf("eventuallyLoggedIn: got %v, want pending", verdicts["eventuallyLoggedIn"])
	}
}
