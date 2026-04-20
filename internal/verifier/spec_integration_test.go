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

const homeHierarchyXML = `<?xml version="1.0" encoding="UTF-8"?>
<hierarchy rotation="0">
  <node index="0" class="android.widget.FrameLayout" package="dev.uatu.sample" bounds="[0,0][1080,2400]">
    <node index="0" class="android.view.View" content-desc="logout_button" clickable="true" bounds="[980,80][1060,160]" />
    <node index="1" class="android.view.View" content-desc="account_card:acc-1" clickable="true" bounds="[64,320][1016,440]" />
    <node index="2" class="android.view.View" content-desc="account_card:acc-2" clickable="true" bounds="[64,460][1016,580]" />
    <node index="3" class="android.view.View" content-desc="add_account_button" clickable="true" bounds="[64,2200][1016,2320]" />
  </node>
</hierarchy>`

const addAccountHierarchyXML = `<?xml version="1.0" encoding="UTF-8"?>
<hierarchy rotation="0">
  <node index="0" class="android.widget.FrameLayout" package="dev.uatu.sample" bounds="[0,0][1080,2400]">
    <node index="0" class="android.view.View" content-desc="Back" clickable="true" bounds="[32,80][112,160]" />
    <node index="1" class="android.view.View" content-desc="account_name_field" clickable="true" bounds="[64,320][1016,440]" />
    <node index="2" class="android.view.View" content-desc="add_account_submit" clickable="true" bounds="[64,2200][1016,2320]" />
  </node>
</hierarchy>`

const ledgerHierarchyXML = `<?xml version="1.0" encoding="UTF-8"?>
<hierarchy rotation="0">
  <node index="0" class="android.widget.FrameLayout" package="dev.uatu.sample" bounds="[0,0][1080,2400]">
    <node index="0" class="android.view.View" content-desc="Back" clickable="true" bounds="[32,80][112,160]" />
    <node index="1" class="android.view.View" content-desc="add_txn_button" clickable="true" bounds="[64,2200][1016,2320]" />
  </node>
</hierarchy>`

const addTxnHierarchyXML = `<?xml version="1.0" encoding="UTF-8"?>
<hierarchy rotation="0">
  <node index="0" class="android.widget.FrameLayout" package="dev.uatu.sample" bounds="[0,0][1080,2400]">
    <node index="0" class="android.view.View" content-desc="Back" clickable="true" bounds="[32,80][112,160]" />
    <node index="1" class="android.view.View" content-desc="txn_credit" clickable="true" bounds="[64,280][540,360]" />
    <node index="2" class="android.view.View" content-desc="txn_debit" clickable="true" bounds="[540,280][1016,360]" />
    <node index="3" class="android.view.View" content-desc="txn_amount" clickable="true" bounds="[64,440][1016,560]" />
    <node index="4" class="android.view.View" content-desc="txn_note" clickable="true" bounds="[64,600][1016,720]" />
    <node index="5" class="android.view.View" content-desc="txn_submit" clickable="true" bounds="[64,2200][1016,2320]" />
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

// twoAccountsJSON is shared between home, ledger, and add-transaction
// snapshots so invariants that correlate accounts with ledger rows stay
// consistent across routes.
const twoAccountsJSON = `[` +
	`{"id":"acc-1","name":"Checking","balance":0,"txnCount":0},` +
	`{"id":"acc-2","name":"Savings","balance":0,"txnCount":0}` +
	`]`

func homeSnapshots() Snapshots {
	return Snapshots{
		"route":               json.RawMessage(`"home"`),
		"logged_in":           json.RawMessage(`true`),
		"auth_status":         json.RawMessage(`"logged-in"`),
		"account_count":       json.RawMessage(`2`),
		"accounts":            json.RawMessage(twoAccountsJSON),
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

func addAccountSnapshots() Snapshots {
	s := homeSnapshots()
	s["route"] = json.RawMessage(`"add-account"`)
	return s
}

func ledgerSnapshots() Snapshots {
	s := homeSnapshots()
	s["route"] = json.RawMessage(`"ledger"`)
	s["active_account_id"] = json.RawMessage(`"acc-1"`)
	return s
}

func addTxnSnapshots() Snapshots {
	s := ledgerSnapshots()
	s["route"] = json.RawMessage(`"add-transaction"`)
	s["txn_form_type"] = json.RawMessage(`"credit"`)
	s["txn_form_account_id"] = json.RawMessage(`"acc-1"`)
	return s
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

// TestSampleAppSpecActionsFireOnEachRoute pushes a hierarchy + snapshot pair
// representative of each sample-app route and verifies the expected action
// generators fire against that state. Guards against silent breakage of any
// one route's generators (a regression only e2e would otherwise catch).
func TestSampleAppSpecActionsFireOnEachRoute(t *testing.T) {
	cases := []struct {
		name       string
		xml        string
		snapshots  Snapshots
		expectKind ActionKind
		expectOns  []string
	}{
		{
			name:       "home",
			xml:        homeHierarchyXML,
			snapshots:  homeSnapshots(),
			expectKind: ActionKindTap,
			expectOns: []string{
				"desc:add_account_button",
				"desc:logout_button",
				"descPrefix:account_card:",
			},
		},
		{
			name:       "add-account",
			xml:        addAccountHierarchyXML,
			snapshots:  addAccountSnapshots(),
			expectKind: ActionKindTap,
			expectOns:  []string{"desc:add_account_submit", "desc:Back"},
		},
		{
			name:       "ledger",
			xml:        ledgerHierarchyXML,
			snapshots:  ledgerSnapshots(),
			expectKind: ActionKindTap,
			expectOns:  []string{"desc:add_txn_button", "desc:Back"},
		},
		{
			name:       "add-transaction",
			xml:        addTxnHierarchyXML,
			snapshots:  addTxnSnapshots(),
			expectKind: ActionKindTap,
			expectOns: []string{
				"desc:txn_submit",
				"desc:txn_debit",
				"desc:Back",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := newVerifier(t)
			if err := v.Load(bundleSampleAppSpec(t)); err != nil {
				t.Fatal(err)
			}
			tree, err := hierarchy.Parse(tc.xml)
			if err != nil {
				t.Fatal(err)
			}
			if err := v.PushSnapshot(SnapshotInput{Snapshots: tc.snapshots, Tree: tree}); err != nil {
				t.Fatal(err)
			}
			sawOn := map[string]bool{}
			sawInputText := map[string]bool{}
			for range 800 {
				action, err := v.NextAction()
				if err != nil {
					continue
				}
				if action.Kind == tc.expectKind {
					sawOn[action.On] = true
				}
				if action.Kind == ActionKindInputText {
					sawInputText[action.On] = true
				}
			}
			for _, on := range tc.expectOns {
				if !sawOn[on] {
					t.Errorf("%s: no %s action on %q; saw %v", tc.name, tc.expectKind, on, keysOf(sawOn))
				}
			}
			if tc.name == "add-account" && !sawInputText["desc:account_name_field"] {
				t.Errorf("add-account: typeAccountName never fired; saw %v", keysOf(sawInputText))
			}
			if tc.name == "add-transaction" {
				if !sawInputText["desc:txn_amount"] {
					t.Errorf("add-transaction: typeAmount never fired; saw %v", keysOf(sawInputText))
				}
				if !sawInputText["desc:txn_note"] {
					t.Errorf("add-transaction: typeNote never fired; saw %v", keysOf(sawInputText))
				}
			}
		})
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
