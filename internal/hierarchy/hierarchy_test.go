package hierarchy

import "testing"

const sampleDump = `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>
<hierarchy rotation="0">
  <node class="android.widget.LinearLayout" package="app" bounds="[0,0][1080,2340]">
    <node resource-id="app:id/title" text="Hello" bounds="[10,20][200,60]" clickable="false" enabled="true"/>
    <node resource-id="app:id/row" text="Alice" content-desc="row" bounds="[0,100][1080,200]" clickable="true" enabled="true"/>
    <node resource-id="app:id/row" text="Bob" content-desc="row" bounds="[0,200][1080,300]" clickable="true" enabled="true"/>
  </node>
</hierarchy>`

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
	xml := `<hierarchy>
		<node content-desc="customer_row_abc-123" bounds="[0,0][100,100]"/>
		<node content-desc="customer_row_def-456" bounds="[0,100][100,200]"/>
		<node content-desc="supplier_row_xyz" bounds="[0,200][100,300]"/>
	</hierarchy>`
	tree, _ := Parse(xml)
	rows := tree.FindAll("descPrefix:customer_row_")
	if len(rows) != 2 {
		t.Fatalf("want 2 customer rows, got %d", len(rows))
	}
}
