package verifier

import (
	"encoding/json"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/priyanshujain/uatu/internal/bundler"
	"github.com/priyanshujain/uatu/internal/hierarchy"
)

// TestSpecDrivesLanguageThenMobile reproduces the real run path: on the
// language screen the spec must produce Tap(english); on the mobile screen
// the language-select generator must return [] so selectEnglish never fires.
func TestSpecDrivesLanguageThenMobile(t *testing.T) {
	languageXML, err := os.ReadFile("/tmp/live-dump.xml")
	if err != nil {
		t.Skip("live-dump.xml not present")
	}
	mobileXML, err := os.ReadFile("/tmp/mobile-real.xml")
	if err != nil {
		t.Skip("mobile-real.xml not present")
	}

	specPath, err := filepath.Abs("../../examples/specs/merchant-ledger.ts")
	if err != nil {
		t.Fatal(err)
	}
	apiPath, err := filepath.Abs("../../pkg/spec-api/src/index.ts")
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := bundler.Bundle(bundler.Options{
		EntryFile: specPath,
		Defines:   map[string]string{"UATU_TEST_PHONE": "7509657590", "UATU_TEST_OTP": "000000"},
		Aliases:   map[string]string{"@uatu/spec": apiPath},
	})
	if err != nil {
		t.Fatal(err)
	}

	v := newVerifier(t)
	if err := v.Load(string(bundle.JavaScript)); err != nil {
		t.Fatal(err)
	}

	languageTree, err := hierarchy.Parse(string(languageXML))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.PushSnapshot(Snapshots{"screen": json.RawMessage(`"customer_ledger"`)}, languageTree); err != nil {
		t.Fatal(err)
	}
	langCounts := map[string]int{}
	for range 200 {
		action, err := v.NextAction()
		if err != nil {
			langCounts["noAction"]++
			continue
		}
		key := string(action.Kind) + ":" + action.On
		langCounts[key]++
	}
	t.Logf("language-screen action counts: %+v", langCounts)
	if langCounts["Tap:text:English"] == 0 {
		t.Fatal("selectEnglish never fired on language screen")
	}

	mobileTree, err := hierarchy.Parse(string(mobileXML))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.PushSnapshot(Snapshots{"screen": json.RawMessage(`"enter_mobile"`)}, mobileTree); err != nil {
		t.Fatal(err)
	}

	// Fire NextAction many times and confirm selectEnglish is never picked.
	englishHits := 0
	for range 200 {
		action, err := v.NextAction()
		if err != nil {
			continue
		}
		if action.Kind == ActionKindTap && action.On == "text:English" {
			englishHits++
			t.Logf("unexpected Tap text:English on mobile screen, action=%+v", action)
		}
	}
	if englishHits > 0 {
		t.Fatalf("selectEnglish leaked into mobile screen %d times", englishHits)
	}
}

// TestSpecDismissesMultiDeviceConfirm makes sure the confirm-dialog gate
// actually fires — the live run got stuck here.
func TestSpecDismissesMultiDeviceConfirm(t *testing.T) {
	dialogXML, err := os.ReadFile("/tmp/confirm-dump.xml")
	if err != nil {
		t.Skip("confirm-dump.xml not present")
	}
	specPath, _ := filepath.Abs("../../examples/specs/merchant-ledger.ts")
	apiPath, _ := filepath.Abs("../../pkg/spec-api/src/index.ts")
	bundle, err := bundler.Bundle(bundler.Options{
		EntryFile: specPath,
		Defines:   map[string]string{"UATU_TEST_PHONE": "7509657590", "UATU_TEST_OTP": "000000"},
		Aliases:   map[string]string{"@uatu/spec": apiPath},
	})
	if err != nil {
		t.Fatal(err)
	}

	v, err := New(WithRand(rand.New(rand.NewPCG(42, 0))))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Load(string(bundle.JavaScript)); err != nil {
		t.Fatal(err)
	}

	tree, err := hierarchy.Parse(string(dialogXML))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.PushSnapshot(Snapshots{}, tree); err != nil {
		t.Fatal(err)
	}

	counts := map[string]int{}
	for range 400 {
		action, err := v.NextAction()
		if err != nil {
			counts["noAction"]++
			continue
		}
		key := string(action.Kind) + ":" + action.On
		counts[key]++
	}
	t.Logf("dialog-screen action counts: %+v", counts)
	if counts["Tap:text:Sign Out"] == 0 {
		t.Fatal("dismissMultiDevice confirm branch never fired on dialog hierarchy")
	}
}
