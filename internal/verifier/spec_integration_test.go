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
      <node index="1" class="android.view.View" content-desc="login_email" clickable="true" enabled="true" bounds="[100,400][900,520]" />
      <node index="2" class="android.view.View" content-desc="login_password" clickable="true" enabled="true" bounds="[100,560][900,680]" />
      <node index="3" class="android.view.View" content-desc="login_submit" clickable="true" enabled="true" bounds="[100,800][900,920]" />
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
			"@uatu/spec":                     apiPath,
			"@uatu/spec/defaults/properties": defaultsPath,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(bundle.JavaScript)
}

func loginSnapshots() Snapshots {
	return Snapshots{
		"route":               json.RawMessage(`"login"`),
		"logged_in":           json.RawMessage(`false`),
		"auth_status":         json.RawMessage(`"logged-out"`),
		"account_count":       json.RawMessage(`0`),
		"accounts":            json.RawMessage(`[]`),
		"total_balance":       json.RawMessage(`0`),
		"active_account_id":   json.RawMessage(`null`),
		"ledger_rows":         json.RawMessage(`[]`),
		"ledger_balance":      json.RawMessage(`0`),
		"focused_input":       json.RawMessage(`null`),
		"txn_form_type":       json.RawMessage(`null`),
		"txn_form_account_id": json.RawMessage(`null`),
		"login_error":         json.RawMessage(`""`),
		"add_account_error":   json.RawMessage(`""`),
		"txn_error":           json.RawMessage(`""`),
	}
}

// TestSampleAppSpecFiresLoginActions verifies the bundled sample-app spec
// emits Tap actions targeting the login screen elements when they are present
// in the hierarchy.
func TestSampleAppSpecFiresLoginActions(t *testing.T) {
	v := newVerifier(t)
	if err := v.Load(bundleSampleAppSpec(t)); err != nil {
		t.Fatal(err)
	}

	tree, err := hierarchy.Parse(sampleAppHierarchyXML)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.PushSnapshot(SnapshotInput{Snapshots: loginSnapshots(), Tree: tree}); err != nil {
		t.Fatal(err)
	}

	typeEmailHits := 0
	tapSubmitHits := 0
	for range 400 {
		action, err := v.NextAction()
		if err != nil {
			continue
		}
		switch {
		case action.Kind == ActionKindInputText && action.On == "desc:login_email":
			typeEmailHits++
		case action.Kind == ActionKindTap && action.On == "desc:login_submit":
			tapSubmitHits++
		}
	}
	if typeEmailHits == 0 {
		t.Fatal("loginHelper never typed into desc:login_email on sample-app hierarchy")
	}
	if tapSubmitHits == 0 {
		t.Fatal("adversarialLogin never tapped desc:login_submit on sample-app hierarchy")
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
	if err := v.PushSnapshot(SnapshotInput{Snapshots: loginSnapshots(), Tree: tree}); err != nil {
		t.Fatal(err)
	}
	verdicts := v.EvaluateProperties()
	if verdicts["accountCountNonNegative"] != ltl.VerdictHolds {
		t.Errorf("accountCountNonNegative: got %v, want holds", verdicts["accountCountNonNegative"])
	}
	if verdicts["noUncaughtExceptions"] != ltl.VerdictHolds {
		t.Errorf("noUncaughtExceptions: got %v, want holds", verdicts["noUncaughtExceptions"])
	}
	if verdicts["authStatusIsKnown"] != ltl.VerdictHolds {
		t.Errorf("authStatusIsKnown: got %v, want holds", verdicts["authStatusIsKnown"])
	}
	if verdicts["routeIsKnown"] != ltl.VerdictHolds {
		t.Errorf("routeIsKnown: got %v, want holds", verdicts["routeIsKnown"])
	}
	// Liveness: loginReachable hasn't resolved yet.
	if verdicts["loginReachable"] != ltl.VerdictPending {
		t.Errorf("loginReachable: got %v, want pending", verdicts["loginReachable"])
	}
}
