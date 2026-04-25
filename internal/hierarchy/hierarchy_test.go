package hierarchy

import "testing"

// sampleDump is a Maestro TreeNode JSON equivalent of the old XML fixture.
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

func TestScrollableGracefulIgnoreOnIOS(t *testing.T) {
	tree, _ := Parse(iosAttrDump)
	el := tree.Find("scrollable:true")
	if el != nil {
		t.Fatal("expected scrollable:true to return nil on iOS hierarchy (graceful ignore)")
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
