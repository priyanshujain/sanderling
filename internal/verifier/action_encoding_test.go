package verifier

import (
	"strings"
	"testing"
)

// The picker under test comes from the runtime entry, so the spec beside it
// only has to satisfy Load's demand for a properties global.
const propertiesOnlySpec = "globalThis.properties = {};\n"

// legacyRuntimeEntry stands in for the @sanderling/spec 0.0.3 runtime entry:
// it installs the picker and declares nothing, and its authored Scroll carries
// the container's own point as both endpoints. Paired with a binary that
// treats pre-computed endpoints as authoritative, every scroll it produces is
// a drag from a point to itself.
const legacyRuntimeEntry = `
const target = globalThis as Record<string, unknown>;
Object.defineProperty(target, "__sanderlingNextAction__", {
  value: () => ({
    kind: "Scroll",
    direction: "down",
    fromX: 540,
    fromY: 1200,
    toX: 540,
    toY: 1200,
    durationMillis: 250,
    selector: "id:ledger",
  }),
  writable: false,
  configurable: false,
  enumerable: false,
});
`

func TestLoad_RefusesABundleThatDeclaresNoActionEncoding(t *testing.T) {
	verifier := newVerifier(t)

	err := verifier.Load(bundleSpec(t, bundleOptions{
		SpecSource:    propertiesOnlySpec,
		RuntimeSource: legacyRuntimeEntry,
	}))
	if err == nil {
		t.Fatal("Load accepted a bundle that declares no action encoding; a spec " +
			"bundled by a package older than this binary dispatches every scroll " +
			"successfully and travels zero distance, and the run reports nothing")
	}
	message := err.Error()
	for _, want := range []string{ActionWireContract, "same commit"} {
		if !strings.Contains(message, want) {
			t.Errorf("Load error %q does not mention %q; the failure has to name "+
				"both encodings and what to do about it", message, want)
		}
	}
}

func TestLoad_RefusesABundleThatDeclaresADifferentActionEncoding(t *testing.T) {
	verifier := newVerifier(t)
	runtime := strings.Replace(legacyRuntimeEntry,
		`const target = globalThis as Record<string, unknown>;`,
		`const target = globalThis as Record<string, unknown>;
target.__sanderlingActionEncoding__ = "action-wire/1";`, 1)

	err := verifier.Load(bundleSpec(t, bundleOptions{
		SpecSource:    propertiesOnlySpec,
		RuntimeSource: runtime,
	}))
	if err == nil {
		t.Fatal("Load accepted a bundle built against a different action encoding")
	}
	if !strings.Contains(err.Error(), "action-wire/1") {
		t.Errorf("Load error %q does not name the encoding the spec was built "+
			"against", err.Error())
	}
}

// The declaration the shipped runtime entry makes and the one this binary
// decodes are one contract. When they drift apart every run refuses to start,
// so the drift has to be reported here rather than as a bundling failure in
// every other suite.
func TestLoad_ShippedRuntimeEntryDeclaresTheContractThisBinaryImplements(t *testing.T) {
	verifier := newVerifier(t)
	loadActionSpec(t, verifier, `
import { actions, Wait } from "@sanderling/spec";
globalThis.actions = actions(Wait({ durationMillis: 1 }));
globalThis.properties = {};
`)

	declared := verifier.runtime.GlobalObject().Get(actionEncodingGlobal)
	if declared == nil || declared.String() != ActionWireContract {
		t.Fatalf("pkg/spec/src/runtime-entry.ts declares %v, this binary implements %q",
			declared, ActionWireContract)
	}
}

// pickerFreeRuntimeEntry stands in for a bundle that registers properties and
// generates no actions: a raw-JS fixture, or the bundle-check tool, which
// bundles with no runtime entry at all.
const pickerFreeRuntimeEntry = `
const target = globalThis as Record<string, unknown>;
target.__sanderlingBundleCheck__ = true;
`

// A bundle with no picker has no encoding to disagree about, so demanding a
// declaration from it would refuse to load specs that were never going to
// dispatch anything. chrome.Driver.InstallBundle applies the same rule.
func TestLoad_AcceptsABundleThatInstallsNoPicker(t *testing.T) {
	verifier := newVerifier(t)

	bundle := bundleSpec(t, bundleOptions{
		SpecSource:    propertiesOnlySpec,
		RuntimeSource: pickerFreeRuntimeEntry,
	})
	if err := verifier.Load(bundle); err != nil {
		t.Fatalf("Load refused a bundle that generates no actions: %v", err)
	}
}
