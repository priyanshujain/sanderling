//go:build browser

package chrome

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/priyanshujain/sanderling/internal/bundler"
	"github.com/priyanshujain/sanderling/internal/driver"
	"github.com/priyanshujain/sanderling/internal/hierarchy"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

// launchChrome starts a browser on target and returns it with the context every
// later driver call must use. The browser, and the deadline that bounds a call
// against a wedged one, are torn down when the test ends.
//
// A test whose subject is the launch itself (a clearState wipe, a caller
// deadline, a browser outliving its caller's context) builds this by hand: the
// flag, the timeout and the cancel are what it is measuring.
func launchChrome(t *testing.T, target string) (*Driver, context.Context) {
	t.Helper()
	d := New()
	t.Cleanup(func() { _ = d.Terminate(context.Background()) })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	if err := d.Launch(ctx, target, false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	return d, ctx
}

// servePage serves html at a real http origin for the duration of the test. A
// data: URL carries the same markup but has an opaque origin, where storage,
// routing and everything else keyed by origin behaves as it never would in an
// app.
func servePage(t *testing.T, html string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	}))
	t.Cleanup(server.Close)
	return server
}

// testdataServer serves the fixture pages in testdata, each at its own path.
func testdataServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.FileServer(http.Dir("testdata")))
	t.Cleanup(server.Close)
	return server
}

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
	server := servePage(t, page)

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
	d, _ := launchChrome(t, "data:text/html,<body></body>")
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
	d, _ := launchChrome(t, "data:text/html,<body><button id=go>go</button></body>")

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

// TestHierarchy_HintTextNamesAnEditableField covers the attribute visibleLabel
// (internal/verifier/llm.go) reads FIRST for an editable element. Without it a
// web field reached the model named by its CSS class, an identifier no user can
// read, on exactly the channel the label-source experiment varies. The ladder is
// fieldHint's in pkg/spec/src/web-runtime.ts, rung for rung.
func TestHierarchy_HintTextNamesAnEditableField(t *testing.T) {
	const html = `<body>` +
		`<label id="amount-label" for="amount">Amount</label>` +
		`<input id="amount" class="input amount-input" placeholder="0.00" name="amount-field">` +
		`<input id="search" class="input search-input" aria-label="Search" placeholder="Type here" name="q">` +
		`<label id="note-label" for="note"> </label>` +
		`<input id="note" class="input note-input" placeholder="What's this for?" name="note-field">` +
		`<input id="reference" class="input" name="reference-field">` +
		`<input id="unnamed" class="input">` +
		`<input id="agree" class="checkbox" type="checkbox" placeholder="ignored">` +
		`<button id="go" class="button" placeholder="ignored">go</button>` +
		`</body>`
	d, ctx := launchChrome(t, servePage(t, html).URL)
	dump, err := d.Hierarchy(ctx)
	if err != nil {
		t.Fatalf("Hierarchy: %v", err)
	}

	type node struct {
		Attributes map[string]string `json:"attributes"`
		Editable   bool              `json:"editable"`
		Children   []node            `json:"children"`
	}
	var root node
	if err := json.Unmarshal([]byte(dump), &root); err != nil {
		t.Fatalf("unmarshal hierarchy: %v", err)
	}
	fieldByID := map[string]node{}
	var walk func(n node)
	walk = func(n node) {
		if id := n.Attributes["resource-id"]; id != "" {
			fieldByID[id] = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)

	for _, tc := range []struct {
		id       string
		want     string
		editable bool
	}{
		{"search", "Search", true},
		{"amount", "Amount", true},
		{"note", "What's this for?", true},
		{"reference", "reference-field", true},
		{"unnamed", "", true},
		{"agree", "", false},
		{"go", "", false},
	} {
		field := fieldByID[tc.id]
		if field.Attributes["hintText"] != tc.want {
			t.Errorf("%q: hintText = %q, want %q", tc.id, field.Attributes["hintText"], tc.want)
		}
		// visibleLabel reaches the hint only for an element the dump calls
		// editable, so a field named right and marked wrong is still named by
		// its class downstream.
		if field.Editable != tc.editable {
			t.Errorf("%q: editable = %v, want %v", tc.id, field.Editable, tc.editable)
		}
		if tc.want != "" && field.Attributes["hintText"] == field.Attributes["class"] {
			t.Errorf("%q: named by its CSS class %q", tc.id, field.Attributes["class"])
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

	d, ctx := launchChrome(t, "data:text/html,"+html)
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
	d, ctx := launchChrome(t, servePage(t, page).URL)
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
	server := servePage(t, `<body><div id="app">app</div></body>`)

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
			d, ctx := launchChrome(t, server.URL+testCase.path)
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
	d, ctx := launchChrome(t, servePage(t, page).URL)
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
	d, ctx := launchChrome(t, servePage(t, page).URL)
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
//
// The page keeps mutating for 300 ms after the route splice, and the settle
// runs on the timeout production hands it (MinIdleTimeout). Both details are
// load-bearing. A quiet page reaches the transition check immediately, so it
// passes whether the transition window is anchored at the script start or at
// the end of the quiet period; churn is what pushes the check past a
// start-anchored deadline, which then finishes at once with two live screens.
// And a caller timeout below MinIdleTimeout cuts the whole settle off before
// the transition window can be spent, which is the same bug from the other end.
func TestWaitForIdle_WaitsOutARouteTransition(t *testing.T) {
	const page = `<body><div id="app"></div><script>
	  const root = document.getElementById("app").attachShadow({mode: "open"});
	  root.innerHTML = '<button id="go" style="width:200px;height:80px">go</button>' +
	    '<div id="LedgerScreen">ledger</div><div id="spinner">0</div>';
	  root.getElementById("go").addEventListener("click", function () {
	    const incoming = document.createElement("div");
	    incoming.id = "HomeScreen";
	    incoming.textContent = "home";
	    root.appendChild(incoming);
	    let frame = 0;
	    const churn = setInterval(function () {
	      root.getElementById("spinner").textContent = String(++frame);
	    }, 30);
	    setTimeout(function () { clearInterval(churn); }, 300);
	    setTimeout(function () { root.getElementById("LedgerScreen").remove(); }, 1000);
	  });
	</script></body>`
	d, ctx := launchChrome(t, servePage(t, page).URL)
	if err := d.Tap(ctx, 40, 40); err != nil {
		t.Fatalf("Tap: %v", err)
	}
	if err := d.WaitForIdle(ctx, d.MinIdleTimeout()); err != nil {
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
	d, ctx := launchChrome(t, servePage(t, page).URL)
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
	    return {0: {value: Array.from(root.querySelectorAll('[id$="Screen"]')).map(e => e.id).join(",")}};
	  };
	  window.startTransition = function () {
	    const incoming = document.createElement("div");
	    incoming.id = "HomeScreen";
	    root.appendChild(incoming);
	    setTimeout(function () { root.getElementById("LedgerScreen").remove(); }, 400);
	  };
	</script></body>`
	d, ctx := launchChrome(t, servePage(t, page).URL)
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

// TestSetLastAction_ReportsAPageThatCannotTakeIt covers the install the whole
// web path's action-gated properties hang off. A page without the setter is
// reachable: internal/testrun resolves the web runtime from
// node_modules/@sanderling/spec when no sibling checkout is present, and an
// older published runtime does not define it. Guarded as
// `setter && setter(...)`, that page returns undefined and chromedp reports
// success, so every step silently no-ops and every property gated on the last
// action goes vacuously true - a green run that checked nothing.
func TestSetLastAction_ReportsAPageThatCannotTakeIt(t *testing.T) {
	const withSetter = `<body><script>
	  window.__lastActionSeen = null;
	  window.__sanderlingSetLastAction__ = function (value) { window.__lastActionSeen = value; };
	</script></body>`
	const withoutSetter = `<body><div id="app">no sanderling runtime here</div></body>`
	without := servePage(t, withoutSetter)

	d, ctx := launchChrome(t, servePage(t, withSetter).URL)
	action := json.RawMessage(`{"kind":"Tap","on":"id:TxnSubmit"}`)
	if err := d.SetLastAction(ctx, action); err != nil {
		t.Fatalf("SetLastAction on a page that defines the setter: %v", err)
	}
	var seen map[string]string
	if err := chromedp.Run(d.tabCtx,
		chromedp.Evaluate(`window.__lastActionSeen`, &seen)); err != nil {
		t.Fatalf("read installed action: %v", err)
	}
	if seen["on"] != "id:TxnSubmit" {
		t.Errorf("the page received %v, want the action the runner applied", seen)
	}

	if err := d.Launch(ctx, without.URL, false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := d.SetLastAction(ctx, action); err == nil {
		t.Error("SetLastAction reported success on a page with no setter; " +
			"a runtime that cannot take lastAction is indistinguishable from one that did")
	}
}

// TestSetLogs_ReportsAPageThatCannotTakeThem is the same install on the channel
// the log properties hang off. The driver holding a console error changes
// nothing on web: the page's reading of every extractor replaces the host's, so
// unless the entries are put back into the page, noLogcatErrors counts an empty
// array and stays green through a run full of errors.
func TestSetLogs_ReportsAPageThatCannotTakeThem(t *testing.T) {
	const withSetter = `<body><script>
	  window.__logsSeen = null;
	  window.__sanderlingSetLogs__ = function (value) { window.__logsSeen = value; };
	</script></body>`
	const withoutSetter = `<body><div id="app">no sanderling runtime here</div></body>`
	without := servePage(t, withoutSetter)

	d, ctx := launchChrome(t, servePage(t, withSetter).URL)
	logs := json.RawMessage(`[{"unixMillis":1,"level":"E","tag":"console","message":"boom"}]`)
	if err := d.SetLogs(ctx, logs); err != nil {
		t.Fatalf("SetLogs on a page that defines the setter: %v", err)
	}
	var seen []map[string]any
	if err := chromedp.Run(d.tabCtx,
		chromedp.Evaluate(`window.__logsSeen`, &seen)); err != nil {
		t.Fatalf("read installed logs: %v", err)
	}
	if len(seen) != 1 || seen[0]["level"] != "E" || seen[0]["message"] != "boom" {
		t.Errorf("the page received %v, want the error-level entry the driver captured", seen)
	}

	if err := d.Launch(ctx, without.URL, false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := d.SetLogs(ctx, logs); err == nil {
		t.Error("SetLogs reported success on a page with no setter; " +
			"a runtime that cannot take the step's logs is indistinguishable from one that did")
	}
}

// TestEvaluateExtractors_ReportsAMissingTable is the same failure on the other
// sampler. An empty override map is what a spec with no extractors returns, so
// treating a missing table as {} makes "this page has no sanderling runtime"
// read as an ordinary step - and the verifier then judges the run on goja's
// dump-derived values while believing they came from the page.
func TestEvaluateExtractors_ReportsAMissingTable(t *testing.T) {
	const page = `<body><div id="app">no sanderling runtime here</div></body>`
	d, ctx := launchChrome(t, servePage(t, page).URL)
	values, err := d.EvaluateExtractors(ctx)
	if err == nil {
		t.Errorf("EvaluateExtractors returned %v and no error on a page with no "+
			"extractor table; a page that cannot be read must not read as empty", values)
	}
}

// TestEvaluateExtractors_KeepsUndefinedApartFromNull covers the wire the page's
// readings cross. JSON has no undefined, so the web runtime wraps each reading
// in a {value} envelope: written straight into the table, an extractor that
// returned undefined lost its whole index to JSON.stringify and the host kept
// goja's dump-derived reading for it while the rest held the page's. An absent
// value has to arrive as an empty payload, which is what makes the verifier
// record undefined (the value the native host records for the same getter);
// arriving as JSON null would claim the getter returned null.
func TestEvaluateExtractors_KeepsUndefinedApartFromNull(t *testing.T) {
	const page = `<body><script>
	  window.__sanderlingExtractors__ = function () {
	    return {0: {}, 1: {value: null}, 2: {value: {balance: 7}}};
	  };
	</script></body>`
	d, ctx := launchChrome(t, servePage(t, page).URL)

	values, err := d.EvaluateExtractors(ctx)
	if err != nil {
		t.Fatalf("EvaluateExtractors: %v", err)
	}
	if len(values) != 3 {
		t.Fatalf("the page reported 3 readings, %d survived the wire: %v", len(values), values)
	}
	if got := values[0]; len(got) != 0 {
		t.Errorf("the undefined reading arrived as %s, want an empty payload; "+
			"the verifier records anything else as a value the getter never returned", got)
	}
	if got := string(values[1]); got != "null" {
		t.Errorf("the null reading arrived as %s, want null", got)
	}
	if got := string(values[2]); got != `{"balance":7}` {
		t.Errorf("the object reading arrived as %s, want {\"balance\":7}", got)
	}
}

// TestEvaluateExtractors_RejectsAnUnenvelopedReading is the loud failure a page
// running an older @sanderling/spec produces. Its readings are bare values, and
// a bare value is indistinguishable from a reading whose getter returned that
// value, so accepting them silently puts the two engines on different bundles.
func TestEvaluateExtractors_RejectsAnUnenvelopedReading(t *testing.T) {
	const page = `<body><script>
	  window.__sanderlingExtractors__ = function () { return {0: "home"}; };
	</script></body>`
	d, ctx := launchChrome(t, servePage(t, page).URL)

	values, err := d.EvaluateExtractors(ctx)
	if err == nil {
		t.Fatalf("EvaluateExtractors accepted %v from a page whose readings are not "+
			"enveloped; the page and the host are running different bundles", values)
	}
	if !strings.Contains(err.Error(), "different bundles") {
		t.Errorf("EvaluateExtractors failed with %q, want it to name the bundle mismatch", err)
	}
}

// TestHierarchy_CarriesEveryMarkupAttribute covers the data the spec actually
// reads. folio-web's extractors read data-cents, data-account-id and
// data-balance off the elements they find; the dump used to emit a fixed
// standard set, so those values were absent from the goja host and from every
// stored trace, and a selector over them resolved nothing offline.
func TestHierarchy_CarriesEveryMarkupAttribute(t *testing.T) {
	const html = `<body>` +
		`<div id="total-balance" data-cents="125000">$1,250.00</div>` +
		`<div id="card" data-testid="account-card" data-account-id="acct-7" data-balance="4200">Tim</div>` +
		`</body>`

	d, ctx := launchChrome(t, "data:text/html,"+html)
	dump, err := d.Hierarchy(ctx)
	if err != nil {
		t.Fatalf("Hierarchy: %v", err)
	}
	tree, err := hierarchy.Parse(dump)
	if err != nil {
		t.Fatalf("parse hierarchy: %v", err)
	}

	total := tree.Find("id:total-balance")
	if total == nil {
		t.Fatal("total-balance not in the dump")
	}
	if got := total.Attributes["data-cents"]; got != "125000" {
		t.Errorf(`attrs["data-cents"] = %q, want "125000"`, got)
	}
	card := tree.Find(`data-account-id:acct-7`)
	if card == nil {
		t.Fatal("no element resolves by a data attribute the markup carries")
	}
	if got := card.Attributes["data-balance"]; got != "4200" {
		t.Errorf(`attrs["data-balance"] = %q, want "4200"`, got)
	}
	if got := card.Attributes["data-testid"]; got != "account-card" {
		t.Errorf(`attrs["data-testid"] = %q, want "account-card"`, got)
	}
}

// growingPage serves a page whose content starts shorter than one screen and
// grows past it when its first button is tapped, which is what a chat, feed or
// ledger does as a run drives it.
const growingPage = `<body style="margin:0">
<button id="grow" style="height:80px">grow</button>
<div id="status">idle</div>
<div id="rest"></div>
<script>
document.getElementById('grow').addEventListener('click', function() {
  document.getElementById('rest').innerHTML =
    '<div style="height:900px"></div>' +
    '<button id="below" style="height:60px">below</button>';
  document.getElementById('below').addEventListener('click', function() {
    document.getElementById('status').textContent = 'below tapped';
  });
});
</script></body>`

// TestTap_ActuatesAnElementBelowTheLaunchViewport pins the invariant the whole
// web path rests on: an element the driver reports as present and clickable can
// be acted on. The emulated viewport is sized once at launch, so content the
// app adds afterwards lies below it while getBoundingClientRect keeps reporting
// where it is; a click dispatched there is hit-tested to the document root and
// the element never sees it, with no error anywhere.
func TestTap_ActuatesAnElementBelowTheLaunchViewport(t *testing.T) {
	d, ctx := launchChrome(t, servePage(t, growingPage).URL)

	tapByID := func(id string) {
		t.Helper()
		dump, err := d.Hierarchy(ctx)
		if err != nil {
			t.Fatalf("Hierarchy: %v", err)
		}
		tree, err := hierarchy.Parse(dump)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		element := tree.Find("id:" + id)
		if element == nil {
			t.Fatalf("%s is not in the dump", id)
		}
		if !element.Clickable {
			t.Fatalf("%s is not reported clickable", id)
		}
		x, y := element.Bounds.Center()
		if err := d.Tap(ctx, x, y); err != nil {
			t.Fatalf("Tap %s at (%d,%d): %v", id, x, y, err)
		}
	}
	tapByID("grow")
	tapByID("below")

	dump, err := d.Hierarchy(ctx)
	if err != nil {
		t.Fatalf("Hierarchy: %v", err)
	}
	tree, err := hierarchy.Parse(dump)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	status := tree.Find("id:status")
	if status == nil {
		t.Fatal("status is not in the dump")
	}
	if status.Text != "below tapped" {
		t.Errorf(
			"status = %q, want %q: the tap reached no element",
			status.Text,
			"below tapped",
		)
	}
}

// TestTap_ReportsAGestureThatReachesNoElement covers the half of the same bug
// that no scrolling can fix: a page that cannot scroll leaves the point out of
// reach, and the caller has to hear about it rather than read a clean run.
func TestTap_ReportsAGestureThatReachesNoElement(t *testing.T) {
	const page = `<body style="margin:0;overflow:hidden">
<div style="height:80px">top</div>
<div id="rest" style="height:900px;overflow:hidden"></div>
<style>html{overflow:hidden}</style></body>`
	d, ctx := launchChrome(t, servePage(t, page).URL)
	err := d.Tap(ctx, 100, 5000)
	if !errors.Is(err, driver.ErrGestureUndelivered) {
		t.Fatalf(
			"Tap far below an unscrollable page: err = %v, want ErrGestureUndelivered",
			err,
		)
	}
}

// TestTapSelector_ReportsASelectorThatMatchesNothing covers the by-selector
// half of a step that reads as dispatched and did nothing: the node the
// selector names is not on the page, so the click has no target at all.
func TestTapSelector_ReportsASelectorThatMatchesNothing(t *testing.T) {
	const page = `<body style="margin:0">
<button id="present">here</button>
<div id="status">none</div>
<script>
  document.getElementById('present').addEventListener('click', function () {
    document.getElementById('status').textContent = 'present tapped';
  });
</script></body>`
	d, ctx := launchChrome(t, servePage(t, page).URL)

	missCtx, missCancel := context.WithTimeout(ctx, 10*time.Second)
	defer missCancel()
	if err := d.TapSelector(missCtx, "id:absent"); !errors.Is(err, driver.ErrSelectorMatchedNothing) {
		t.Fatalf("TapSelector on an absent element: err = %v, want ErrSelectorMatchedNothing", err)
	}
	if err := d.DoubleTapSelector(missCtx, "id:absent"); !errors.Is(err, driver.ErrSelectorMatchedNothing) {
		t.Fatalf("DoubleTapSelector on an absent element: err = %v, want ErrSelectorMatchedNothing", err)
	}
	if err := d.TapSelector(ctx, "id:present"); err != nil {
		t.Fatalf("TapSelector on the element that is there: %v", err)
	}
	dump, err := d.Hierarchy(ctx)
	if err != nil {
		t.Fatalf("Hierarchy: %v", err)
	}
	tree, err := hierarchy.Parse(dump)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if status := tree.Find("id:status"); status == nil || status.Text != "present tapped" {
		t.Fatalf("status = %+v, want the tap on the present element to have landed", status)
	}
}

const doubleClickPage = `<body style="margin:0">
<div id="target" style="width:200px;height:60px">edit me</div>
<div id="status">none</div>
<script>
  var box = document.getElementById('target');
  var report = document.getElementById('status');
  var clicks = 0;
  box.addEventListener('click', function () { clicks++; });
  box.addEventListener('dblclick', function () {
    report.textContent = 'edited after ' + clicks + ' clicks';
  });
</script></body>`

func doubleClickStatus(t *testing.T, d *Driver, ctx context.Context) string {
	t.Helper()
	dump, err := d.Hierarchy(ctx)
	if err != nil {
		t.Fatalf("Hierarchy: %v", err)
	}
	tree, err := hierarchy.Parse(dump)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	status := tree.Find("id:status")
	if status == nil {
		t.Fatal("status is not in the dump")
	}
	return status.Text
}

// TestDoubleTap_ReachesADoubleClickHandler pins a gesture the web driver had no
// way to deliver. Blink raises dblclick off the click count the second event
// carries, so a pair that both said "first click" arrived as two ordinary
// clicks: every double-click affordance on the web (an editable list row, a
// canvas, a table cell) was unreachable, with no error on any layer.
func TestDoubleTap_ReachesADoubleClickHandler(t *testing.T) {
	d, ctx := launchChrome(t, servePage(t, doubleClickPage).URL)
	if err := d.DoubleTap(ctx, 100, 30); err != nil {
		t.Fatalf("DoubleTap: %v", err)
	}
	if status := doubleClickStatus(t, d, ctx); status != "edited after 2 clicks" {
		t.Errorf(
			"status = %q, want %q: the pair never read as one double click",
			status,
			"edited after 2 clicks",
		)
	}
}

// TestDoubleTapSelector_ReachesADoubleClickHandler covers the same gesture on
// the path the runner takes when the action names its target rather than a
// point.
func TestDoubleTapSelector_ReachesADoubleClickHandler(t *testing.T) {
	d, ctx := launchChrome(t, servePage(t, doubleClickPage).URL)
	if err := d.DoubleTapSelector(ctx, "id:target"); err != nil {
		t.Fatalf("DoubleTapSelector: %v", err)
	}
	if status := doubleClickStatus(t, d, ctx); status != "edited after 2 clicks" {
		t.Errorf(
			"status = %q, want %q: the pair never read as one double click",
			status,
			"edited after 2 clicks",
		)
	}
}

// TestScroll_MovesThePageAndAScrollableContainer covers the verb the runner
// lowers every Scroll action onto. Script-dispatched pointer events are
// untrusted and a browser never scrolls on them, so the web Scroll used to
// leave scrollY and every scrollTop exactly where they were while reporting a
// step that ran. The repeat also pins the distance: a run that scrolls a
// different amount each time explores differently on the same seed.
func TestScroll_MovesThePageAndAScrollableContainer(t *testing.T) {
	d, ctx := launchChrome(t, testdataServer(t).URL+"/gestures.html")

	read := func(expression string) int {
		t.Helper()
		var value int
		if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(expression, &value)); err != nil {
			t.Fatalf("evaluate %s: %v", expression, err)
		}
		return value
	}
	const pageScroll = `Math.round(window.scrollY)`
	const containerScroll = `Math.round(document.getElementById("inner").scrollTop)`

	if before := read(pageScroll); before != 0 {
		t.Fatalf("scrollY before = %d, want 0", before)
	}
	var pageDistances []int
	for range 3 {
		if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(`window.scrollTo(0, 0)`, nil)); err != nil {
			t.Fatalf("reset: %v", err)
		}
		if err := d.Scroll(ctx, 195, 500, 195, 260, 300*time.Millisecond); err != nil {
			t.Fatalf("Scroll: %v", err)
		}
		pageDistances = append(pageDistances, read(pageScroll))
	}
	if pageDistances[0] <= 0 {
		t.Errorf(
			"scrollY after = %d, want > 0: the page never scrolled",
			pageDistances[0],
		)
	}
	if pageDistances[0] != pageDistances[1] ||
		pageDistances[1] != pageDistances[2] {
		t.Errorf(
			"scrollY over three identical scrolls = %v, want one distance",
			pageDistances,
		)
	}

	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(`window.scrollTo(0, 0)`, nil)); err != nil {
		t.Fatalf("reset: %v", err)
	}
	containerY := read(
		`Math.round(document.getElementById("inner").getBoundingClientRect().top + 100)`,
	)
	if before := read(containerScroll); before != 0 {
		t.Fatalf("container scrollTop before = %d, want 0", before)
	}
	if err := d.Scroll(ctx, 195, containerY, 195, containerY-120, 300*time.Millisecond); err != nil {
		t.Fatalf("Scroll in the container: %v", err)
	}
	if after := read(containerScroll); after <= 0 {
		t.Errorf(
			"container scrollTop after = %d, want > 0: the container never scrolled",
			after,
		)
	}
	if after := read(pageScroll); after != 0 {
		t.Errorf(
			"scrollY = %d, want 0: a scroll inside a container moved the page instead",
			after,
		)
	}
}

// TestScroll_ReportsAGestureThatReachesNoElement keeps the scroll path on the
// same footing as the tap path: a page that cannot bring the point into the
// viewport has to say the gesture reached nothing rather than read as a step
// that scrolled.
func TestScroll_ReportsAGestureThatReachesNoElement(t *testing.T) {
	const page = `<body style="margin:0;overflow:hidden">
<div style="height:80px">top</div>
<style>html{overflow:hidden}</style></body>`
	d, ctx := launchChrome(t, servePage(t, page).URL)
	err := d.Scroll(ctx, 100, 5000, 100, 4800, 300*time.Millisecond)
	if !errors.Is(err, driver.ErrGestureUndelivered) {
		t.Fatalf(
			"Scroll far below an unscrollable page: err = %v, want ErrGestureUndelivered",
			err,
		)
	}
}

// TestSwipe_DeliversATrustedDragToARowHandler covers what the manual says
// sideways swipes are for. Script-dispatched pointer events carry isTrusted
// false, which is the mark of a gesture the browser never routed: nothing in
// the page's own input pipeline saw it, so scrolling, touch-action and any
// handler that filters on trust behave as if the finger never moved.
func TestSwipe_DeliversATrustedDragToARowHandler(t *testing.T) {
	d, ctx := launchChrome(t, testdataServer(t).URL+"/gestures.html")
	if err := d.Swipe(ctx, 300, 40, 100, 40, 300*time.Millisecond); err != nil {
		t.Fatalf("Swipe: %v", err)
	}
	var status string
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(
		`document.getElementById("status").textContent`, &status)); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if status != "dismissed left trusted" {
		t.Errorf("row status = %q, want %q", status, "dismissed left trusted")
	}
}

// TestInstallBundle_RefusesARuntimeThatDeclaresADifferentActionEncoding covers
// the pairing that voided a whole campaign: a spec bundled by an older
// @sanderling/spec, run by this binary. internal/testrun resolves the web
// runtime from whatever the spec's project has installed, so the page's picker
// and the runner that dispatches its actions can encode a gesture differently.
// Neither half fails: every action dispatches successfully and executes the
// wrong gesture, and the run reports a full step count of results that mean
// nothing.
func TestInstallBundle_RefusesARuntimeThatDeclaresADifferentActionEncoding(t *testing.T) {
	d, ctx := launchChrome(t, servePage(t, `<body><div id="app">app</div></body>`).URL)

	const legacyRuntime = `window.__sanderlingNextAction__ = function () { return null; };`
	if err := d.InstallBundle(ctx, []byte(legacyRuntime)); err == nil {
		t.Error("InstallBundle accepted a runtime that declares no action encoding; " +
			"the run would dispatch every action successfully and execute the wrong gesture")
	} else if !strings.Contains(err.Error(), verifier.ActionWireContract) {
		t.Errorf("InstallBundle error %q does not name the encoding this binary implements", err)
	}

	currentRuntime := legacyRuntime +
		`window.__sanderlingActionEncoding__ = ` + strconv.Quote(verifier.ActionWireContract) + `;`
	if err := d.InstallBundle(ctx, []byte(currentRuntime)); err != nil {
		t.Fatalf("InstallBundle rejected a runtime on this binary's encoding: %v", err)
	}
}

// TestInstallBundle_AcceptsTheWebRuntimeThisCheckoutShips is the other half of
// the gate: the encoding pkg/spec/src/web-runtime.ts declares has to be the one
// this binary decodes, or every web run refuses to start.
func TestInstallBundle_AcceptsTheWebRuntimeThisCheckoutShips(t *testing.T) {
	directory := t.TempDir()
	specPath := filepath.Join(directory, "spec.ts")
	const spec = `
import { actions, Wait } from "@sanderling/spec";
export const actionsRoot = actions(Wait({ durationMillis: 1 }));
export const properties = {};
`
	if err := os.WriteFile(specPath, []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	apiPath, err := filepath.Abs("../../../pkg/spec/src/index.ts")
	if err != nil {
		t.Fatal(err)
	}
	runtimePath, err := filepath.Abs("../../../pkg/spec/src/web-runtime.ts")
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := bundler.BundleWeb(bundler.WebOptions{
		EntryFile:      specPath,
		WebRuntimeFile: runtimePath,
		Aliases:        map[string]string{"@sanderling/spec": apiPath},
	})
	if err != nil {
		t.Fatalf("BundleWeb: %v", err)
	}

	d, ctx := launchChrome(t, servePage(t, `<body><div id="app">app</div></body>`).URL)
	if err := d.InstallBundle(ctx, bundle.JavaScript); err != nil {
		t.Fatalf("InstallBundle rejected this checkout's own web runtime: %v", err)
	}
}

// TestInstallBundle_AcceptsAPageThatInstallsNoPicker pins the other half of the
// rule Verifier.checkActionEncoding states: a bundle that installs no picker
// generates no actions, so it has no encoding to disagree about and demanding a
// declaration from it would refuse a spec that was never going to dispatch.
func TestInstallBundle_AcceptsAPageThatInstallsNoPicker(t *testing.T) {
	d, ctx := launchChrome(t, servePage(t, `<body><div id="app">app</div></body>`).URL)

	const pickerFree = `window.__sanderlingBundleCheck__ = true;`
	if err := d.InstallBundle(ctx, []byte(pickerFree)); err != nil {
		t.Fatalf("InstallBundle refused a bundle that generates no actions: %v", err)
	}
}

// TestMetrics_AFailedRoundTripIsNotAPageWithoutTheAPI pins the distinction the
// zero-and-nil return erased. performance.memory is absent on plenty of pages
// and zero is the honest answer there, so a round trip that never happened has
// to be an error or a run records a memory reading it never took.
func TestMetrics_AFailedRoundTripIsNotAPageWithoutTheAPI(t *testing.T) {
	d, ctx := launchChrome(t, servePage(t, `<body><div id="app">app</div></body>`).URL)
	if _, err := d.Metrics(ctx, ""); err != nil {
		t.Fatalf("Metrics on a live page: %v", err)
	}

	if err := d.Terminate(context.Background()); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	metrics, err := d.Metrics(ctx, "")
	if err == nil {
		t.Errorf("Metrics returned %+v and a nil error after the tab was gone; "+
			"the run cannot tell that from a page with no performance.memory", metrics)
	}
}

// CDP exposes no per-page CPU, so every web step used to record a zero the
// sampler never took, which is the same answer an idle app gives.
func TestMetrics_ReportsNoCPUSampleRatherThanZero(t *testing.T) {
	server := servePage(t, `<body><div id="app">app</div></body>`)
	d, ctx := launchChrome(t, server.URL)

	metrics, err := d.Metrics(ctx, "")
	if err != nil {
		t.Fatalf("Metrics: %v", err)
	}
	if metrics.CPUPercent != nil {
		t.Errorf("Metrics reported CPUPercent %v; chrome samples no CPU, and a "+
			"run cannot tell that reading from an app using none", *metrics.CPUPercent)
	}
}
