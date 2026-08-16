package hierarchy

import (
	"strings"
	"testing"
)

// sampleDump is a sidecar TreeNode JSON equivalent of the old XML fixture.
const sampleDump = `{
  "attributes": {"class": "android.widget.LinearLayout", "package": "app", "bounds": "[0,0,1080,2340]"},
  "children": [
    {
      "attributes": {"resource-id": "app:id/title", "text": "Hello", "bounds": "[10,20,200,60]"},
      "children": [],
      "clickable": false,
      "enabled": true
    },
    {
      "attributes": {"resource-id": "app:id/row", "text": "Alice", "content-desc": "row", "bounds": "[0,100,1080,200]"},
      "children": [],
      "clickable": true,
      "enabled": true
    },
    {
      "attributes": {"resource-id": "app:id/row", "text": "Bob", "content-desc": "row", "bounds": "[0,200,1080,300]"},
      "children": [],
      "clickable": true,
      "enabled": true
    }
  ]
}`

func TestParseCountsNodes(t *testing.T) {
	tree, err := Parse(sampleDump)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(tree.Elements) != 4 {
		t.Fatalf("want 4 elements, got %d", len(tree.Elements))
	}
}

func TestFindByIDSuffix(t *testing.T) {
	tree, _ := Parse(sampleDump)
	element := tree.Find("id:title")
	if element == nil {
		t.Fatal("expected match for id:title")
	}
	if element.Text != "Hello" {
		t.Fatalf("unexpected text %q", element.Text)
	}
}

func TestFindByText(t *testing.T) {
	tree, _ := Parse(sampleDump)
	element := tree.Find("text:Alice")
	if element == nil {
		t.Fatal("expected match for text:Alice")
	}
}

func TestFindAllReturnsDuplicates(t *testing.T) {
	tree, _ := Parse(sampleDump)
	elements := tree.FindAll("id:row")
	if len(elements) != 2 {
		t.Fatalf("want 2, got %d", len(elements))
	}
}

func TestBoundsCenter(t *testing.T) {
	tree, _ := Parse(sampleDump)
	element := tree.Find("text:Alice")
	x, y := element.Bounds.Center()
	if x != 540 || y != 150 {
		t.Fatalf("unexpected center %d,%d", x, y)
	}
}

func TestParseEmpty(t *testing.T) {
	tree, err := Parse("")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(tree.Elements) != 0 {
		t.Fatalf("want empty, got %d", len(tree.Elements))
	}
}

func TestUnknownSelector(t *testing.T) {
	tree, _ := Parse(sampleDump)
	if tree.Find("bogus:value") != nil {
		t.Fatal("unknown kind should not match")
	}
}

func TestDescPrefix(t *testing.T) {
	input := `{
	  "attributes": {},
	  "children": [
	    {"attributes": {"content-desc": "customer_row_abc-123", "bounds": "[0,0,100,100]"}, "children": []},
	    {"attributes": {"content-desc": "customer_row_def-456", "bounds": "[0,100,100,200]"}, "children": []},
	    {"attributes": {"content-desc": "supplier_row_xyz", "bounds": "[0,200,100,300]"}, "children": []}
	  ]
	}`
	tree, _ := Parse(input)
	rows := tree.FindAll("descPrefix:customer_row_")
	if len(rows) != 2 {
		t.Fatalf("want 2 customer rows, got %d", len(rows))
	}
}

// idPrefixDump is a list whose rows carry a durable role prefix followed by the
// record's identifier, the convention that makes every row's full id unique and
// unwritable in a spec.
const idPrefixDump = `{
  "attributes": {"resource-id": "com.example:id/customer_list", "bounds": "[0,0,100,400]"},
  "children": [
    {"attributes": {"resource-id": "com.example:id/customer_row_abc-123", "bounds": "[0,0,100,100]"}, "children": []},
    {"attributes": {"resource-id": "com.example:id/customer_row_def-456", "bounds": "[0,100,100,200]"}, "children": []},
    {"attributes": {"resource-id": "com.example:id/supplier_row_xyz", "bounds": "[0,200,100,300]"}, "children": []},
    {"attributes": {"identifier": "customer_row_ghi-789", "bounds": "[0,300,100,400]"}, "children": []}
  ]
}`

func TestIDPrefixMatchesEveryRowSharingTheRole(t *testing.T) {
	tree, _ := Parse(idPrefixDump)
	rows := tree.FindAll("idPrefix:customer_row_")
	if len(rows) != 3 {
		t.Fatalf("want 3 customer rows, got %d", len(rows))
	}
}

func TestIDPrefixDoesNotRequireThePackagePrefix(t *testing.T) {
	tree, _ := Parse(idPrefixDump)
	el := tree.Find("idPrefix:customer_row_abc")
	if el == nil {
		t.Fatal("expected the local name after :id/ to match on its own")
	}
	if el.ResourceID != "com.example:id/customer_row_abc-123" {
		t.Fatalf("got %q", el.ResourceID)
	}
}

func TestIDPrefixAlsoMatchesTheWholeIdentifier(t *testing.T) {
	tree, _ := Parse(idPrefixDump)
	if tree.Find("idPrefix:com.example:id/customer_row_") == nil {
		t.Fatal("expected a package-qualified prefix to match")
	}
}

func TestIDPrefixMatchesIOSAccessibilityIdentifier(t *testing.T) {
	tree, _ := Parse(idPrefixDump)
	el := tree.Find("idPrefix:customer_row_ghi")
	if el == nil {
		t.Fatal("expected identifier to match on a node with no resource-id")
	}
	if el.Bounds.Top != 300 {
		t.Fatalf("matched the wrong node: %+v", el.Bounds)
	}
}

func TestIDPrefixMatchesNothingWhenNoIDStartsWithIt(t *testing.T) {
	tree, _ := Parse(idPrefixDump)
	if rows := tree.FindAll("idPrefix:invoice_row_"); len(rows) != 0 {
		t.Fatalf("want no matches, got %d", len(rows))
	}
}

func TestIDPrefixIsNotASubstringMatch(t *testing.T) {
	tree, _ := Parse(idPrefixDump)
	if tree.Find("idPrefix:row_") != nil {
		t.Fatal("expected starts-with, not substring")
	}
}

// The string and object forms are one rule, so a prefix filter combined with a
// second attribute has to keep the same meaning it has on its own.
func TestIDPrefixInObjectSelector(t *testing.T) {
	tree, _ := Parse(idPrefixDump)
	sel := Selector{Filters: []AttrFilter{{Attr: "idPrefix", Value: "customer_row_"}}}
	if nodes := tree.Root.FindAllBySelector(sel); len(nodes) != 3 {
		t.Fatalf("want 3 customer rows, got %d", len(nodes))
	}
}

func TestDescPrefixInObjectSelector(t *testing.T) {
	input := `{
	  "attributes": {},
	  "children": [
	    {"attributes": {"content-desc": "customer_row_abc-123", "bounds": "[0,0,100,100]"}, "children": []},
	    {"attributes": {"content-desc": "supplier_row_xyz", "bounds": "[0,100,100,200]"}, "children": []}
	  ]
	}`
	tree, _ := Parse(input)
	sel := Selector{Filters: []AttrFilter{{Attr: "descPrefix", Value: "customer_row_"}}}
	if nodes := tree.Root.FindAllBySelector(sel); len(nodes) != 1 {
		t.Fatalf("want 1 customer row, got %d", len(nodes))
	}
}

func TestBoolFieldsFromNode(t *testing.T) {
	input := `{
	  "attributes": {"resource-id": "x", "bounds": "[0,0,100,100]"},
	  "children": [],
	  "clickable": true,
	  "enabled": false,
	  "focused": true,
	  "checked": true,
	  "selected": false
	}`
	tree, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(tree.Elements) != 1 {
		t.Fatalf("want 1 element, got %d", len(tree.Elements))
	}
	el := tree.Elements[0]
	if !el.Clickable {
		t.Error("expected clickable=true")
	}
	if el.Enabled {
		t.Error("expected enabled=false")
	}
	if !el.Focused {
		t.Error("expected focused=true")
	}
	if !el.Checked {
		t.Error("expected checked=true")
	}
	if el.Selected {
		t.Error("expected selected=false")
	}
}

func TestEditableDerivation(t *testing.T) {
	cases := []struct {
		name string
		node string
		want bool
	}{
		{"driver flag", `{"attributes": {"bounds": "[0,0,10,10]"}, "editable": true}`, true},
		{"driver flag false", `{"attributes": {"bounds": "[0,0,10,10]"}, "editable": false}`, false},
		{"native EditText class", `{"attributes": {"class": "android.widget.EditText", "bounds": "[0,0,10,10]"}}`, true},
		{"hintText attr", `{"attributes": {"hintText": "Enter amount", "bounds": "[0,0,10,10]"}}`, true},
		{"plain button", `{"attributes": {"class": "android.widget.Button", "bounds": "[0,0,10,10]"}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree, err := Parse(tc.node)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			el := tree.Elements[0]
			if el.Editable != tc.want {
				t.Errorf("Editable = %v, want %v", el.Editable, tc.want)
			}
			if got := el.Attributes["editable"]; got != boolString(tc.want) {
				t.Errorf("attrs[editable] = %q, want %q", got, boolString(tc.want))
			}
		})
	}
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// TestFindByEditableSelector confirms find({editable:true}) matches via the
// mirrored attrs entry, the same path clickable uses.
func TestFindByEditableSelector(t *testing.T) {
	input := `{
	  "attributes": {"resource-id": "root", "bounds": "[0,0,100,100]"},
	  "children": [
	    {"attributes": {"class": "android.widget.EditText", "bounds": "[0,0,100,40]"}, "children": []},
	    {"attributes": {"class": "android.widget.Button", "bounds": "[0,40,100,80]"}, "children": []}
	  ]
	}`
	tree, _ := Parse(input)
	if el := tree.Find("editable:true"); el == nil {
		t.Fatal("expected to find the EditText via editable:true")
	}
	if matches := tree.FindAll("editable:true"); len(matches) != 1 {
		t.Fatalf("editable:true matched %d elements, want 1", len(matches))
	}
}

func TestIdentifierFallback(t *testing.T) {
	input := `{
	  "attributes": {"identifier": "my-button", "bounds": "[0,0,100,100]"},
	  "children": []
	}`
	tree, _ := Parse(input)
	el := tree.Find("id:my-button")
	if el == nil {
		t.Fatal("expected match via identifier fallback")
	}
}

func TestAccessibilityTextFallback(t *testing.T) {
	input := `{
	  "attributes": {"accessibilityText": "Close dialog", "bounds": "[0,0,50,50]"},
	  "children": []
	}`
	tree, _ := Parse(input)
	el := tree.Find("desc:Close dialog")
	if el == nil {
		t.Fatal("expected match via accessibilityText fallback")
	}
}

func TestIOSMergedLabel(t *testing.T) {
	// iOS merges contentDescription with child text: "add_account_button, + Add account"
	input := `{
	  "attributes": {"accessibilityText": "add_account_button, + Add account", "bounds": "[20,777][382,825]"},
	  "children": []
	}`
	tree, _ := Parse(input)
	el := tree.Find("desc:add_account_button")
	if el == nil {
		t.Fatal("expected desc: to match iOS merged label")
	}
	if el.Bounds.Left != 20 || el.Bounds.Top != 777 || el.Bounds.Right != 382 || el.Bounds.Bottom != 825 {
		t.Errorf("unexpected bounds: %+v", el.Bounds)
	}
}

const pathDump = `{
  "attributes": {"resource-id": "root", "bounds": "[0,0,1080,2340]"},
  "children": [
    {
      "attributes": {"resource-id": "A", "content-desc": "screen_a", "bounds": "[0,0,540,2340]"},
      "children": [
        {
          "attributes": {"resource-id": "B", "content-desc": "label_b", "bounds": "[0,0,100,100]"},
          "children": [
            {
              "attributes": {"resource-id": "C", "content-desc": "label_c", "bounds": "[0,0,50,50]"},
              "children": []
            }
          ]
        }
      ]
    },
    {
      "attributes": {"resource-id": "A2", "content-desc": "screen_a2", "bounds": "[540,0,1080,2340]"},
      "children": [
        {
          "attributes": {"resource-id": "B2", "content-desc": "label_b", "bounds": "[540,0,640,100]"},
          "children": []
        }
      ]
    }
  ]
}`

func TestPathQuerySingleLevel(t *testing.T) {
	tree, _ := Parse(pathDump)
	el := tree.Find("id:A > id:B")
	if el == nil {
		t.Fatal("expected to find B under A")
	}
	if el.ResourceID != "B" {
		t.Fatalf("got %q, want B", el.ResourceID)
	}
}

func TestPathQueryNotBUnderOtherRoot(t *testing.T) {
	tree, _ := Parse(pathDump)
	// B2 is under A2, not A; path from A should not reach B2
	el := tree.Find("id:A > id:B2")
	if el != nil {
		t.Fatalf("expected nil, got element with id %q", el.ResourceID)
	}
}

func TestPathQueryMultiLevel(t *testing.T) {
	tree, _ := Parse(pathDump)
	el := tree.Find("id:A > id:B > id:C")
	if el == nil {
		t.Fatal("expected to find C under A > B")
	}
	if el.ResourceID != "C" {
		t.Fatalf("got %q, want C", el.ResourceID)
	}
}

func TestPathQueryMixedTypes(t *testing.T) {
	tree, _ := Parse(pathDump)
	el := tree.Find("desc:screen_a > desc:label_b")
	if el == nil {
		t.Fatal("expected to find label_b under screen_a")
	}
	if el.Description != "label_b" {
		t.Fatalf("got %q, want label_b", el.Description)
	}
}

func TestPathQueryNotFound(t *testing.T) {
	tree, _ := Parse(pathDump)
	if tree.Find("id:A > id:NoSuch") != nil {
		t.Fatal("expected nil for missing second segment")
	}
}

func TestPathQueryFirstMatchesSecondDoesNot(t *testing.T) {
	tree, _ := Parse(pathDump)
	// B2 is under A2, so A > B2 should return nil (B2 is not a descendant of A)
	if tree.Find("id:A > id:B2") != nil {
		t.Fatal("B2 is not a descendant of A, expected nil")
	}
}

func TestPathQueryFindAllAcrossRoots(t *testing.T) {
	tree, _ := Parse(pathDump)
	// label_b appears under A (as B) and under A2 (as B2)
	// FindAll("desc:screen_a > desc:label_b") should only find B under A, not B2 under A2
	matches := tree.FindAll("desc:screen_a > desc:label_b")
	if len(matches) != 1 {
		t.Fatalf("want 1 match, got %d", len(matches))
	}
	if matches[0].ResourceID != "B" {
		t.Fatalf("got %q, want B", matches[0].ResourceID)
	}
}

func TestPathQueryFindAllMultipleRootMatches(t *testing.T) {
	tree, _ := Parse(pathDump)
	// Both A and A2 are children of root, both have a child with content-desc "label_b"
	matches := tree.FindAll("id:root > desc:label_b")
	if len(matches) != 2 {
		t.Fatalf("want 2 matches (B and B2), got %d", len(matches))
	}
}

func TestIOSBoundsFormat(t *testing.T) {
	input := `{
	  "attributes": {"accessibilityText": "account_card:abc123, Tim, $100", "bounds": "[20,130][382,202]"},
	  "children": []
	}`
	tree, _ := Parse(input)
	el := tree.Find("descPrefix:account_card:")
	if el == nil {
		t.Fatal("expected descPrefix to match iOS account card")
	}
	if el.Bounds.Left != 20 || el.Bounds.Top != 130 || el.Bounds.Right != 382 || el.Bounds.Bottom != 202 {
		t.Errorf("unexpected bounds: %+v", el.Bounds)
	}
	cx, cy := el.Bounds.Center()
	if cx != 201 || cy != 166 {
		t.Errorf("unexpected center: (%d, %d)", cx, cy)
	}
}

// --- full-attribute selector tests ---

const androidAttrDump = `{
  "attributes": {"resource-id": "com.app:id/list", "bounds": "[0,0,1080,2340]"},
  "children": [
    {
      "attributes": {"resource-id": "com.app:id/row1", "scrollable": "true", "bounds": "[0,0,1080,200]"},
      "children": [],
      "clickable": true,
      "enabled": true
    },
    {
      "attributes": {"resource-id": "com.app:id/row2", "scrollable": "false", "bounds": "[0,200,1080,400]"},
      "children": [],
      "clickable": false,
      "enabled": true
    }
  ]
}`

const iosAttrDump = `{
  "attributes": {"bounds": "[0,0,390,844]"},
  "children": [
    {
      "attributes": {"accessibilityText": "Close", "title": "Settings", "bounds": "[0,0,100,50]"},
      "children": [],
      "enabled": true
    },
    {
      "attributes": {"identifier": "Feed", "scrollable": "true", "bounds": "[0,120,390,700]"},
      "children": [],
      "enabled": true
    }
  ]
}`

func TestRawResourceIDSubstringMatch(t *testing.T) {
	tree, _ := Parse(androidAttrDump)
	el := tree.Find("resource-id:row1")
	if el == nil {
		t.Fatal("expected resource-id: to match via substring")
	}
}

func TestLabelAliasMatchesAccessibilityText(t *testing.T) {
	tree, _ := Parse(iosAttrDump)
	el := tree.Find("label:Close")
	if el == nil {
		t.Fatal("expected label: to match accessibilityText via alias")
	}
}

func TestContentDescAliasOnIOS(t *testing.T) {
	tree, _ := Parse(iosAttrDump)
	el := tree.Find("content-desc:Close")
	if el == nil {
		t.Fatal("expected content-desc: to match accessibilityText via alias on iOS")
	}
}

func TestScrollableTrueMatches(t *testing.T) {
	tree, _ := Parse(androidAttrDump)
	el := tree.Find("scrollable:true")
	if el == nil {
		t.Fatal("expected scrollable:true to match")
	}
	if el.ResourceID != "com.app:id/row1" {
		t.Fatalf("got %q, want row1", el.ResourceID)
	}
}

func TestScrollableFalseMatchesSecondRow(t *testing.T) {
	tree, _ := Parse(androidAttrDump)
	el := tree.Find("scrollable:false")
	if el == nil {
		t.Fatal("expected scrollable:false to match row2")
	}
	if el.ResourceID != "com.app:id/row2" {
		t.Fatalf("got %q, want row2", el.ResourceID)
	}
}

func TestTitleMatchesIOSElement(t *testing.T) {
	tree, _ := Parse(iosAttrDump)
	el := tree.Find("title:Settings")
	if el == nil {
		t.Fatal("expected title:Settings to match iOS element")
	}
}

func TestTitleReturnsNilForAndroid(t *testing.T) {
	tree, _ := Parse(androidAttrDump)
	el := tree.Find("title:Settings")
	if el != nil {
		t.Fatal("expected title:Settings to return nil for Android element (graceful ignore)")
	}
}

func TestScrollableMatchesOnIOS(t *testing.T) {
	tree, _ := Parse(iosAttrDump)
	el := tree.Find("scrollable:true")
	if el == nil {
		t.Fatal("expected scrollable:true to match the iOS scroll container")
	}
	if el.Attributes["identifier"] != "Feed" {
		t.Fatalf("got %q, want Feed", el.Attributes["identifier"])
	}
}

func TestTextIsNowSubstring(t *testing.T) {
	tree, _ := Parse(sampleDump)
	el := tree.Find("text:Hel")
	if el == nil {
		t.Fatal("expected text: to match substring")
	}
	if el.Text != "Hello" {
		t.Fatalf("got %q, want Hello", el.Text)
	}
}

func TestMultiFilterSelectorAND(t *testing.T) {
	tree, _ := Parse(androidAttrDump)
	sel := Selector{Filters: []AttrFilter{
		{Attr: "scrollable", Value: "true"},
		{Attr: "resource-id", Value: "row1"},
	}}
	node := tree.Root.FindBySelector(sel)
	if node == nil {
		t.Fatal("expected AND selector to find row1 (scrollable=true AND resource-id contains row1)")
	}
	if node.Element.ResourceID != "com.app:id/row1" {
		t.Fatalf("got %q, want row1", node.Element.ResourceID)
	}
}

func TestMultiFilterSelectorMissReturnsNil(t *testing.T) {
	tree, _ := Parse(androidAttrDump)
	sel := Selector{Filters: []AttrFilter{
		{Attr: "scrollable", Value: "true"},
		{Attr: "resource-id", Value: "row2"}, // row2 is not scrollable=true
	}}
	node := tree.Root.FindBySelector(sel)
	if node != nil {
		t.Fatal("expected AND selector to return nil when one filter misses")
	}
}

func TestNodeFindScopedSearch(t *testing.T) {
	tree, _ := Parse(pathDump)
	// A2 has a child B2 with content-desc "label_b"
	// Find the A node, then search its subtree for label_b -- should find B (not B2)
	aNode := tree.FindNode("id:A")
	if aNode == nil {
		t.Fatal("expected to find A node")
	}
	result := aNode.Find("desc:label_b")
	if result == nil {
		t.Fatal("expected Node.Find to find label_b in A's subtree")
	}
	if result.Element.ResourceID != "B" {
		t.Fatalf("got %q, want B (not B2 from sibling A2)", result.Element.ResourceID)
	}
}

func TestTestTagAliasMatchesResourceIDAndroid(t *testing.T) {
	input := `{
	  "attributes": {"resource-id": "AccountCard", "bounds": "[0,0,100,100]"},
	  "children": []
	}`
	tree, _ := Parse(input)
	sel := Selector{Filters: []AttrFilter{{Attr: "testTag", Value: "AccountCard"}}}
	if len(searchSubtreeBySelector(tree.Root, sel)) == 0 {
		t.Fatal("expected testTag selector to match resource-id on Android")
	}
}

func TestTestTagAliasMatchesAccessibilityIdentifierIOS(t *testing.T) {
	input := `{
	  "attributes": {"accessibilityIdentifier": "AccountCard", "bounds": "[0,0,100,100]"},
	  "children": []
	}`
	tree, _ := Parse(input)
	sel := Selector{Filters: []AttrFilter{{Attr: "testTag", Value: "AccountCard"}}}
	matches := searchSubtreeBySelector(tree.Root, sel)
	if len(matches) == 0 {
		t.Fatal("expected testTag selector to match accessibilityIdentifier on iOS")
	}
}

func TestTestTagAliasMatchesIdentifierIOSRaw(t *testing.T) {
	input := `{
	  "attributes": {"identifier": "AccountCard", "bounds": "[0,0,100,100]"},
	  "children": []
	}`
	tree, _ := Parse(input)
	sel := Selector{Filters: []AttrFilter{{Attr: "testTag", Value: "AccountCard"}}}
	matches := searchSubtreeBySelector(tree.Root, sel)
	if len(matches) == 0 {
		t.Fatal("expected testTag selector to match identifier on iOS raw AXElement")
	}
}

func TestResourceIDFallsBackToAccessibilityIdentifier(t *testing.T) {
	input := `{
	  "attributes": {"accessibilityIdentifier": "MyButton", "bounds": "[0,0,100,100]"},
	  "children": []
	}`
	tree, _ := Parse(input)
	sel := Selector{Filters: []AttrFilter{{Attr: "resource-id", Value: "MyButton"}}}
	if len(searchSubtreeBySelector(tree.Root, sel)) == 0 {
		t.Fatal("expected resource-id selector to fall back to accessibilityIdentifier")
	}
}

func TestElementResourceIDPopulatesFromAccessibilityIdentifier(t *testing.T) {
	input := `{
	  "attributes": {"accessibilityIdentifier": "LoginEmail", "bounds": "[0,0,100,100]"},
	  "children": []
	}`
	tree, _ := Parse(input)
	if len(tree.Elements) == 0 {
		t.Fatal("no elements parsed")
	}
	if got := tree.Elements[0].ResourceID; got != "LoginEmail" {
		t.Fatalf("ResourceID = %q, want LoginEmail (iOS Compose accessibilityIdentifier path)", got)
	}
}

const selectorPathDump = `{
  "attributes": {"resource-id": "rootView", "bounds": "[0,0,1080,2340]"},
  "children": [
    {
      "attributes": {"testTag": "HomeScreen", "bounds": "[0,0,540,2340]"},
      "children": [
        {
          "attributes": {"testTag": "AccountCard", "bounds": "[0,0,540,200]"},
          "children": [
            {"attributes": {"testTag": "AccountName", "text": "Checking", "bounds": "[10,10,200,40]"}, "children": []}
          ]
        },
        {
          "attributes": {"testTag": "AccountCard", "bounds": "[0,200,540,400]"},
          "children": [
            {"attributes": {"testTag": "AccountName", "text": "Savings", "bounds": "[10,210,200,240]"}, "children": []}
          ]
        }
      ]
    },
    {
      "attributes": {"testTag": "LedgerScreen", "bounds": "[540,0,1080,2340]"},
      "children": [
        {"attributes": {"testTag": "AccountName", "text": "Travel", "bounds": "[600,10,800,40]"}, "children": []}
      ]
    }
  ]
}`

func TestFindBySelectorPathSingleSegment(t *testing.T) {
	tree, _ := Parse(selectorPathDump)
	path := []Selector{{Filters: []AttrFilter{{Attr: "testTag", Value: "HomeScreen"}}}}
	node := tree.FindBySelectorPath(path)
	if node == nil {
		t.Fatal("expected match for HomeScreen")
	}
	if got := node.Element.Attributes["testTag"]; got != "HomeScreen" {
		t.Fatalf("testTag = %q, want HomeScreen", got)
	}
}

func TestFindBySelectorPathScopedDescent(t *testing.T) {
	tree, _ := Parse(selectorPathDump)
	path := []Selector{
		{Filters: []AttrFilter{{Attr: "testTag", Value: "HomeScreen"}}},
		{Filters: []AttrFilter{{Attr: "testTag", Value: "AccountCard"}}},
		{Filters: []AttrFilter{{Attr: "testTag", Value: "AccountName"}}},
	}
	node := tree.FindBySelectorPath(path)
	if node == nil {
		t.Fatal("expected match for HomeScreen > AccountCard > AccountName")
	}
	if node.Element.Text != "Checking" {
		t.Fatalf("text = %q, want Checking", node.Element.Text)
	}
}

func TestFindBySelectorPathRespectsScope(t *testing.T) {
	tree, _ := Parse(selectorPathDump)
	path := []Selector{
		{Filters: []AttrFilter{{Attr: "testTag", Value: "LedgerScreen"}}},
		{Filters: []AttrFilter{{Attr: "testTag", Value: "AccountCard"}}},
	}
	if node := tree.FindBySelectorPath(path); node != nil {
		t.Fatalf("AccountCard is under HomeScreen only, expected nil, got %+v", node.Element)
	}
}

func TestFindAllBySelectorPathReturnsAllDeepestMatches(t *testing.T) {
	tree, _ := Parse(selectorPathDump)
	path := []Selector{
		{Filters: []AttrFilter{{Attr: "testTag", Value: "HomeScreen"}}},
		{Filters: []AttrFilter{{Attr: "testTag", Value: "AccountName"}}},
	}
	matches := tree.FindAllBySelectorPath(path)
	if len(matches) != 2 {
		t.Fatalf("want 2 matches (Checking, Savings), got %d", len(matches))
	}
}

func TestFindBySelectorPathEmptyPathReturnsNil(t *testing.T) {
	tree, _ := Parse(selectorPathDump)
	if tree.FindBySelectorPath(nil) != nil {
		t.Fatal("empty path should return nil")
	}
}

func TestNodeFindDoesNotReturnSiblings(t *testing.T) {
	tree, _ := Parse(pathDump)
	a2Node := tree.FindNode("id:A2")
	if a2Node == nil {
		t.Fatal("expected to find A2 node")
	}
	// B is under A, not A2 -- should not be found from A2's subtree
	result := a2Node.Find("id:B")
	if result != nil && result.Element.ResourceID == "B" {
		t.Fatal("Node.Find should not return nodes from sibling subtrees")
	}
}

// TestPackageDerivedFromResourceIDPrefix verifies that when the sidecar omits an
// explicit package attribute, native nodes pick it up from the resource-id
// prefix while colon-less Compose testTags stay empty (in scope for the app).
func TestPackageDerivedFromResourceIDPrefix(t *testing.T) {
	const dump = `{
	  "attributes": {"class": "android.widget.FrameLayout", "bounds": "[0,0,320,640]"},
	  "children": [
	    {"attributes": {"resource-id": "AddAccountScreen", "bounds": "[0,0,320,400]"}, "clickable": true, "enabled": true, "children": []},
	    {"attributes": {"resource-id": "com.google.android.inputmethod.latin:id/key_pos_0_0", "bounds": "[0,400,40,440]"}, "clickable": true, "enabled": true, "children": []},
	    {"attributes": {"resource-id": "android:id/content", "bounds": "[0,0,320,640]"}, "children": []}
	  ]
	}`
	tree, err := Parse(dump)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := map[string]string{
		"AddAccountScreen": "",
		"com.google.android.inputmethod.latin:id/key_pos_0_0": "com.google.android.inputmethod.latin",
		"android:id/content": "android",
	}
	for _, element := range tree.Elements {
		expected, ok := want[element.ResourceID]
		if !ok {
			continue
		}
		if element.Package != expected {
			t.Errorf("resource-id %q: package = %q, want %q", element.ResourceID, element.Package, expected)
		}
	}
}

// TestExplicitPackageAttributeWins verifies an explicit package attribute is not
// overridden by the resource-id prefix fallback.
func TestExplicitPackageAttributeWins(t *testing.T) {
	const dump = `{
	  "attributes": {"resource-id": "android:id/content", "package": "app.folio", "bounds": "[0,0,320,640]"},
	  "children": []
	}`
	tree, err := Parse(dump)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := tree.Elements[0].Package; got != "app.folio" {
		t.Errorf("package = %q, want app.folio (explicit attr should win)", got)
	}
}

// iosFlatDump mirrors the Compose-on-iOS accessibility shape: the testTag node
// surfaces as an empty leaf SIBLING of the container holding the content it
// labels, with equal bounds, instead of as an ancestor.
const iosFlatDump = `{
  "attributes": {"bounds": "[0,0][402,874]"},
  "children": [
    {
      "attributes": {"accessibilityText": "Folio", "bounds": "[0,0][402,874]"},
      "children": [
        {
          "attributes": {"resource-id": "LoginScreen", "bounds": "[0,62][402,840]"},
          "children": []
        },
        {
          "attributes": {"bounds": "[0,62][402,840]"},
          "children": [
            {
              "attributes": {"accessibilityText": "EMAIL", "bounds": "[20,106][60,120]"},
              "children": []
            },
            {
              "attributes": {"resource-id": "LoginEmail", "accessibilityText": "Email", "bounds": "[34,125][368,173]"},
              "children": [
                {"attributes": {"bounds": "[34,125][368,173]"}, "children": []}
              ]
            },
            {
              "attributes": {"resource-id": "LoginPassword", "accessibilityText": "Password", "bounds": "[34,205][368,253]"},
              "children": []
            },
            {
              "attributes": {"text": "Sign in", "bounds": "[20,297][382,345]"},
              "children": []
            },
            {
              "attributes": {"resource-id": "LoginSubmit", "accessibilityText": "Sign in", "bounds": "[20,297][382,345]"},
              "children": [
                {"attributes": {"text": "Sign in", "resource-id": "SubmitLabel", "bounds": "[168,312][233,330]"}, "children": []}
              ]
            }
          ]
        }
      ]
    },
    {
      "attributes": {"bounds": "[0,0][402,54]"},
      "children": [
        {
          "attributes": {"resource-id": "StatusClock", "accessibilityText": "3:24 PM", "bounds": "[55,22][92,42]"},
          "children": []
        }
      ]
    }
  ]
}`

func TestIOSFlatSelectorPathFallsBackToBounds(t *testing.T) {
	tree, err := Parse(iosFlatDump)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	path := []Selector{
		{Filters: []AttrFilter{{Attr: "testTag", Value: "LoginScreen"}}},
		{Filters: []AttrFilter{{Attr: "testTag", Value: "LoginEmail"}}},
	}
	node := tree.FindBySelectorPath(path)
	if node == nil {
		t.Fatal("expected LoginEmail via bounds containment under leaf LoginScreen")
	}
	if node.ResourceID != "LoginEmail" {
		t.Fatalf("got %q, want LoginEmail", node.ResourceID)
	}
}

func TestIOSFlatStringPathFallsBackToBounds(t *testing.T) {
	tree, _ := Parse(iosFlatDump)
	element := tree.Find("id:LoginScreen > id:LoginSubmit")
	if element == nil {
		t.Fatal("expected LoginSubmit via bounds containment under leaf LoginScreen")
	}
	if element.ResourceID != "LoginSubmit" {
		t.Fatalf("got %q, want LoginSubmit", element.ResourceID)
	}
}

func TestIOSFlatScopedNodeFindFallsBackToBounds(t *testing.T) {
	tree, _ := Parse(iosFlatDump)
	screen := tree.FindNode("id:LoginScreen")
	if screen == nil {
		t.Fatal("expected LoginScreen node")
	}
	node := screen.Find("id:LoginPassword")
	if node == nil {
		t.Fatal("expected LoginPassword via bounds containment")
	}
	if node.ResourceID != "LoginPassword" {
		t.Fatalf("got %q, want LoginPassword", node.ResourceID)
	}
}

func TestIOSFlatFindAllBySelectorPathReturnsEachField(t *testing.T) {
	tree, _ := Parse(iosFlatDump)
	path := []Selector{
		{Filters: []AttrFilter{{Attr: "testTag", Value: "LoginScreen"}}},
		{Filters: []AttrFilter{{Attr: "label", Value: "word"}}},
	}
	// label "word" substring-matches "Password" only.
	nodes := tree.FindAllBySelectorPath(path)
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(nodes))
	}
	if nodes[0].ResourceID != "LoginPassword" {
		t.Fatalf("got %q, want LoginPassword", nodes[0].ResourceID)
	}
}

func TestIOSFlatSpatialScopeExcludesOutsideBounds(t *testing.T) {
	tree, _ := Parse(iosFlatDump)
	// StatusClock sits in the status bar above LoginScreen's bounds; the
	// spatial fallback must not leak it into the screen's scope.
	if tree.Find("id:LoginScreen > id:StatusClock") != nil {
		t.Fatal("StatusClock is outside LoginScreen bounds, expected nil")
	}
}

// spatialSpecificityDump scopes a leaf testTag (iOS-flat pattern) over a
// screen where both a screen-sized container and the small element inside it
// match the same attribute, plus two equal-bounds siblings for tie ordering.
const spatialSpecificityDump = `{
  "attributes": {"bounds": "[0,0][402,874]"},
  "children": [
    {
      "attributes": {"resource-id": "FormScreen", "bounds": "[0,62][402,840]"},
      "children": []
    },
    {
      "attributes": {"resource-id": "FieldContainer", "accessibilityText": "Amount", "bounds": "[0,62][402,840]"},
      "children": [
        {
          "attributes": {"resource-id": "AmountField", "accessibilityText": "Amount", "bounds": "[34,125][368,173]"},
          "children": []
        },
        {
          "attributes": {"resource-id": "FirstTab", "accessibilityText": "Tab", "bounds": "[20,297][382,345]"},
          "children": []
        },
        {
          "attributes": {"resource-id": "SecondTab", "accessibilityText": "Tab", "bounds": "[20,297][382,345]"},
          "children": []
        }
      ]
    }
  ]
}`

func TestSpatialFallbackPrefersSmallestContainingMatch(t *testing.T) {
	tree, err := Parse(spatialSpecificityDump)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	screen := tree.FindNode("id:FormScreen")
	if screen == nil {
		t.Fatal("expected FormScreen node")
	}
	// Both FieldContainer (screen-sized, earlier in pre-order) and
	// AmountField (small) match; the most specific match must win.
	node := screen.Find("desc:Amount")
	if node == nil {
		t.Fatal("expected a spatial-fallback match")
	}
	if node.ResourceID != "AmountField" {
		t.Fatalf("expected the smallest containing match AmountField, got id=%q", node.ResourceID)
	}
}

func TestSpatialFallbackEqualAreaKeepsPreOrder(t *testing.T) {
	tree, err := Parse(spatialSpecificityDump)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	screen := tree.FindNode("id:FormScreen")
	if screen == nil {
		t.Fatal("expected FormScreen node")
	}
	node := screen.Find("desc:Tab")
	if node == nil {
		t.Fatal("expected a spatial-fallback match")
	}
	if node.ResourceID != "FirstTab" {
		t.Fatalf("equal-area matches must keep pre-order, got id=%q", node.ResourceID)
	}
}

func TestIOSFlatStructuralChildStillPreferred(t *testing.T) {
	tree, _ := Parse(iosFlatDump)
	submit := tree.FindNode("id:LoginSubmit")
	if submit == nil {
		t.Fatal("expected LoginSubmit node")
	}
	// "Sign in" exists as the structural child of LoginSubmit and as an
	// equal-bounds sibling decoration; the structural child must win.
	node := submit.Find("text:Sign in")
	if node == nil {
		t.Fatal("expected structural child match")
	}
	if node.ResourceID != "SubmitLabel" {
		t.Fatalf("expected the structural child SubmitLabel, got id=%q", node.ResourceID)
	}
}

// Bug class: malformed device output must surface as an error, not a nil tree
// the callers dereference. A blank string is the only documented benign input.
func TestParseInvalidJSON(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"truncated object", `{"attributes":`, true},
		{"trailing garbage", `{"attributes":{}} oops`, true},
		{"not an object", `[1,2,3]`, true},
		{"bare garbage", `not json at all`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Parse(%q) err = %v, wantErr %v", tc.input, err, tc.wantErr)
			}
		})
	}
}

// Bug class: a node carrying a bounds value the device emitted in an
// unrecognized shape must degrade to a zero rectangle without losing the rest
// of the element. parseBounds' error is intentionally swallowed in
// elementFromNode, so a regression that aborts the whole parse, or that
// corrupts neighboring fields, would be caught here.
func TestParseMalformedBoundsKeepsElementIntact(t *testing.T) {
	cases := []string{
		"[1,2,3]",   // too few coordinates
		"garbage",   // not a bounds string at all
		"[a,b,c,d]", // non-numeric
		"",          // empty
	}
	for _, b := range cases {
		t.Run(b, func(t *testing.T) {
			input := `{"attributes":{"resource-id":"app:id/x","text":"keep","bounds":"` + b + `"},"children":[]}`
			tree, err := Parse(input)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			el := tree.Find("id:x")
			if el == nil {
				t.Fatal("element dropped when bounds was malformed")
			}
			if el.Text != "keep" {
				t.Errorf("neighboring field corrupted: text=%q", el.Text)
			}
			if el.Bounds != (Bounds{}) {
				t.Errorf("malformed bounds must yield zero rectangle, got %+v", el.Bounds)
			}
		})
	}
}

// Bug class: parseBounds must reject inputs that are neither [L,T,R,B] nor
// [x1,y1][x2,y2] rather than returning a partially-filled rectangle.
func TestParseBoundsRejectsBadInput(t *testing.T) {
	for _, in := range []string{"[1,2,3]", "[1,2,3,4,5]", "1,2,3,4", "[]", "garbage"} {
		if _, err := parseBounds(in); err == nil {
			t.Errorf("parseBounds(%q) = nil error, want error", in)
		}
	}
}

// Bug class: a NavHost cross-fade carries two route-level *Screen ids at once;
// both the runner's re-fetch guard and the LLM's candidate enumeration depend on
// spotting it, and neither must flag a settled single-screen tree.
func TestTreeTransitional(t *testing.T) {
	multi, err := Parse(`{"attributes":{"resource-id":"root"},"children":[
	  {"attributes":{"resource-id":"AddAccountScreen"},"children":[]},
	  {"attributes":{"resource-id":"HomeScreen"},"children":[]}
	]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !multi.Transitional() {
		t.Error("expected multi-screen tree to be flagged as transitional")
	}

	single, err := Parse(`{"attributes":{"resource-id":"HomeScreen"},"children":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	if single.Transitional() {
		t.Error("single-screen tree must not be flagged as transitional")
	}

	var nilTree *Tree
	if nilTree.Transitional() {
		t.Error("nil tree must not be flagged as transitional")
	}
}

// A key means the same thing whichever form the author writes it in. The object
// form used to fall through to the raw attribute map, which carries neither
// "id" nor "desc" on any platform.
func TestObjectSelectorIDMatchesTheSameElementsAsTheStringForm(t *testing.T) {
	tree, _ := Parse(sampleDump)
	sel := Selector{Filters: []AttrFilter{{Attr: "id", Value: "row"}}}
	object := tree.Root.FindAllBySelector(sel)
	if len(object) != len(tree.FindAll("id:row")) {
		t.Fatalf("object form matched %d, string form %d", len(object), len(tree.FindAll("id:row")))
	}
	if len(object) != 2 {
		t.Fatalf("want 2 rows, got %d", len(object))
	}
}

func TestObjectSelectorDescMatchesTheSameElementsAsTheStringForm(t *testing.T) {
	tree, _ := Parse(sampleDump)
	sel := Selector{Filters: []AttrFilter{{Attr: "desc", Value: "row"}}}
	if len(tree.Root.FindAllBySelector(sel)) != len(tree.FindAll("desc:row")) {
		t.Fatal("object and string form disagree on desc")
	}
}

func TestUnknownSelectorKeyIsReported(t *testing.T) {
	tree, _ := Parse(sampleDump)
	sel := Selector{Filters: []AttrFilter{{Attr: "descripton", Value: "row"}}}
	unknown := tree.UnknownSelectorKeys(sel)
	if len(unknown) != 1 || unknown[0] != "descripton" {
		t.Fatalf("got %v, want [descripton]", unknown)
	}
	message := UnknownSelectorKeyMessage(unknown)
	if !strings.Contains(message, `"descripton"`) || !strings.Contains(message, "resource-id") {
		t.Fatalf("message names neither the key nor the accepted list: %s", message)
	}
}

func TestAcceptedSelectorKeyAbsentFromTheScreenIsNotUnknown(t *testing.T) {
	tree, _ := Parse(sampleDump)
	sel := Selector{Filters: []AttrFilter{{Attr: "title", Value: "Settings"}}}
	if unknown := tree.UnknownSelectorKeys(sel); len(unknown) != 0 {
		t.Fatalf("a platform-specific key must stay silent, got %v", unknown)
	}
}

// Raw driver attributes stay reachable: a key some element carries can match,
// whether or not this package enumerates it.
func TestRawDriverAttributeIsNotUnknown(t *testing.T) {
	input := `{
	  "attributes": {"resource-id": "root", "important-for-accessibility": "true"},
	  "children": []
	}`
	tree, _ := Parse(input)
	sel := Selector{Filters: []AttrFilter{{Attr: "important-for-accessibility", Value: "true"}}}
	if unknown := tree.UnknownSelectorKeys(sel); len(unknown) != 0 {
		t.Fatalf("got %v, want none", unknown)
	}
}
