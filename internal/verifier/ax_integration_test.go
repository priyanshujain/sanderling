package verifier

import (
	"os"
	"testing"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// TestStateAxObjectSelectorTestTagAlias verifies that an object selector
// `{ testTag: "X" }` resolves through the testTag alias to match an element
// whose source attributes carry resource-id="X" (the Compose
// testTagsAsResourceId=true case on Android).
func TestStateAxObjectSelectorTestTagAlias(t *testing.T) {
	src := `{
		"attributes": {"class": "android.widget.LinearLayout"},
		"children": [
			{
				"attributes": {"resource-id": "LoginScreen", "class": "android.view.View"},
				"children": [
					{
						"attributes": {"resource-id": "LoginEmail", "class": "android.widget.EditText"},
						"children": []
					}
				]
			}
		]
	}`
	tree, err := hierarchy.Parse(src)
	if err != nil {
		t.Fatal(err)
	}

	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.loginRoot = __sanderling__.extract(state => {
			const r = state.ax.find({ testTag: "LoginScreen" });
			return r ? "matched" : "miss";
		});
		globalThis.loginEmailViaChain = __sanderling__.extract(state => {
			const r = state.ax.find({ testTag: "LoginScreen" });
			if (!r) return "outer-miss";
			const inner = r.find({ testTag: "LoginEmail" });
			return inner ? "inner-matched" : "inner-miss";
		});
	`)

	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree}); err != nil {
		t.Fatal(err)
	}

	root := verifier.runtime.GlobalObject().Get("loginRoot").ToObject(verifier.runtime).Get("current").String()
	if root != "matched" {
		t.Fatalf("loginRoot = %q, want matched", root)
	}
	chain := verifier.runtime.GlobalObject().Get("loginEmailViaChain").ToObject(verifier.runtime).Get("current").String()
	if chain != "inner-matched" {
		t.Fatalf("loginEmailViaChain = %q, want inner-matched", chain)
	}
}

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
