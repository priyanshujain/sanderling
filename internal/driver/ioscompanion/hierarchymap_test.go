package ioscompanion

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

func mapAndParse(t *testing.T, dump []byte, width, height int) *hierarchy.Tree {
	t.Helper()
	treeJSON, err := MapHierarchy(dump, width, height)
	if err != nil {
		t.Fatalf("MapHierarchy: %v", err)
	}
	tree, err := hierarchy.Parse(string(treeJSON))
	if err != nil {
		t.Fatalf("hierarchy.Parse: %v", err)
	}
	return tree
}

func readDump(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

func TestGoldenLoginResolvesIdentifier(t *testing.T) {
	tree := mapAndParse(t, readDump(t, "login-describe.json"), 402, 874)

	element := tree.Find("id:LoginEmail")
	if element == nil {
		t.Fatal("id:LoginEmail did not resolve")
	}
	if element.ResourceID != "LoginEmail" {
		t.Fatalf("ResourceID = %q, want LoginEmail", element.ResourceID)
	}
	if element.Class != "TextArea" {
		t.Fatalf("Class = %q, want TextArea", element.Class)
	}
	// LoginEmail is an empty field, so its placeholder is the hintText.
	if element.Attributes["hintText"] != "Email" {
		t.Fatalf("hintText = %q, want Email", element.Attributes["hintText"])
	}
	if !element.Editable {
		t.Fatal("LoginEmail should be editable")
	}
	wantBounds := hierarchy.Bounds{Left: 34, Top: 125, Right: 368, Bottom: 173}
	if element.Bounds != wantBounds {
		t.Fatalf("Bounds = %+v, want %+v", element.Bounds, wantBounds)
	}
}

func TestGoldenScopedQueryUsesSpatialFallback(t *testing.T) {
	tree := mapAndParse(t, readDump(t, "accounts-describe.json"), 402, 874)

	// The Application node spans the full screen but the flat tree gives it no
	// descendants. A scoped query for content inside it must resolve through the
	// hierarchy package's spatial-containment fallback.
	container := tree.FindNode("class:Application")
	if container == nil {
		t.Fatal("class:Application did not resolve")
	}
	if len(container.Children) != 0 {
		t.Fatalf("expected a flat tree with no Application descendants, got %d", len(container.Children))
	}

	scoped := container.Find("desc:Accounts")
	if scoped == nil {
		t.Fatal("scoped desc:Accounts did not resolve via spatial fallback")
	}
	if scoped.Description != "Accounts" {
		t.Fatalf("scoped Description = %q, want Accounts", scoped.Description)
	}

	// Smallest-area ranking should put the compact label ahead of any larger
	// spatially-contained match.
	scopedButton := container.Find("id:AddAccountButton")
	if scopedButton == nil {
		t.Fatal("scoped id:AddAccountButton did not resolve via spatial fallback")
	}
	if scopedButton.ResourceID != "AddAccountButton" {
		t.Fatalf("scoped ResourceID = %q, want AddAccountButton", scopedButton.ResourceID)
	}
}

func parseSingle(t *testing.T, dump string) *hierarchy.Element {
	t.Helper()
	tree := mapAndParse(t, []byte(dump), 400, 800)
	if len(tree.Elements) < 2 {
		t.Fatalf("expected root plus one child, got %d elements", len(tree.Elements))
	}
	// Element 0 is the synthesized root; element 1 is the mapped child.
	return tree.Elements[1]
}

func TestPlaceholderMapsToHintText(t *testing.T) {
	dump := `[{"type":"TextField","frame":{"x":10,"y":20,"width":100,"height":40},
		"AXUniqueId":"Search","AXLabel":"Search accounts","AXValue":null,"enabled":true}]`
	element := parseSingle(t, dump)
	if element.Attributes["hintText"] != "Search accounts" {
		t.Fatalf("hintText = %q, want Search accounts", element.Attributes["hintText"])
	}
	if element.Attributes["accessibilityText"] != "" {
		t.Fatalf("accessibilityText = %q, want empty", element.Attributes["accessibilityText"])
	}
	if element.Text != "" {
		t.Fatalf("text = %q, want empty", element.Text)
	}
}

func TestNonEmptyValueMapsToText(t *testing.T) {
	dump := `[{"type":"TextField","frame":{"x":0,"y":0,"width":10,"height":10},
		"AXUniqueId":"Search","AXLabel":"Search accounts","AXValue":"groceries","enabled":true}]`
	element := parseSingle(t, dump)
	if element.Text != "groceries" {
		t.Fatalf("text = %q, want groceries", element.Text)
	}
	// With a value present, the label is a real accessibility label, not a hint.
	if element.Attributes["accessibilityText"] != "Search accounts" {
		t.Fatalf("accessibilityText = %q, want Search accounts", element.Attributes["accessibilityText"])
	}
	if element.Attributes["hintText"] != "" {
		t.Fatalf("hintText = %q, want empty", element.Attributes["hintText"])
	}
}

func TestEditableAndClickableFlags(t *testing.T) {
	cases := []struct {
		elementType   string
		wantEditable  bool
		wantClickable bool
	}{
		{"TextField", true, false},
		{"TextArea", true, false},
		{"Button", false, true},
		{"StaticText", false, false},
	}
	for _, testCase := range cases {
		dump := `[{"type":"` + testCase.elementType + `","frame":{"x":0,"y":0,"width":10,"height":10},"enabled":true}]`
		element := parseSingle(t, dump)
		if element.Editable != testCase.wantEditable {
			t.Errorf("%s editable = %v, want %v", testCase.elementType, element.Editable, testCase.wantEditable)
		}
		if element.Clickable != testCase.wantClickable {
			t.Errorf("%s clickable = %v, want %v", testCase.elementType, element.Clickable, testCase.wantClickable)
		}
	}
}

func TestEnabledMapsToTopLevelBool(t *testing.T) {
	dump := `[{"type":"Button","frame":{"x":0,"y":0,"width":10,"height":10},"enabled":false}]`
	element := parseSingle(t, dump)
	if element.Enabled {
		t.Fatal("element should be disabled")
	}
	if element.Attributes["enabled"] != "false" {
		t.Fatalf("enabled attr = %q, want false", element.Attributes["enabled"])
	}
}

func TestBoundsArithmetic(t *testing.T) {
	dump := `[{"type":"Button","frame":{"x":34,"y":125.33333,"width":334,"height":48.00001},"enabled":true}]`
	element := parseSingle(t, dump)
	want := hierarchy.Bounds{Left: 34, Top: 125, Right: 368, Bottom: 173}
	if element.Bounds != want {
		t.Fatalf("Bounds = %+v, want %+v", element.Bounds, want)
	}
}

func TestMalformedElementsSkipped(t *testing.T) {
	// First element has no type and must be skipped; second has a NaN frame and
	// must be skipped; third is valid.
	dump := `[
		{"frame":{"x":0,"y":0,"width":10,"height":10},"enabled":true},
		{"type":"Button","frame":{"x":0,"y":0,"width":1e999,"height":10},"enabled":true},
		{"type":"Button","frame":{"x":0,"y":0,"width":10,"height":10},"AXUniqueId":"OK","enabled":true}
	]`
	tree := mapAndParse(t, []byte(dump), 400, 800)
	// Root plus exactly one valid child.
	if len(tree.Elements) != 2 {
		t.Fatalf("expected 2 elements (root + 1 valid), got %d", len(tree.Elements))
	}
	if tree.Find("id:OK") == nil {
		t.Fatal("valid element OK should resolve")
	}
}

func TestEmptyAndGarbageInputAreTotal(t *testing.T) {
	for _, dump := range [][]byte{nil, {}, []byte("not json"), []byte("{}"), []byte("[]")} {
		treeJSON, err := MapHierarchy(dump, 100, 200)
		if err != nil {
			t.Fatalf("MapHierarchy(%q) error: %v", dump, err)
		}
		var node treeNode
		if err := json.Unmarshal(treeJSON, &node); err != nil {
			t.Fatalf("result not valid TreeNode JSON for %q: %v", dump, err)
		}
		if node.Attributes["bounds"] != "[0,0][100,200]" {
			t.Fatalf("root bounds = %q, want [0,0][100,200]", node.Attributes["bounds"])
		}
	}
}

func TestRootIsFlatWithAllChildren(t *testing.T) {
	tree := mapAndParse(t, readDump(t, "login-describe.json"), 402, 874)
	if tree.Root == nil {
		t.Fatal("nil root")
	}
	// The login dump has 10 elements; all map to direct children of the root.
	if len(tree.Root.Children) != 10 {
		t.Fatalf("root children = %d, want 10", len(tree.Root.Children))
	}
	wantRoot := hierarchy.Bounds{Left: 0, Top: 0, Right: 402, Bottom: 874}
	if tree.Root.Bounds != wantRoot {
		t.Fatalf("root bounds = %+v, want %+v", tree.Root.Bounds, wantRoot)
	}
}

func TestHasUnresolvedValues(t *testing.T) {
	cases := []struct {
		name string
		dump string
		want bool
	}{
		{name: "sentinel value", dump: `[{"type":"TextField","AXValue":"Invalid","frame":{"x":0,"y":0,"width":1,"height":1}}]`, want: true},
		{name: "clean values", dump: `[{"type":"TextField","AXValue":"hello","frame":{"x":0,"y":0,"width":1,"height":1}}]`, want: false},
		{name: "empty value", dump: `[{"type":"TextField","AXValue":"","frame":{"x":0,"y":0,"width":1,"height":1}}]`, want: false},
		{name: "empty dump", dump: `[]`, want: false},
		{name: "malformed dump", dump: `nope`, want: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasUnresolvedValues([]byte(c.dump)); got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestMapHierarchySentinelValueMapsAsEmpty(t *testing.T) {
	dump := `[{"type":"TextField","AXUniqueId":"F","AXLabel":"Email","AXValue":"Invalid","frame":{"x":0,"y":0,"width":10,"height":10},"enabled":true}]`
	mapped, err := MapHierarchy([]byte(dump), 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mapped), "Invalid") {
		t.Fatalf("sentinel leaked into mapped tree: %s", mapped)
	}
	if !strings.Contains(string(mapped), "hintText") {
		t.Fatalf("empty editable field should map label to hintText: %s", mapped)
	}
}
