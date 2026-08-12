package verifier

import (
	"slices"
	"testing"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// TestTargetsReportFactsWithoutFiltering pins the native host's half of the
// split introduced to stop the two hosts drifting: it reports what an element
// IS and never decides which verb may act on it. The verb decision is one shared
// rule (pkg/spec/src/targets.ts) both hosts consume, asserted across engines by
// TestHostsAgreeOnTargetEligibility.
func TestTargetsReportFactsWithoutFiltering(t *testing.T) {
	tree, err := hierarchy.Parse(hostParityTreeJSON)
	if err != nil {
		t.Fatal(err)
	}
	v := &Verifier{lastTree: tree}
	targets := v.targets()
	if len(targets) != len(hostParityScreen) {
		t.Fatalf("targets() returned %d elements, want all %d in the tree",
			len(targets), len(hostParityScreen))
	}
	for index, target := range targets {
		want := hostParityScreen[index]
		got := []bool{
			target.clickable, target.enabled, target.editable, target.scrollable,
		}
		expected := []bool{
			want.clickable, want.enabled, want.editable, want.scrollable,
		}
		if !slices.Equal(got, expected) {
			t.Errorf("%s clickable/enabled/editable/scrollable = %v, want %v",
				want.name, got, expected)
		}
		positiveBounds := target.width > 0 && target.height > 0
		if positiveBounds != want.positiveBounds {
			t.Errorf("%s positive bounds = %v, want %v",
				want.name, positiveBounds, want.positiveBounds)
		}
	}
}

// TestScopedElements is the core of keeping the fuzzer in the app. It checks
// the window-ownership rule against a realistic tree: the app window carries no
// package (Compose), even under android:id/content; the soft keyboard and system
// UI are separate windows with concrete packages, and their empty-package child
// wrappers (a keyboard "Settings" key) must inherit the foreign owner and drop
// out -- the exact node that used to leak in and navigate to system Settings.
func TestScopedElements(t *testing.T) {
	const treeJSON = `{
	  "attributes": {"bounds": "[0,0,1080,2400]"},
	  "children": [
	    {"attributes": {"resource-id": "LoginEmail", "bounds": "[0,100,1080,200]"}, "children": []},
	    {"attributes": {"resource-id": "android:id/content", "bounds": "[0,0,1080,2400]"}, "children": [
	      {"attributes": {"resource-id": "AccountNameField", "bounds": "[0,300,1080,400]"}, "children": []}
	    ]},
	    {"attributes": {"resource-id": "com.oplus.securitykeyboard:id/keyboard", "bounds": "[0,1503,1080,2268]"}, "children": [
	      {"attributes": {"content-desc": "Settings", "bounds": "[461,1503,618,1635]"}, "children": []}
	    ]},
	    {"attributes": {"resource-id": "com.android.systemui:id/nav", "bounds": "[0,2268,1080,2400]"}, "children": []}
	  ]
	}`
	tree, err := hierarchy.Parse(treeJSON)
	if err != nil {
		t.Fatal(err)
	}
	v := &Verifier{appPackage: "app.folio", lastTree: tree}
	scope := v.scopedElements()
	inScope := func(selector string) bool {
		element := tree.Find(selector)
		if element == nil {
			t.Fatalf("element %q not found in tree", selector)
		}
		return scope[element]
	}

	// App nodes carry no package and stay in scope, even under the android
	// framework content wrapper.
	for _, selector := range []string{"id:LoginEmail", "id:AccountNameField"} {
		if !inScope(selector) {
			t.Errorf("%s should be in scope (app window)", selector)
		}
	}
	// The keyboard's empty-package "Settings" key inherits the IME window owner
	// and drops out; the system UI node drops out by its own package.
	if inScope("desc:Settings") {
		t.Error("keyboard Settings key must be out of scope (owned by the IME window)")
	}
	if inScope("id:nav") {
		t.Error("system UI node must be out of scope")
	}
}
