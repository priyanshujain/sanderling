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
