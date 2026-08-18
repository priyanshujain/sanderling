package verifier

import (
	"os"
	"strconv"
	"strings"
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
// actually lets the spec resolve selectors through state.ax.find. Reads a
// committed sidecar TreeNode JSON fixture so the round trip always runs.
func TestStateAxFindWorks(t *testing.T) {
	jsonText, err := os.ReadFile("testdata/ax_find_tree.json")
	if err != nil {
		t.Fatalf("committed fixture unreadable, so the round trip never ran: %v", err)
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

// A selector key that can never match is a spec bug, and an empty result hides
// it: the generator yields no action, the runner waits out the step, and the
// run ends clean having explored nothing. The spec must fail instead.
func TestStateAxObjectSelectorRejectsAnUnknownKey(t *testing.T) {
	tree, err := hierarchy.Parse(`{
		"attributes": {"resource-id": "root"},
		"children": [{"attributes": {"content-desc": "Supplier"}, "children": []}]
	}`)
	if err != nil {
		t.Fatal(err)
	}
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.probe = __sanderling__.extract(state => !!state.ax.find({ descripton: "Supplier" }));
	`)

	err = verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree})
	if err == nil {
		t.Fatal("expected an unknown selector key to fail the spec")
	}
	if !strings.Contains(err.Error(), "descripton") {
		t.Errorf("error does not name the offending key: %v", err)
	}
	if !strings.Contains(err.Error(), "accepted keys") {
		t.Errorf("error does not list the accepted keys: %v", err)
	}
}

// desc names the accessibility description in the element fields and in the
// string form, so the object form answers to it too rather than reporting it as
// a mistake.
func TestStateAxObjectSelectorAcceptsDesc(t *testing.T) {
	tree, err := hierarchy.Parse(`{
		"attributes": {"resource-id": "root"},
		"children": [{"attributes": {"content-desc": "Supplier"}, "children": []}]
	}`)
	if err != nil {
		t.Fatal(err)
	}
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.probe = __sanderling__.extract(state => state.ax.find({ desc: "Supplier" }) ? "matched" : "miss");
	`)
	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree}); err != nil {
		t.Fatal(err)
	}
	got := verifier.runtime.GlobalObject().Get("probe").ToObject(verifier.runtime).Get("current").String()
	if got != "matched" {
		t.Fatalf("probe = %q, want matched", got)
	}
}

// A key that belongs to another platform must stay silent: one spec runs on
// every platform, and iOS-only attributes are absent from an Android tree by
// design rather than by mistake.
func TestStateAxObjectSelectorKeepsCrossPlatformKeysSilent(t *testing.T) {
	tree, err := hierarchy.Parse(`{
		"attributes": {"resource-id": "root"},
		"children": []
	}`)
	if err != nil {
		t.Fatal(err)
	}
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.probe = __sanderling__.extract(state => state.ax.find({ title: "Settings" }) ? "matched" : "miss");
	`)
	if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, Tree: tree}); err != nil {
		t.Fatalf("a platform-specific key must not fail the run: %v", err)
	}
	got := verifier.runtime.GlobalObject().Get("probe").ToObject(verifier.runtime).Get("current").String()
	if got != "miss" {
		t.Fatalf("probe = %q, want miss", got)
	}
}

// axSelectorFormsTree carries one node per id shape a dump produces: the bare
// tag Compose and the web driver emit, the package-qualified resource id
// Android emits, and the iOS accessibility identifier.
const axSelectorFormsTree = `{
	"attributes": {"resource-id": "root", "bounds": "[0,0,400,800]"},
	"children": [
		{"attributes": {"resource-id": "BareThing", "text": "bare", "bounds": "[0,0,100,50]"},
		 "children": []},
		{"attributes": {"resource-id": "com.example.app:id/AndroidThing", "text": "android",
		 "bounds": "[0,50,100,100]"}, "children": []},
		{"attributes": {"accessibilityIdentifier": "IosThing", "text": "ios",
		 "bounds": "[0,100,100,150]"}, "children": []}
	]
}`

// TestStateAxSelectorFormsAgree drives both selector forms a spec can write
// through state.ax.find and holds them to the same element. The two forms
// dispatch to different lookups (findNodeFromJS sends a string to FindNode and
// an object to FindBySelector), and the object one used to skip the id rule
// that knows an Android resource id is package-qualified, so a spec that wrote
// ax.find({id: "AddAccountSubmit"}) got undefined on Android and every property
// reading it passed while checking nothing.
func TestStateAxSelectorFormsAgree(t *testing.T) {
	tree, err := hierarchy.Parse(axSelectorFormsTree)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		value string
		want  string
	}{
		{"BareThing", "bare"},
		{"AndroidThing", "android"},
		{"com.example.app:id/AndroidThing", "android"},
		{"IosThing", "ios"},
	} {
		t.Run(test.value, func(t *testing.T) {
			verifier := newVerifier(t)
			mustLoad(t, verifier, `
				globalThis.fromObject = __sanderling__.extract(
					state => state.ax.find({ id: `+strconv.Quote(test.value)+` })?.text, "fromObject");
				globalThis.fromString = __sanderling__.extract(
					state => state.ax.find("id:" + `+strconv.Quote(test.value)+`)?.text, "fromString");
				globalThis.properties = {};
			`)
			if err := verifier.PushSnapshot(SnapshotInput{Tree: tree}); err != nil {
				t.Fatal(err)
			}
			fromObject := readCurrent(t, verifier, "fromObject")
			fromString := readCurrent(t, verifier, "fromString")
			if fromString != test.want {
				t.Fatalf(`ax.find("id:%s") read %v, want %q`, test.value, fromString, test.want)
			}
			if fromObject != fromString {
				t.Errorf(
					`one selector, two answers: ax.find({id: %q}) read %v and ax.find("id:%s") read %v`,
					test.value, fromObject, test.value, fromString,
				)
			}
		})
	}
}

// readCurrent returns a named extractor's current value, or nil when the getter
// returned undefined, which is what an unresolved selector produces.
func readCurrent(t *testing.T, verifier *Verifier, name string) any {
	t.Helper()
	handle := verifier.runtime.GlobalObject().Get(name)
	if handle == nil {
		t.Fatalf("%s is not defined", name)
	}
	return handle.ToObject(verifier.runtime).Get("current").Export()
}
