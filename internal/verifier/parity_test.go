package verifier

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestCrossRuntimeParity is the W2 acceptance gate: for a FIXED seed and a FIXED
// candidate state, the goja-bundled picker must emit the SAME action stream as
// the node/web pick.ts. Both engines run the SHARED pick.ts over the SHARED Pcg,
// so each asserts the SAME committed golden (pkg/spec/test/fixtures/
// parity-golden.json); the node side does so in pkg/spec/test/parity.test.ts.
// Matching one golden on both sides proves they match each other without either
// invoking the other.
//
// The candidate ORDER and the per-tick PCG draw order are the parity contract.
// A stub __sanderlingHost__ feeds the SAME three candidates (in the SAME order)
// for every verb, so the hierarchy filter is out of the picture and the only
// variable left is the JS engine. The weighted root mixes a tap branch with a
// typing branch, exercising weighted selection, a builtin, and the input corpus
// within the 20-tick window; reordering candidates or adding/dropping a draw on
// either side shifts the stream and fails the golden.
func TestCrossRuntimeParity(t *testing.T) {
	const seed uint64 = 0x9e3779b97f4a7c15
	golden := loadParityGolden(t)

	verifier := newVerifier(t, WithSeed(seed))
	installStubHost(t, verifier)
	loadActionSpec(t, verifier, `
		import { taps, typing, weighted } from "@sanderling/spec";
		globalThis.actions = weighted([3, taps], [1, typing]);
	`)

	if len(golden) == 0 {
		t.Fatal("golden stream is empty")
	}
	for step, want := range golden {
		got, err := verifier.NextAction()
		if err != nil {
			t.Fatalf("goja next action at step %d: %v", step, err)
		}
		if got != want {
			t.Fatalf("step %d diverged from golden:\n goja=%+v\n want=%+v", step, got, want)
		}
	}
}

// installStubHost replaces the verifier's hierarchy-backed __sanderlingHost__
// with one returning a FIXED target list, keeping the seed the verifier was
// constructed with. Every fact is set so the shared eligibility rule admits all
// three targets for every verb, leaving the draw order as the only variable. It
// must run BEFORE Load, because the bundled goja runtime entry captures the host
// when the spec evaluates.
func installStubHost(t *testing.T, verifier *Verifier) {
	t.Helper()
	const stub = `
		const facts = { clickable: true, enabled: true, editable: true, scrollable: true };
		const targets = [
			{ x: 50, y: 60, selector: "id:alpha", width: 100, height: 40, ...facts },
			{ x: 150, y: 160, selector: "id:beta", width: 120, height: 48, ...facts },
			{ x: 250, y: 260, selector: "id:gamma", width: 80, height: 32, ...facts },
		];
		const seedHi = globalThis.__sanderlingHost__.seedHi;
		const seedLo = globalThis.__sanderlingHost__.seedLo;
		globalThis.__sanderlingHost__ = {
			platform: () => "android",
			queryTargets: () => targets,
			reportUnsupported: () => {},
			seedHi,
			seedLo,
		};
	`
	if _, err := verifier.runtime.RunString(stub); err != nil {
		t.Fatalf("install stub host: %v", err)
	}
}

// loadParityGolden decodes the shared golden stream with the SAME DecodeAction
// the runner uses, so the goja comparison is apples-to-apples with the wire the
// node picker emits.
func loadParityGolden(t *testing.T) []Action {
	t.Helper()
	path, err := filepath.Abs("../../pkg/spec/test/fixtures/parity-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	stream := make([]Action, len(raw))
	for i, message := range raw {
		action, err := DecodeAction(message)
		if err != nil {
			t.Fatalf("decode golden action %d: %v", i, err)
		}
		stream[i] = action
	}
	return stream
}
