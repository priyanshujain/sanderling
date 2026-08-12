//go:build browser

package chrome

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestLaunch_ClearStateWipesStorageForTheTargetOrigin covers the CLI's default
// path (--clear-data). The tab sits on about:blank when Launch runs, an opaque
// origin that denies storage access, so clearing by script there throws
// SecurityError and kills every web run before the app loads.
func TestLaunch_ClearStateWipesStorageForTheTargetOrigin(t *testing.T) {
	const page = `<body><script>
	  const visits = Number(localStorage.getItem("visits") ?? "0") + 1;
	  localStorage.setItem("visits", String(visits));
	  sessionStorage.setItem("tab", "dirty");
	</script></body>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}))
	defer server.Close()

	d := New()
	defer d.Terminate(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := d.Launch(ctx, server.URL, true, nil); err != nil {
		t.Fatalf("Launch with clearState on a fresh tab: %v", err)
	}
	if err := d.Launch(ctx, server.URL, false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	var visits string
	if err := chromedp.Run(d.tabCtx,
		chromedp.Evaluate(`localStorage.getItem("visits")`, &visits)); err != nil {
		t.Fatalf("read localStorage: %v", err)
	}
	if visits != "2" {
		t.Fatalf("visits = %q, want 2 (two loads, storage kept)", visits)
	}

	if err := d.Launch(ctx, server.URL, true, nil); err != nil {
		t.Fatalf("Launch with clearState on the target origin: %v", err)
	}
	if err := chromedp.Run(d.tabCtx,
		chromedp.Evaluate(`localStorage.getItem("visits")`, &visits)); err != nil {
		t.Fatalf("read localStorage: %v", err)
	}
	if visits != "1" {
		t.Errorf("visits = %q, want 1 (storage cleared before the app loaded)", visits)
	}
}

// TestLaunch_WebGLContextIsAvailable pins the SwiftShader fallback. Headless
// Chrome runs with --disable-gpu, and without --enable-unsafe-swiftshader it
// refuses the software WebGL backend: getContext returns null, so a
// canvas-rendered app paints nothing and every screenshot is identical black.
func TestLaunch_WebGLContextIsAvailable(t *testing.T) {
	d := New()
	defer d.Terminate(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := d.Launch(ctx, "data:text/html,<body></body>", false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	var hasContext bool
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(
		`!!document.createElement("canvas").getContext("webgl2")`, &hasContext)); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !hasContext {
		t.Error("webgl2 context is null; a canvas-rendered app would render nothing")
	}
}

// TestActionMethods_HonorCallerCancellation confirms the DeviceDriver action
// methods route through runCtx so a cancelled caller context aborts the CDP
// round-trip instead of blocking on d.tabCtx. Without this a hung browser would
// ignore step deadlines and Ctrl-C.
func TestActionMethods_HonorCallerCancellation(t *testing.T) {
	d := New()
	defer d.Terminate(context.Background())
	launchCtx, launchCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer launchCancel()
	if err := d.Launch(launchCtx, "data:text/html,<body><button id=go>go</button></body>", false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	actions := map[string]func() error{
		"Tap":         func() error { return d.Tap(cancelled, 1, 1) },
		"Swipe":       func() error { return d.Swipe(cancelled, 1, 1, 2, 2, 50*time.Millisecond) },
		"LongPress":   func() error { return d.LongPress(cancelled, 1, 1) },
		"PressKey":    func() error { return d.PressKey(cancelled, "enter") },
		"InputText":   func() error { return d.InputText(cancelled, "x") },
		"EraseText":   func() error { return d.EraseText(cancelled, 1) },
		"TapSelector": func() error { return d.TapSelector(cancelled, "#go") },
	}
	for name, action := range actions {
		t.Run(name, func(t *testing.T) {
			done := make(chan error, 1)
			go func() { done <- action() }()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("want context.Canceled, got %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("action ignored cancelled caller ctx and blocked")
			}
		})
	}
}

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

// TestHierarchy_ClickableMatchesTheWebRuntimeSelector pins clickable to the
// same membership test pkg/spec/src/web-runtime.ts applies. The dump used to
// test el.onclick, which React sets on its root container for event delegation,
// so the whole viewport became a tap target in this dump and in no other
// enumeration of the same page.
func TestHierarchy_ClickableMatchesTheWebRuntimeSelector(t *testing.T) {
	const html = `<body>` +
		`<div id="root"><button id="go">go</button></div>` +
		`<textarea id="bio"></textarea>` +
		`<div id="plain">text</div>` +
		`<div id="rolebutton" role="button">act</div>` +
		`<script>document.getElementById("root").onclick = function () {};</script>` +
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
		Clickable  *bool             `json:"clickable"`
	}
	var root node
	if err := json.Unmarshal([]byte(dump), &root); err != nil {
		t.Fatalf("unmarshal hierarchy: %v", err)
	}
	clickableByID := map[string]*bool{}
	var walk func(n node)
	walk = func(n node) {
		if id := n.Attributes["resource-id"]; id != "" {
			clickableByID[id] = n.Clickable
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)

	isClickable := func(id string) bool {
		return clickableByID[id] != nil && *clickableByID[id]
	}
	for _, id := range []string{"go", "bio", "rolebutton"} {
		if !isClickable(id) {
			t.Errorf("%q: clickable = %v, want true", id, clickableByID[id])
		}
	}
	for _, id := range []string{"root", "plain"} {
		if isClickable(id) {
			t.Errorf("%q: clickable = true, want false/absent (an onclick property is not a target)", id)
		}
	}
}

// TestHierarchy_ScrollableAttribute covers the fact the goja host reads off the
// attributes map. The V8 picker computes the same overflow test in
// web-runtime.ts, so leaving it out of the dump made the two enumerations
// disagree on web: the model policy could never be offered a scroll the seeded
// policy could draw.
func TestHierarchy_ScrollableAttribute(t *testing.T) {
	const html = `<body style="margin:0">` +
		`<div id="overflowing" style="width:100px;height:100px;overflow:auto">` +
		`<div style="width:100px;height:900px"></div></div>` +
		`<div id="sideways" style="width:100px;height:50px;overflow:auto">` +
		`<div style="width:900px;height:20px"></div></div>` +
		`<div id="fits" style="width:100px;height:100px;overflow:auto">` +
		`<div style="width:50px;height:50px"></div></div>` +
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
	}
	var root node
	if err := json.Unmarshal([]byte(dump), &root); err != nil {
		t.Fatalf("unmarshal hierarchy: %v", err)
	}
	scrollableByID := map[string]string{}
	var walk func(n node)
	walk = func(n node) {
		if id := n.Attributes["resource-id"]; id != "" {
			scrollableByID[id] = n.Attributes["scrollable"]
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)

	for _, id := range []string{"overflowing", "sideways"} {
		if scrollableByID[id] != "true" {
			t.Errorf("%q: scrollable = %q, want \"true\"", id, scrollableByID[id])
		}
	}
	if scrollableByID["fits"] != "" {
		t.Errorf("%q: scrollable = %q, want absent", "fits", scrollableByID["fits"])
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

// TestHierarchy_RootsAtDocumentElementWithoutHead pins the dump's root to html,
// because collectTargets in pkg/spec/src/web-runtime.ts walks
// querySelectorAll("*") and therefore sees html. A dump rooted at body hides
// page-level scrolling, which lives on html on a standard page, so the two
// enumerations disagree on exactly that candidate. The head subtree stays out:
// it is all zero-bounds, so it changes no eligible set, and it would otherwise
// carry script and title text into the trace.
func TestHierarchy_RootsAtDocumentElementWithoutHead(t *testing.T) {
	const html = `<html><head><title>secret title</title>` +
		`<script>window.marker = 1;</script></head>` +
		`<body><div id="content">hello</div></body></html>`

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
	}
	var root node
	if err := json.Unmarshal([]byte(dump), &root); err != nil {
		t.Fatalf("unmarshal hierarchy: %v", err)
	}
	if root.Attributes["tag"] != "html" {
		t.Errorf("root tag: got %q, want html", root.Attributes["tag"])
	}
	if root.Attributes["sanderling-screen"] == "" {
		t.Error("the root must still carry sanderling-screen")
	}

	tags := map[string]bool{}
	var walk func(n node)
	walk = func(n node) {
		tags[n.Attributes["tag"]] = true
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	if !tags["body"] {
		t.Error("body must appear under the root")
	}
	for _, tag := range []string{"head", "title", "script"} {
		if tags[tag] {
			t.Errorf("the head subtree must stay out of the dump, found %q", tag)
		}
	}
}
