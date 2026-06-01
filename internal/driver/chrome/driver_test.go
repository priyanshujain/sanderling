package chrome

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestHierarchy_EditableFlag confirms the injected hierarchy script marks text
// inputs, textareas, and contenteditable elements editable while leaving
// buttons and non-text inputs alone.
func TestHierarchy_EditableFlag(t *testing.T) {
	const html = `<body>` +
		`<input id="name">` +
		`<textarea id="bio"></textarea>` +
		`<button id="go">go</button>` +
		`<div id="rich" contenteditable="true">x</div>` +
		`<input id="chk" type="checkbox">` +
		`</body>`

	d := New()
	defer d.Terminate(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := d.Launch(ctx, "data:text/html,"+html, false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	dump, err := d.Hierarchy(ctx)
	if err != nil {
		t.Fatalf("Hierarchy: %v", err)
	}

	type node struct {
		Attributes map[string]string `json:"attributes"`
		Children   []node            `json:"children"`
		Editable   *bool             `json:"editable"`
	}
	var root node
	if err := json.Unmarshal([]byte(dump), &root); err != nil {
		t.Fatalf("unmarshal hierarchy: %v", err)
	}
	editableByID := map[string]*bool{}
	var walk func(n node)
	walk = func(n node) {
		if id := n.Attributes["resource-id"]; id != "" {
			editableByID[id] = n.Editable
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)

	isEditable := func(id string) bool {
		return editableByID[id] != nil && *editableByID[id]
	}
	for _, id := range []string{"name", "bio", "rich"} {
		if !isEditable(id) {
			t.Errorf("%q: editable = %v, want true", id, editableByID[id])
		}
	}
	for _, id := range []string{"go", "chk"} {
		if isEditable(id) {
			t.Errorf("%q: editable = true, want false/absent", id)
		}
	}
}

// TestRunCtx_CallerCancelPropagates confirms that cancelling the caller's
// context cancels the chromedp-bound context returned by runCtx. This is the
// channel by which step deadlines and Ctrl-C reach in-flight CDP calls.
func TestRunCtx_CallerCancelPropagates(t *testing.T) {
	tabCtx, tabCancel := context.WithCancel(context.Background())
	defer tabCancel()
	d := &Driver{tabCtx: tabCtx}

	callerCtx, callerCancel := context.WithCancel(context.Background())
	derived, cancel := d.runCtx(callerCtx)
	defer cancel()

	callerCancel()
	select {
	case <-derived.Done():
	case <-time.After(time.Second):
		t.Fatal("derived ctx did not cancel after caller cancellation")
	}
}

// TestRunCtx_TabCancelPropagates confirms the inverse: tearing down the tab
// also cancels any in-flight derived context.
func TestRunCtx_TabCancelPropagates(t *testing.T) {
	tabCtx, tabCancel := context.WithCancel(context.Background())
	d := &Driver{tabCtx: tabCtx}

	derived, cancel := d.runCtx(context.Background())
	defer cancel()

	tabCancel()
	select {
	case <-derived.Done():
	case <-time.After(time.Second):
		t.Fatal("derived ctx did not cancel after tab cancellation")
	}
}
