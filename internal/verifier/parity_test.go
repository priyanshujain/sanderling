package verifier

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// TestCrossRuntimeParity is the W2 acceptance gate: given the SAME seed and the
// SAME candidate state, the goja verifier and the node/web picker must emit a
// BYTE-IDENTICAL action stream. Both engines run the SHARED pick.ts over the
// SHARED Pcg, so this test fails the moment one runtime's JS number/bigint
// behavior diverges from the other's.
//
// The goja side drives the real verifier.NextAction over a fixed hierarchy. The
// candidate list that hierarchy yields is then handed verbatim to the node
// harness so both engines index the same targets; the only variable left is the
// JS engine.
func TestCrossRuntimeParity(t *testing.T) {
	const seed uint64 = 0x9e3779b97f4a7c15
	const steps = 32

	const treeJSON = `{
	  "attributes": {"resource-id": "root", "bounds": "[0,0,400,800]"},
	  "children": [
	    {"attributes": {"resource-id": "alpha", "bounds": "[0,0,400,100]"}, "clickable": true, "enabled": true, "children": []},
	    {"attributes": {"resource-id": "beta", "bounds": "[0,100,400,200]"}, "clickable": true, "enabled": true, "children": []},
	    {"attributes": {"resource-id": "gamma", "bounds": "[0,200,400,300]"}, "clickable": true, "enabled": true, "children": []},
	    {"attributes": {"resource-id": "delta", "bounds": "[0,300,400,400]"}, "clickable": true, "enabled": true, "children": []},
	    {"attributes": {"resource-id": "epsilon", "bounds": "[0,400,400,500]"}, "clickable": true, "enabled": true, "children": []}
	  ]
	}`
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatalf("parse tree: %v", err)
	}

	verifier := newVerifier(t, WithSeed(seed))
	loadActionSpec(t, verifier, `
		import { taps } from "@sanderling/spec";
		globalThis.actions = taps;
	`)
	if err := verifier.PushSnapshot(SnapshotInput{Tree: tree}); err != nil {
		t.Fatalf("push snapshot: %v", err)
	}

	gojaStream := make([]Action, 0, steps)
	for range steps {
		action, err := verifier.NextAction()
		if err != nil {
			t.Fatalf("goja next action: %v", err)
		}
		gojaStream = append(gojaStream, action)
	}

	// Hand node the exact candidate list goja drew from, so both engines index
	// the same targets and only the JS engine differs.
	nodeStream := runNodeParity(t, seed, steps, verifier.candidatesForVerb("taps"))
	if len(nodeStream) != len(gojaStream) {
		t.Fatalf("stream length: goja=%d node=%d", len(gojaStream), len(nodeStream))
	}
	for i := range gojaStream {
		if gojaStream[i] != nodeStream[i] {
			t.Fatalf("step %d diverged:\n goja=%+v\n node=%+v", i, gojaStream[i], nodeStream[i])
		}
	}
}

// runNodeParity runs the shared picker under node (tsx) with the given seed and
// candidate list, decoding its serialized stream with the SAME DecodeAction the
// runner uses so the comparison is apples-to-apples.
func runNodeParity(t *testing.T, seed uint64, steps int, candidates []candidate) []Action {
	t.Helper()
	wire := make([]map[string]any, len(candidates))
	for i, candidate := range candidates {
		wire[i] = map[string]any{
			"x": candidate.x, "y": candidate.y,
			"width": candidate.width, "height": candidate.height,
			"selector": candidate.selector,
		}
	}
	candidatesJSON, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	specDir, err := filepath.Abs("../../pkg/spec")
	if err != nil {
		t.Fatal(err)
	}

	command := exec.Command("node", "--import", "tsx", "test/parity-harness.ts")
	command.Dir = specDir
	command.Env = append(command.Environ(),
		"SANDERLING_PARITY_SEED="+strconv.FormatUint(seed, 10),
		"SANDERLING_PARITY_STEPS="+strconv.Itoa(steps),
		"SANDERLING_PARITY_CANDIDATES="+string(candidatesJSON),
	)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("node parity harness: %v\noutput: %s", err, output)
	}

	var raw []json.RawMessage
	if err := json.Unmarshal(output, &raw); err != nil {
		t.Fatalf("decode node output %q: %v", output, err)
	}
	stream := make([]Action, len(raw))
	for i, message := range raw {
		action, err := DecodeAction(message)
		if err != nil {
			t.Fatalf("decode node action %d: %v", i, err)
		}
		stream[i] = action
	}
	return stream
}
