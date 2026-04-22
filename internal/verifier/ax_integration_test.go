package verifier

import (
	"os"
	"testing"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// TestStateAxFindWorks verifies that a Parse+PushSnapshot+extract round trip
// actually lets the spec resolve selectors through state.ax.find.
// Reads /tmp/live-dump.json (Maestro TreeNode JSON format); skipped if absent.
func TestStateAxFindWorks(t *testing.T) {
	jsonText, err := os.ReadFile("/tmp/live-dump.json")
	if err != nil {
		t.Skip("live-dump.json not present")
	}
	tree, err := hierarchy.Parse(string(jsonText))
	if err != nil {
		t.Fatal(err)
	}
	if tree.Find("id:select_language") == nil {
		t.Fatal("Go-side parser should find id:select_language")
	}

	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.probe = __sanderling__.extract(state => {
			const element = state.ax.find("id:select_language");
			return element ? "matched:" + element.text : "miss";
		});
		globalThis.count = __sanderling__.extract(state => state.ax.findAll("id:select_language").length);
	`)

	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree}); err != nil {
		t.Fatal(err)
	}

	probe := verifier.runtime.GlobalObject().Get("probe").ToObject(verifier.runtime).Get("current").String()
	if probe == "miss" {
		t.Fatalf("state.ax.find returned undefined; got %q", probe)
	}
	t.Logf("probe returned %q", probe)

	count := verifier.runtime.GlobalObject().Get("count").ToObject(verifier.runtime).Get("current").ToInteger()
	if count != 1 {
		t.Fatalf("findAll count = %d, want 1", count)
	}
}
