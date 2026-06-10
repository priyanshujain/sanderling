package verifier

import (
	"testing"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// TestVerbAcceptsSwipeRequiresPositiveBounds locks the fix for the notification
// shade: a zero-bounds element centers at (0,0), and a downward swipe from the
// top-left corner is the system gesture that pulls the shade over the app. The
// swipe verb must reject zero-bounds nodes like every other verb does.
func TestVerbAcceptsSwipeRequiresPositiveBounds(t *testing.T) {
	zeroBounds := &hierarchy.Element{Bounds: hierarchy.Bounds{}}
	if verbAccepts("swipes", zeroBounds) {
		t.Error("swipes must reject a zero-bounds element (it centers at (0,0) and pulls the notification shade)")
	}

	realBounds := &hierarchy.Element{Bounds: hierarchy.Bounds{Left: 100, Top: 400, Right: 980, Bottom: 600}}
	if !verbAccepts("swipes", realBounds) {
		t.Error("swipes must accept an element with positive bounds")
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
