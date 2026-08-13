//go:build browser

package chrome

import (
	"context"
	"encoding/json"
	"errors"
	"net"
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

// TestLaunch_HonorsCallerDeadline points the driver at a listener that accepts
// the connection and never answers. Chrome has no page-load deadline of its
// own, so a Launch that ignored its caller context waited forever: an
// unattended campaign worker aimed at an unreachable target wedged with no
// diagnostic and did not even answer SIGTERM.
func TestLaunch_HonorsCallerDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	held := make(chan net.Conn, 8)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			held <- conn
		}
	}()
	defer func() {
		close(held)
		for conn := range held {
			conn.Close()
		}
	}()

	d := New()
	defer d.Terminate(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Launch(ctx, "http://"+listener.Addr().String(), false, nil) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Launch returned nil against a target that never answers")
		}
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			t.Fatalf("Launch error = %v, want a context error", err)
		}
	case <-time.After(45 * time.Second):
		t.Fatal("Launch ignored the caller deadline and blocked")
	}
}

// TestLaunch_KeepsBrowserAliveAfterCallerContextEnds guards the trap that made
// Launch use the driver context in the first place: chromedp starts Chrome
// under whichever context runs first, so allocating under the caller's
// short-lived context would kill the browser as soon as Launch returned.
func TestLaunch_KeepsBrowserAliveAfterCallerContextEnds(t *testing.T) {
	d := New()
	defer d.Terminate(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	if err := d.Launch(ctx, "data:text/html,<body>alive</body>", false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	cancel()

	var text string
	if err := chromedp.Run(d.tabCtx,
		chromedp.Evaluate(`document.body.textContent`, &text)); err != nil {
		t.Fatalf("browser died with the caller context: %v", err)
	}
	if text != "alive" {
		t.Errorf("body text = %q, want \"alive\"", text)
	}
	if err := d.Launch(context.Background(), "data:text/html,<body>again</body>", false, nil); err != nil {
		t.Fatalf("second Launch after the first caller context ended: %v", err)
	}
}

// TestInputText_ReplacesTextInsideAShadowRoot pins ReplacesTextOnInput's promise
// on the shape a canvas app actually has. Compose for Web draws its text fields
// on a canvas and routes typing through a hidden <input> INSIDE the shadow root
// it mounts, and document.activeElement stops at a shadow boundary: it names the
// host. The select-all therefore ran against a <div> with no select(), every
// InputText appended to the last, and a fuzzer typing twice into one field built
// up text it could never clear (observed on the folio wasm build as
// "0.0000001" -> "0.0000001\t-1").
func TestInputText_ReplacesTextInsideAShadowRoot(t *testing.T) {
	const page = `<body><div id="app"></div><script>
	  const root = document.getElementById("app").attachShadow({mode: "open"});
	  root.innerHTML = ` + "`" + `
	    <style>
	      #surface { position: absolute; left: 0; top: 0; }
	      #a11y { position: absolute; left: 0; top: 0; pointer-events: none; }
	      #proxy { position: absolute; left: -9999px; }
	    </style>
	    <canvas id="surface" width="300" height="200"></canvas>
	    <div id="a11y"><div id="field">-</div></div>
	    <input id="proxy" type="text">` + "`" + `;
	  const proxy = root.getElementById("proxy");
	  const field = root.getElementById("field");
	  // The canvas owns the pointer (the a11y overlay is pointer-events: none)
	  // and hands focus to the proxy, exactly as a canvas app does.
	  root.getElementById("surface").addEventListener("click", function () { proxy.focus(); });
	  proxy.addEventListener("input", function () { field.textContent = proxy.value; });
	</script></body>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}))
	defer server.Close()

	d := New()
	defer d.Terminate(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := d.Launch(ctx, server.URL, false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := d.Tap(ctx, 40, 40); err != nil {
		t.Fatalf("Tap: %v", err)
	}
	if err := d.InputText(ctx, "alpha"); err != nil {
		t.Fatalf("InputText: %v", err)
	}
	if err := d.InputText(ctx, "beta"); err != nil {
		t.Fatalf("InputText: %v", err)
	}

	var shown string
	script := `document.getElementById("app").shadowRoot.getElementById("field").textContent`
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(script, &shown)); err != nil {
		t.Fatalf("read field: %v", err)
	}
	if shown != "beta" {
		t.Errorf("field holds %q, want %q; the second InputText appended instead of replacing", shown, "beta")
	}

	if err := d.EraseText(ctx, len("beta")); err != nil {
		t.Fatalf("EraseText: %v", err)
	}
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(script, &shown)); err != nil {
		t.Fatalf("read field: %v", err)
	}
	if shown != "" {
		t.Errorf("field holds %q after EraseText, want empty", shown)
	}
}

// TestHierarchy_ScreenFallsBackToThePathname pins the route the goja host reads
// off the dump. Reading location.hash alone reported "/" on every step of a
// path-routed SPA (react-router's BrowserRouter, which the replay UI itself
// uses), so every screen looked like the same screen and no route-scoped
// property or action could tell them apart.
func TestHierarchy_ScreenFallsBackToThePathname(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<body><div id="app">app</div></body>`))
	}))
	defer server.Close()

	for _, testCase := range []struct {
		name string
		path string
		want string
	}{
		{"path-routed", "/runs/20260101-120000/steps/7", "/runs/20260101-120000/steps/7"},
		{"hash wins when present", "/runs/1#/detail", "/detail"},
		{"root", "/", "/"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			d := New()
			defer d.Terminate(context.Background())
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := d.Launch(ctx, server.URL+testCase.path, false, nil); err != nil {
				t.Fatalf("Launch: %v", err)
			}
			dump, err := d.Hierarchy(ctx)
			if err != nil {
				t.Fatalf("Hierarchy: %v", err)
			}
			var root struct {
				Attributes map[string]string `json:"attributes"`
			}
			if err := json.Unmarshal([]byte(dump), &root); err != nil {
				t.Fatalf("unmarshal hierarchy: %v", err)
			}
			if got := root.Attributes["sanderling-screen"]; got != testCase.want {
				t.Errorf("sanderling-screen: got %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestWaitForIdle_WaitsForWorkTheActionKickedOff pins the settle the runner
// relies on between acting and observing. WaitForIdle used to return the moment
// <body> existed, which is true before the app has reacted at all: measured on
// the folio wasm build, Compose's accessibility DOM lands ~136 ms after an
// InputText, so the next step read the pre-action text and typed into a field
// it believed was still empty.
func TestWaitForIdle_WaitsForWorkTheActionKickedOff(t *testing.T) {
	const page = `<body><div id="app"></div><script>
	  const root = document.getElementById("app").attachShadow({mode: "open"});
	  root.innerHTML = '<button id="go" style="width:200px;height:80px">go</button><div id="out">pending</div>';
	  root.getElementById("go").addEventListener("click", function () {
	    setTimeout(function () { root.getElementById("out").textContent = "settled"; }, 100);
	  });
	</script></body>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}))
	defer server.Close()

	d := New()
	defer d.Terminate(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := d.Launch(ctx, server.URL, false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := d.Tap(ctx, 40, 40); err != nil {
		t.Fatalf("Tap: %v", err)
	}
	if err := d.WaitForIdle(ctx, time.Second); err != nil {
		t.Fatalf("WaitForIdle: %v", err)
	}

	var shown string
	script := `document.getElementById("app").shadowRoot.getElementById("out").textContent`
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(script, &shown)); err != nil {
		t.Fatalf("read: %v", err)
	}
	if shown != "settled" {
		t.Errorf("observed %q; WaitForIdle returned before the tap's own work landed", shown)
	}
}

// TestWaitForIdle_ReturnsOnABusyPage is the other half: a page that never stops
// mutating (an animation, a polling widget) must not hold the step loop open.
func TestWaitForIdle_ReturnsOnABusyPage(t *testing.T) {
	const page = `<body><div id="tick">0</div><script>
	  let n = 0;
	  setInterval(function () { document.getElementById("tick").textContent = String(++n); }, 15);
	</script></body>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}))
	defer server.Close()

	d := New()
	defer d.Terminate(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := d.Launch(ctx, server.URL, false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	start := time.Now()
	if err := d.WaitForIdle(ctx, time.Second); err != nil {
		t.Fatalf("WaitForIdle: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("WaitForIdle took %s on a busy page; it must return inside its budget", elapsed)
	}
}

// TestWaitForIdle_WaitsOutARouteTransition covers the settle case a mutation
// observer cannot see. A canvas app splices the incoming screen's
// accessibility nodes in when its cross-fade STARTS and drops the outgoing
// screen's when it ends; between those two mutations the DOM is quiet with both
// routes live. Returning there hands the next step a tree naming the screen the
// app is leaving, which on the folio wasm build recorded a submit that had
// landed on Home as still being on the transaction screen: the route gate of an
// action-gated property then skipped the very step the action landed on.
func TestWaitForIdle_WaitsOutARouteTransition(t *testing.T) {
	const page = `<body><div id="app"></div><script>
	  const root = document.getElementById("app").attachShadow({mode: "open"});
	  root.innerHTML = '<button id="go" style="width:200px;height:80px">go</button>' +
	    '<div id="LedgerScreen">ledger</div>';
	  root.getElementById("go").addEventListener("click", function () {
	    const incoming = document.createElement("div");
	    incoming.id = "HomeScreen";
	    incoming.textContent = "home";
	    root.appendChild(incoming);
	    setTimeout(function () { root.getElementById("LedgerScreen").remove(); }, 400);
	  });
	</script></body>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}))
	defer server.Close()

	d := New()
	defer d.Terminate(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := d.Launch(ctx, server.URL, false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := d.Tap(ctx, 40, 40); err != nil {
		t.Fatalf("Tap: %v", err)
	}
	if err := d.WaitForIdle(ctx, 2*time.Second); err != nil {
		t.Fatalf("WaitForIdle: %v", err)
	}

	var live []string
	script := `Array.from(document.getElementById("app").shadowRoot
		.querySelectorAll('[id$="Screen"]')).map(e => e.id)`
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(script, &live)); err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(live) != 1 || live[0] != "HomeScreen" {
		t.Errorf("live screens after the settle = %v, want [HomeScreen]; WaitForIdle "+
			"returned mid-transition, so the next step verifies the outgoing route", live)
	}
}

// TestWaitForIdle_BoundsTheTransitionWait is the other half of the transition
// wait: a page that shows two *Screen ids at rest is not mid-transition, it
// just matches the heuristic, and it must cost one bounded wait rather than the
// whole step budget on every step.
func TestWaitForIdle_BoundsTheTransitionWait(t *testing.T) {
	const page = `<body><div id="HomeScreen">home</div><div id="LedgerScreen">ledger</div></body>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}))
	defer server.Close()

	d := New()
	defer d.Terminate(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := d.Launch(ctx, server.URL, false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	start := time.Now()
	if err := d.WaitForIdle(ctx, 10*time.Second); err != nil {
		t.Fatalf("WaitForIdle: %v", err)
	}
	if elapsed := time.Since(start); elapsed > transitionSettlePeriod+time.Second {
		t.Errorf("WaitForIdle took %s on a page with two resting screens; the "+
			"transition wait must be bounded by %s", elapsed, transitionSettlePeriod)
	}
}

// TestEvaluateExtractors_WaitsOutARouteTransition covers the other sampler. A
// step reads the page twice: the hierarchy dump (which re-fetches while the
// tree looks transitional) and the spec's own extractors in V8. Sampling the
// extractors mid cross-fade reports the route the app is leaving, and an
// action-gated property then skips the one step its action can be judged on:
// on the folio wasm build a double-submit that landed on Home was extracted as
// still being on the transaction screen.
func TestEvaluateExtractors_WaitsOutARouteTransition(t *testing.T) {
	const page = `<body><div id="app"></div><script>
	  const root = document.getElementById("app").attachShadow({mode: "open"});
	  root.innerHTML = '<div id="LedgerScreen">ledger</div>';
	  window.__sanderlingExtractors__ = function () {
	    return {0: Array.from(root.querySelectorAll('[id$="Screen"]')).map(e => e.id).join(",")};
	  };
	  window.startTransition = function () {
	    const incoming = document.createElement("div");
	    incoming.id = "HomeScreen";
	    root.appendChild(incoming);
	    setTimeout(function () { root.getElementById("LedgerScreen").remove(); }, 400);
	  };
	</script></body>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}))
	defer server.Close()

	d := New()
	defer d.Terminate(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := d.Launch(ctx, server.URL, false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(`window.startTransition()`, nil)); err != nil {
		t.Fatalf("start transition: %v", err)
	}

	values, err := d.EvaluateExtractors(ctx)
	if err != nil {
		t.Fatalf("EvaluateExtractors: %v", err)
	}
	if got := string(values[0]); got != `"HomeScreen"` {
		t.Errorf("extractor read %s, want \"HomeScreen\"; the extractors sampled "+
			"mid-transition, so the spec sees the route the app is leaving", got)
	}
}
