package verifier

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/priyanshujain/sanderling/internal/bundler"
	"github.com/priyanshujain/sanderling/internal/hierarchy"
	"github.com/priyanshujain/sanderling/internal/ltl"
)

// bundleIntegrationSpec bundles testdata/integration_spec.ts via the real
// @uatu/spec API so the integration test exercises the same path the CLI
// uses, with no reference to any specific example app.
func bundleIntegrationSpec(t *testing.T) string {
	t.Helper()
	specPath, err := filepath.Abs("testdata/integration_spec.ts")
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

func listSnapshots() Snapshots {
	return Snapshots{
		"route":          json.RawMessage(`"list"`),
		"item_count":     json.RawMessage(`0`),
		"has_submitted":  json.RawMessage(`false`),
	}
}

func formSnapshots() Snapshots {
	return Snapshots{
		"route":          json.RawMessage(`"form"`),
		"item_count":     json.RawMessage(`0`),
		"has_submitted":  json.RawMessage(`false`),
	}
}

// TestIntegrationSpecFiresInputActions verifies the bundled neutral spec
// emits an InputText action on the text field and a Tap action on the
// primary button when both are present in the hierarchy.
func TestIntegrationSpecFiresInputActions(t *testing.T) {
	v := newVerifier(t)
	if err := v.Load(bundleIntegrationSpec(t)); err != nil {
		t.Fatal(err)
	}

	tree, err := hierarchy.Parse(formHierarchyXML)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.PushSnapshot(SnapshotInput{Snapshots: formSnapshots(), Tree: tree}); err != nil {
		t.Fatal(err)
	}

	typeFieldHits := 0
	tapPrimaryHits := 0
	for range 400 {
		action, err := v.NextAction()
		if err != nil {
			continue
		}
		switch {
		case action.Kind == ActionKindInputText && action.On == "desc:text_field":
			typeFieldHits++
		case action.Kind == ActionKindTap && action.On == "desc:primary_action":
			tapPrimaryHits++
		}
	}
	if typeFieldHits == 0 {
		t.Fatal("typeIntoField never typed into desc:text_field on form hierarchy")
	}
	if tapPrimaryHits == 0 {
		t.Fatal("tapPrimary never tapped desc:primary_action on form hierarchy")
	}
}

// TestIntegrationSpecPropertiesEvaluate checks the properties declared in
// the neutral spec evaluate sensibly across a small snapshot sequence. The
// spec mixes safety and liveness properties; Pending verdicts are expected
// for liveness properties that haven't had time to resolve yet.
func TestIntegrationSpecPropertiesEvaluate(t *testing.T) {
	v := newVerifier(t)
	if err := v.Load(bundleIntegrationSpec(t)); err != nil {
		t.Fatal(err)
	}

	tree, err := hierarchy.Parse(listHierarchyXML)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.PushSnapshot(SnapshotInput{Snapshots: listSnapshots(), Tree: tree}); err != nil {
		t.Fatal(err)
	}
	verdicts := v.EvaluateProperties()
	if verdicts["itemCountNonNegative"] != ltl.VerdictHolds {
		t.Errorf("itemCountNonNegative: got %v, want holds", verdicts["itemCountNonNegative"])
	}
	if verdicts["routeIsKnown"] != ltl.VerdictHolds {
		t.Errorf("routeIsKnown: got %v, want holds", verdicts["routeIsKnown"])
	}
	if verdicts["noUncaughtExceptions"] != ltl.VerdictHolds {
		t.Errorf("noUncaughtExceptions: got %v, want holds", verdicts["noUncaughtExceptions"])
	}
	// Liveness: submitEventually hasn't resolved yet.
	if verdicts["submitEventually"] != ltl.VerdictPending {
		t.Errorf("submitEventually: got %v, want pending", verdicts["submitEventually"])
	}
}

// TestIntegrationSpecActionsFireOnEachRoute pushes a hierarchy + snapshot
// pair representative of each route and verifies the expected action
// generators fire against that state.
func TestIntegrationSpecActionsFireOnEachRoute(t *testing.T) {
	cases := []struct {
		name       string
		xml        string
		snapshots  Snapshots
		expectKind ActionKind
		expectOns  []string
	}{
		{
			name:       "list",
			xml:        listHierarchyXML,
			snapshots:  listSnapshots(),
			expectKind: ActionKindTap,
			expectOns:  []string{"desc:primary_action", "desc:secondary_action"},
		},
		{
			name:       "form",
			xml:        formHierarchyXML,
			snapshots:  formSnapshots(),
			expectKind: ActionKindTap,
			expectOns:  []string{"desc:primary_action", "desc:secondary_action"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := newVerifier(t)
			if err := v.Load(bundleIntegrationSpec(t)); err != nil {
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
			if tc.name == "form" && !sawInputText["desc:text_field"] {
				t.Errorf("form: typeIntoField never fired; saw %v", keysOf(sawInputText))
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
