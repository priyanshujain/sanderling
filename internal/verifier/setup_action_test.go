package verifier

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/priyanshujain/sanderling/internal/bundler"
	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// bundleInlineSpec bundles an inline spec through the real @sanderling/spec API
// and goja runtime entry, so the bundle installs __sanderlingSetupAction__ the
// way the CLI does.
func bundleInlineSpec(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.ts")
	if err := os.WriteFile(specPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	abs := func(rel string) string {
		path, err := filepath.Abs(rel)
		if err != nil {
			t.Fatal(err)
		}
		return path
	}
	bundle, err := bundler.Bundle(bundler.Options{
		EntryFile:   specPath,
		RuntimeFile: abs("../../pkg/spec/src/goja-runtime.ts"),
		Aliases: map[string]string{
			"@sanderling/spec":                     abs("../../pkg/spec/src/index.ts"),
			"@sanderling/spec/defaults":            abs("../../pkg/spec/src/defaults/index.ts"),
			"@sanderling/spec/defaults/properties": abs("../../pkg/spec/src/defaults/properties.ts"),
		},
	})
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}
	return string(bundle.JavaScript)
}

func loadBundled(t *testing.T, source, treeJSON string) *Verifier {
	t.Helper()
	v, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Load(bundleInlineSpec(t, source)); err != nil {
		t.Fatalf("load: %v", err)
	}
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.PushSnapshot(SnapshotInput{Tree: tree}); err != nil {
		t.Fatalf("push snapshot: %v", err)
	}
	return v
}

func TestSetupActionWalksSetupOnly(t *testing.T) {
	spec := `
import { Tap, actions, taps } from "@sanderling/spec";
export const setup = actions(() => [Tap({ on: "id:SignIn" })]);
export const actionsRoot = taps;
`
	v := loadBundled(t, spec, enumTreeJSON)
	action, err := v.SetupAction()
	if err != nil {
		t.Fatalf("SetupAction: %v", err)
	}
	if action.Kind != ActionKindTap || action.On != "id:SignIn" {
		t.Errorf("SetupAction = %+v, want Tap on id:SignIn from setup", action)
	}
}

func TestSetupActionIgnoresActionsRoot(t *testing.T) {
	// No setup, but a live actionsRoot that NextAction would happily draw from.
	spec := `
import { taps } from "@sanderling/spec";
export const actionsRoot = taps;
`
	v := loadBundled(t, spec, enumTreeJSON)
	if _, err := v.SetupAction(); !errors.Is(err, ErrNoAction) {
		t.Fatalf("SetupAction err = %v, want ErrNoAction when no setup is declared", err)
	}
	// Sanity: the seeded root would have produced an action, proving SetupAction
	// deliberately skips it.
	if _, err := v.NextAction(); err != nil {
		t.Fatalf("NextAction should draw from actionsRoot: %v", err)
	}
}

// TestNextActionNamesTheGeneratorThatProducedIt is the native half of the
// cross-host marker contract: for the same spec shape, a setup-driven step and
// an action-root step must name different producers. The web half asserts the
// same two names in pkg/spec/test/web-runtime.test.ts, so a match on both sides
// proves the engines agree without either invoking the other. A per-action rate
// divides by the root's steps only, and an unnamed producer inflates it by
// however many steps the login consumed.
func TestNextActionNamesTheGeneratorThatProducedIt(t *testing.T) {
	spec := `
import { Tap, actions, extract, taps } from "@sanderling/spec";
const signIn = extract("signIn", state => state.ax.find("id:SignIn"));
export const setup = actions(() => (signIn.current ? [Tap({ on: "id:SignIn" })] : []));
export const actionsRoot = taps;
`
	v := loadBundled(t, spec, enumTreeJSON)
	action, err := v.NextAction()
	if err != nil {
		t.Fatalf("NextAction: %v", err)
	}
	if action.On != "id:SignIn" || action.Source != "setup" {
		t.Errorf("NextAction = %+v, want the Tap on id:SignIn named setup", action)
	}

	pushTree(t, v, policyTreeJSON)
	action, err = v.NextAction()
	if err != nil {
		t.Fatalf("NextAction after setup went quiet: %v", err)
	}
	if action.Source != "seeded" {
		t.Errorf("NextAction = %+v, want the action root's tap named seeded", action)
	}
}

// TestSetupActionNamesSetup: the model policy reaches setup through its own
// entry, which must name the producer the same way the seeded entry does, or
// the two arms' per-action rates divide by different things.
func TestSetupActionNamesSetup(t *testing.T) {
	spec := `
import { Tap, actions, taps } from "@sanderling/spec";
export const setup = actions(() => [Tap({ on: "id:SignIn" })]);
export const actionsRoot = taps;
`
	v := loadBundled(t, spec, enumTreeJSON)
	action, err := v.SetupAction()
	if err != nil {
		t.Fatalf("SetupAction: %v", err)
	}
	if action.Source != "setup" {
		t.Errorf("SetupAction = %+v, want it named setup", action)
	}
}

// TestSetupGeneratorDrawsUnderTheModelPolicy: setup walks through the picker
// with its rng under both policies, so a generator there is not the divergence
// the enumeration refuses and must keep drawing. Enumerating before every setup
// step is what a model-driven run does, and the refusal must not leak out of it.
func TestSetupGeneratorDrawsUnderTheModelPolicy(t *testing.T) {
	spec := `
import { InputText, actions, integers, taps } from "@sanderling/spec";
const setupValues = integers().between(1, 500);
export const setup = actions(() => [InputText({ into: "id:Amount", text: String(setupValues.generate()) })]);
export const actionsRoot = taps;
`
	v := loadBundled(t, spec, policyTreeJSON)
	typed := map[string]bool{}
	for range 16 {
		if _, err := v.Candidates(LabelSourceVisibleText); err != nil {
			t.Fatalf("Candidates: %v", err)
		}
		action, err := v.SetupAction()
		if err != nil {
			t.Fatalf("SetupAction: %v", err)
		}
		typed[action.Text] = true
	}
	if len(typed) < 2 {
		t.Errorf("setup typed %v on every step; the picker's rng did not reach it", slices.Sorted(maps.Keys(typed)))
	}
}
