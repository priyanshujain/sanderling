// Package chrome implements the device driver for web targets by driving Chrome over the DevTools protocol.
package chrome

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/priyanshujain/sanderling/internal/driver"
)

// Driver implements DeviceDriver via chromedp for web platform testing.
type Driver struct {
	allocCtx    context.Context
	allocCancel context.CancelFunc
	tabCtx      context.Context
	tabCancel   context.CancelFunc

	logsMu sync.Mutex
	logs   []driver.LogEntry

	navigationsMu sync.Mutex
	navigations   []driver.Navigation

	// pickerState is the seeded picker's draw position, held here rather than
	// in the page: a navigation replaces the page's runtime, and a runtime that
	// starts over restarts the seed's stream at its first draw.
	pickerState string
}

// New creates a new ChromeDriver. Call Terminate when done.
func New() *Driver {
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", true),
			chromedp.Flag("disable-gpu", true),
			// Chrome refuses to fall back to the SwiftShader WebGL backend
			// without this flag, so with --disable-gpu a canvas app (Compose
			// for Web, Flutter web, anything on WebGL) gets a null context and
			// paints nothing: black screenshots and an empty accessibility DOM.
			chromedp.Flag("enable-unsafe-swiftshader", true),
			chromedp.NoSandbox,
			// CI runners give Chrome a tiny /dev/shm; without this the browser
			// process hangs on startup and never reports its DevTools socket.
			chromedp.Flag("disable-dev-shm-usage", true),
			// Cold-starting Chrome on a loaded CI runner can take longer than the
			// 20s default to print its DevTools websocket URL; give it more room
			// so launch does not flake with "websocket url timeout reached".
			chromedp.WSURLReadTimeout(60*time.Second),
		)...,
	)
	tabCtx, tabCancel := chromedp.NewContext(allocCtx)

	d := &Driver{
		allocCtx:    allocCtx,
		allocCancel: allocCancel,
		tabCtx:      tabCtx,
		tabCancel:   tabCancel,
	}

	chromedp.ListenTarget(tabCtx, func(ev any) {
		e, ok := ev.(*runtime.EventConsoleAPICalled)
		if !ok {
			return
		}
		var parts []string
		for _, arg := range e.Args {
			// An object argument, which is what console.error(err) passes,
			// carries no value at all: CDP sends a description instead. Reading
			// only the value logged those calls with an empty message, so the
			// entry named a level and nothing a reader could act on.
			if arg.Value == nil {
				if arg.Description != "" {
					parts = append(parts, arg.Description)
				}
				continue
			}
			var s string
			if err := json.Unmarshal(arg.Value, &s); err == nil {
				parts = append(parts, s)
			} else {
				parts = append(parts, string(arg.Value))
			}
		}
		d.logsMu.Lock()
		d.logs = append(d.logs, driver.LogEntry{
			UnixMillis: int64(e.Timestamp.Time().UnixMilli()),
			Level:      consoleLevel(e.Type),
			Tag:        "console",
			Message:    strings.Join(parts, " "),
		})
		d.logsMu.Unlock()
	})

	chromedp.ListenTarget(tabCtx, func(ev any) {
		e, ok := ev.(*page.EventFrameNavigated)
		if !ok || e.Frame == nil || e.Frame.ParentID != "" {
			return
		}
		d.navigationsMu.Lock()
		d.navigations = append(d.navigations, driver.Navigation{
			URL:        e.Frame.URL,
			UnixMillis: time.Now().UnixMilli(),
		})
		d.navigationsMu.Unlock()
	})

	return d
}

func (d *Driver) Launch(ctx context.Context, bundleID string, clearState bool, _ map[string]string) error {
	// Allocate the browser against the driver's own context before anything
	// caller-bound runs. chromedp starts Chrome under whichever context first
	// calls Run, so allocating under a caller deadline would tie the browser
	// process to this one call and kill it the moment Launch returns.
	if err := chromedp.Run(d.tabCtx); err != nil {
		return err
	}
	// Everything after allocation goes through runCtx, so a caller deadline or
	// a SIGTERM aborts a launch that would otherwise wait forever on a target
	// that accepts the connection and never answers.
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	if clearState {
		if err := d.clearState(runCtx, bundleID); err != nil {
			return err
		}
	}
	if err := chromedp.Run(runCtx, chromedp.Navigate(bundleID)); err != nil {
		return err
	}
	// After navigation, read CSS custom properties --frame-w / --frame-h (common
	// mobile-frame convention) so screenshots fit the app without grey borders.
	// Falls back to the body scroll dimensions if the properties are absent.
	var dims [2]int64
	if err := chromedp.Run(runCtx, chromedp.Evaluate(`
		(function() {
			const s = getComputedStyle(document.documentElement);
			const pw = parseInt(s.getPropertyValue('--frame-w'), 10);
			const ph = parseInt(s.getPropertyValue('--frame-h'), 10);
			const w = isNaN(pw) ? document.body.scrollWidth : pw;
			const h = isNaN(ph) ? document.body.scrollHeight : ph;
			return [w, h];
		})()`, &dims)); err == nil && dims[0] > 0 && dims[1] > 0 {
		_ = chromedp.Run(runCtx, chromedp.EmulateViewport(dims[0], dims[1]))
	}
	// The opening navigation is the harness arriving, not the app navigating.
	_, _ = d.Navigations(ctx)
	return nil
}

// Navigations returns the document-replacing main-frame navigations seen since
// the last call and forgets them. Each one replaced the page's runtime, which
// is what separates "the app reloaded" from "the picker repeated itself".
func (d *Driver) Navigations(context.Context) ([]driver.Navigation, error) {
	d.navigationsMu.Lock()
	defer d.navigationsMu.Unlock()
	drained := d.navigations
	d.navigations = nil
	return drained, nil
}

// clearState wipes the target's stored data before the application loads.
// Script cannot do it: the tab still sits on about:blank, whose opaque origin
// denies storage access, so `localStorage.clear()` throws SecurityError and
// every web run dies at launch. The Storage domain clears by origin instead,
// which needs no navigation. sessionStorage is per-tab and outside that
// domain's reach; it only survives when a relaunch reuses a tab already on
// the target origin, which is the one case where script can reach it.
func (d *Driver) clearState(runCtx context.Context, bundleID string) error {
	if err := chromedp.Run(runCtx, network.ClearBrowserCookies()); err != nil {
		return fmt.Errorf("clear cookies: %w", err)
	}
	origin := securityOrigin(bundleID)
	if origin == "" {
		return nil
	}
	clearForOrigin := storage.ClearDataForOrigin(origin, string(storage.TypeAll))
	if err := chromedp.Run(runCtx, clearForOrigin); err != nil {
		return fmt.Errorf("clear storage for %s: %w", origin, err)
	}
	script := fmt.Sprintf(
		`location.origin === %q && (sessionStorage.clear(), true)`, origin)
	return chromedp.Run(runCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, exception, err := runtime.Evaluate(script).Do(ctx)
		if err != nil {
			return fmt.Errorf("clear session storage: %w", err)
		}
		if exception != nil {
			return fmt.Errorf("clear session storage: %s", exceptionMessage(exception))
		}
		return nil
	}))
}

// securityOrigin returns the scheme://host[:port] the Storage domain keys data
// by, or "" for a target that has no such origin (data:, file:, about:blank),
// where there is no per-origin storage to clear.
func securityOrigin(bundleID string) string {
	parsed, err := url.Parse(bundleID)
	if err != nil || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// exceptionMessage renders a page exception for an error string. The
// description carries the actual message ("SecurityError: Failed to read the
// 'localStorage' property..."); Text alone is the useless "Uncaught".
func exceptionMessage(exception *runtime.ExceptionDetails) string {
	if exception == nil {
		return ""
	}
	if exception.Exception != nil && exception.Exception.Description != "" {
		return exception.Exception.Description
	}
	return exception.Text
}

func (d *Driver) Terminate(_ context.Context) error {
	d.tabCancel()
	d.allocCancel()
	return nil
}

// pointInViewScript scrolls a point the caller took from the hierarchy back
// inside the viewport and reports where to dispatch at, plus whether anything
// is there to receive it.
//
// The emulated viewport is sized once at launch, but getBoundingClientRect goes
// on reporting elements the growing document has pushed below it, so the two
// disagree the moment an app adds content. Input coordinates are
// viewport-relative: a click below the fold is hit-tested to the document root,
// which delivers it to <html> and never to the element the caller named. No
// error is raised on any layer, so the step reads as an action that landed and
// changed nothing.
//
// Only a point outside the viewport is moved, so a gesture that already had a
// reachable target dispatches exactly where it did before.
const pointInViewScript = `
(function(x, y) {
  const root = document.scrollingElement || document.documentElement;
  let shiftX = 0, shiftY = 0;
  if (x < 0 || x >= window.innerWidth) shiftX = Math.round(x - window.innerWidth / 2);
  if (y < 0 || y >= window.innerHeight) shiftY = Math.round(y - window.innerHeight / 2);
  if (shiftX || shiftY) {
    const fromX = root.scrollLeft, fromY = root.scrollTop;
    root.scrollLeft = fromX + shiftX;
    root.scrollTop = fromY + shiftY;
    shiftX = root.scrollLeft - fromX;
    shiftY = root.scrollTop - fromY;
  }
  const atX = x - shiftX, atY = y - shiftY;
  return [atX, atY, document.elementFromPoint(atX, atY) ? 1 : 0];
})(%d, %d)`

// pointInView returns the point to dispatch a gesture at for the point the
// caller named, having scrolled it into view. It fails with
// driver.ErrGestureUndelivered when no scroll can put an element under it.
func pointInView(runCtx context.Context, x, y int) (int, int, error) {
	var point [3]int
	script := fmt.Sprintf(pointInViewScript, x, y)
	if err := chromedp.Run(runCtx, chromedp.Evaluate(script, &point)); err != nil {
		return 0, 0, err
	}
	if point[2] == 0 {
		return 0, 0, fmt.Errorf(
			"%w: (%d,%d)",
			driver.ErrGestureUndelivered,
			x,
			y,
		)
	}
	return point[0], point[1], nil
}

func (d *Driver) Tap(ctx context.Context, x, y int) error {
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	atX, atY, err := pointInView(runCtx, x, y)
	if err != nil {
		return err
	}
	return chromedp.Run(runCtx,
		chromedp.MouseClickXY(float64(atX), float64(atY)),
	)
}

// requireSelectorMatch reports driver.ErrSelectorMatchedNothing when the
// selector names no node on the page right now. chromedp.Click waits instead,
// so without this the caller hears a deadline (or nothing at all) for an action
// that had no target.
func requireSelectorMatch(runCtx context.Context, target, selector string) error {
	var nodes []*cdp.Node
	if err := chromedp.Run(runCtx,
		chromedp.Nodes(target, &nodes, chromedp.BySearch, chromedp.AtLeast(0)),
	); err != nil {
		return err
	}
	if len(nodes) == 0 {
		return fmt.Errorf("%w: %q", driver.ErrSelectorMatchedNothing, selector)
	}
	return nil
}

func (d *Driver) TapSelector(ctx context.Context, selector string) error {
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	target, isXPath, err := TranslateStringSelector(selector)
	if err != nil {
		// Fall back to passing the string straight through; chromedp will
		// reject it loudly if it isn't a valid CSS selector.
		target = selector
	}
	if err := requireSelectorMatch(runCtx, target, selector); err != nil {
		return err
	}
	if isXPath {
		return chromedp.Run(runCtx, chromedp.Click(target, chromedp.NodeVisible, chromedp.BySearch))
	}
	return chromedp.Run(runCtx, chromedp.Click(target, chromedp.NodeVisible))
}

// doubleTapGap is the inter-tap delay for DoubleTap: short enough to land both
// events inside a sub-100 ms race window. The browser has no single double-tap
// primitive, so the gesture is two taps with this gap.
const doubleTapGap = 50 * time.Millisecond

// DoubleTap resolves the point once and dispatches both taps there: resolving
// per tap would scroll the second one away from the element the first hit.
func (d *Driver) DoubleTap(ctx context.Context, x, y int) error {
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	atX, atY, err := pointInView(runCtx, x, y)
	if err != nil {
		return err
	}
	return webDoubleTap(ctx, func(clickCount int) error {
		return chromedp.Run(
			runCtx,
			chromedp.MouseClickXY(
				float64(atX),
				float64(atY),
				chromedp.ClickCount(clickCount),
			),
		)
	})
}

func (d *Driver) DoubleTapSelector(ctx context.Context, selector string) error {
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	target, isXPath, err := TranslateStringSelector(selector)
	if err != nil {
		target = selector
	}
	if err := requireSelectorMatch(runCtx, target, selector); err != nil {
		return err
	}
	options := []chromedp.QueryOption{chromedp.NodeVisible}
	if isXPath {
		options = append(options, chromedp.BySearch)
	}
	return webDoubleTap(ctx, func(clickCount int) error {
		if clickCount < 2 {
			return chromedp.Run(runCtx, chromedp.Click(target, options...))
		}
		return chromedp.Run(runCtx, chromedp.DoubleClick(target, options...))
	})
}

// webDoubleTap dispatches the pair a browser reads as one double click. Blink
// raises dblclick off the click count the second event carries, so two taps
// that both say "first click" arrive at a dblclick handler as two ordinary
// clicks and the gesture never happens at all.
func webDoubleTap(ctx context.Context, tap func(clickCount int) error) error {
	if err := tap(1); err != nil {
		return err
	}
	timer := time.NewTimer(doubleTapGap)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	return tap(2)
}

func (d *Driver) InputText(callerCtx context.Context, text string) error {
	runCtx, cancel := d.runCtx(callerCtx)
	defer cancel()
	return chromedp.Run(runCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			if err := selectFocusedText(ctx); err != nil {
				return err
			}
			return input.InsertText(text).Do(ctx)
		}),
	)
}

// selectAllScript selects everything in the focused field so the InsertText
// that follows replaces rather than appends.
//
// document.activeElement stops at a shadow boundary: it names the HOST, not the
// focused node inside. Compose for Web focuses a hidden <input> inside the
// shadow root it mounts, so the host answer has no select() and the selection
// never happened - every InputText appended to the last one, and a fuzzer that
// types into the same field twice built up garbage it could never clear.
// Descending activeElement through each shadow root finds the real field.
const selectAllScript = `
	(function() {
		let el = document.activeElement;
		while (el && el.shadowRoot && el.shadowRoot.activeElement) {
			el = el.shadowRoot.activeElement;
		}
		if (el && typeof el.select === 'function') el.select();
	})()`

func selectFocusedText(ctx context.Context) error {
	return chromedp.Evaluate(selectAllScript, nil).Do(ctx)
}

// ReplacesTextOnInput reports that InputText replaces existing content via
// select-all, so the runner skips its pre-erase.
func (d *Driver) ReplacesTextOnInput() bool {
	return true
}

// EraseText clears the focused field. InputText above already replaces via
// select-all, so the character count is not needed to bound the deletion.
func (d *Driver) EraseText(callerCtx context.Context, _ int) error {
	runCtx, cancel := d.runCtx(callerCtx)
	defer cancel()
	return chromedp.Run(runCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			if err := selectFocusedText(ctx); err != nil {
				return err
			}
			return input.InsertText("").Do(ctx)
		}),
	)
}

// Swipe drags a finger across the page as a trusted touch stream. Events
// synthesized in the page carry isTrusted false: they reach a handler that
// happens to listen, but never enter the input pipeline that scrolls, honours
// touch-action or resolves a gesture.
func (d *Driver) Swipe(ctx context.Context, fromX, fromY, toX, toY int, duration time.Duration) error {
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	atX, atY, err := pointInView(runCtx, fromX, fromY)
	if err != nil {
		return err
	}
	toX, toY = toX-(fromX-atX), toY-(fromY-atY)
	steps := max(int(duration.Milliseconds())/16, 1)
	actions := []chromedp.Action{touchAt(input.TouchStart, atX, atY)}
	for i := 1; i <= steps; i++ {
		actions = append(actions, touchAt(input.TouchMove,
			atX+(toX-atX)*i/steps, atY+(toY-atY)*i/steps))
	}
	actions = append(
		actions,
		input.DispatchTouchEvent(input.TouchEnd, []*input.TouchPoint{}),
	)
	return chromedp.Run(runCtx, actions...)
}

func touchAt(kind input.TouchType, x, y int) *input.DispatchTouchEventParams {
	return input.DispatchTouchEvent(
		kind,
		[]*input.TouchPoint{{X: float64(x), Y: float64(y)}},
	)
}

// Scroll moves the content under the point with a trusted wheel, which is how a
// browser scrolls.
//
// A finger drag scrolls too, but it ends in a fling whose distance follows the
// release velocity: five identical 240 px drags moved the page 354 to 616 px,
// so two runs of one seed would explore different screens. A wheel delta lands
// exactly, and chains from the element under the point out to its scrollable
// ancestors, which is what scrolling a named container means. The drag stays as
// Swipe, the verb for the gestures only a finger reaches.
func (d *Driver) Scroll(
	ctx context.Context,
	fromX, fromY, toX, toY int,
	_ time.Duration,
) error {
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	atX, atY, err := pointInView(runCtx, fromX, fromY)
	if err != nil {
		return err
	}
	wheel := input.DispatchMouseEvent(input.MouseWheel, float64(atX), float64(atY)).
		WithDeltaX(float64(fromX - toX)).
		WithDeltaY(float64(fromY - toY))
	// The wheel is applied off the CDP round trip, so without this the next
	// read races it: a step could observe the page before its own scroll, and
	// the pending scroll then lands during the following one.
	return chromedp.Run(runCtx, wheel, chromedp.Evaluate(
		`new Promise(done => requestAnimationFrame(() => requestAnimationFrame(done)))`,
		nil,
		awaitPromise,
	))
}

func (d *Driver) PressKey(ctx context.Context, key string) error {
	k, ok := keyMap[key]
	if !ok {
		return fmt.Errorf("unsupported key: %q", key)
	}
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	return chromedp.Run(runCtx, chromedp.KeyEvent(k))
}

func (d *Driver) LongPress(ctx context.Context, x, y int) error {
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	x, y, err := pointInView(runCtx, x, y)
	if err != nil {
		return err
	}
	script := fmt.Sprintf(`
(function() {
  const el = document.elementFromPoint(%d, %d);
  if (!el) return;
  el.dispatchEvent(new PointerEvent('pointerdown', {clientX: %d, clientY: %d, bubbles: true}));
  setTimeout(function() {
    el.dispatchEvent(new PointerEvent('pointerup', {clientX: %d, clientY: %d, bubbles: true}));
  }, 600);
})();`,
		x, y,
		x, y,
		x, y,
	)
	return chromedp.Run(runCtx, chromedp.Evaluate(script, nil))
}

// keyMap covers the keys web specs may emit (enter/tab/escape/arrows).
// "back"/"home" are intentionally absent: backspace/NUL have no navigation
// semantics in a browser, and the V8 action mix already excludes them.
var keyMap = map[string]string{
	"enter":  kb.Enter,
	"tab":    kb.Tab,
	"escape": kb.Escape,
	"up":     kb.ArrowUp,
	"down":   kb.ArrowDown,
	"left":   kb.ArrowLeft,
	"right":  kb.ArrowRight,
}

func (d *Driver) Hierarchy(ctx context.Context) (string, error) {
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	script := `
(function() {
  // Hash first (a HashRouter names the screen there), then the pathname, which
  // is where a path-routed SPA keeps it. Reporting '/' for every step of a
  // BrowserRouter app made every screen look like the same screen.
  const route = window.location.hash.replace(/^#/, '').split('?')[0] ||
    window.location.pathname || '/';
  // clickable and editable are resolved through the SAME selector sets
  // pkg/spec/src/web-runtime.ts uses, so the goja host (which reads this dump)
  // and the V8 host (which reads the DOM directly) cannot mean different things
  // by one fact on one platform. Testing el.onclick instead made every React
  // root a full-viewport tap target here and nowhere else.
  const NON_TEXT_INPUT_TYPES =
    ['button','submit','checkbox','radio','range','color','file','image','reset'];
  // The disabled property belongs to real form controls only, so it reads
  // undefined on the role-based controls the tappable set now covers, and every
  // one of them looked enabled however plainly it was marked otherwise.
  // isEnabled in pkg/spec/src/web-runtime.ts answers the same two ways.
  function isEnabled(el) {
    if (el.disabled) return false;
    return el.getAttribute('aria-disabled') !== 'true';
  }
  function isEditableElement(el) {
    if (el.isContentEditable) return true;
    const tag = el.tagName.toLowerCase();
    if (tag === 'textarea') return true;
    if (tag === 'input') return !NON_TEXT_INPUT_TYPES.includes((el.type || '').toLowerCase());
    return false;
  }
  // An editable field's own text is the transient typed value; its hint names
  // its purpose, which is the rung visibleLabel (internal/verifier/llm.go) reads
  // first for such an element. Without it a web field reached the model named by
  // its CSS class, an identifier no user can read. Same ladder as fieldHint in
  // pkg/spec/src/web-runtime.ts, so one field is named one way on both hosts.
  function fieldHint(el) {
    if (!isEditableElement(el)) return '';
    const ariaLabel = el.getAttribute('aria-label');
    if (ariaLabel) return ariaLabel;
    for (const label of el.labels || []) {
      const text = (label.textContent || '').trim();
      if (text) return text;
    }
    const placeholder = el.getAttribute('placeholder');
    if (placeholder) return placeholder;
    return el.getAttribute('name') || '';
  }
  // Shadow roots are part of the page a user sees, so they are part of the page
  // we enumerate. Compose for Web mounts its canvas AND its accessibility tree
  // inside a shadow root on the mount element, so a light-DOM-only walk reports
  // four nodes for a whole app and offers no action on any of them.
  function deepQuery(sel) {
    const out = [];
    const visit = (root) => {
      for (const el of root.querySelectorAll(sel)) out.push(el);
      for (const el of root.querySelectorAll('*')) if (el.shadowRoot) visit(el.shadowRoot);
    };
    visit(document);
    return out;
  }
  const TAPPABLE_ROLES = [
    'button', 'link', 'checkbox', 'radio', 'switch', 'tab', 'option',
    'menuitem', 'menuitemcheckbox', 'menuitemradio', 'treeitem'];
  const clickableSet = new Set(deepQuery(
    'a, button, input, select, textarea, ' +
    TAPPABLE_ROLES.map(role => '[role="' + role + '"]').join(', ') +
    ', [onclick]'));
  const editableSet = new Set(deepQuery(
    'input, textarea, [contenteditable]').filter(isEditableElement));
  // Descended once for the whole dump, for the reason selectAllScript above
  // descends: document.activeElement names the shadow host, so a Compose for
  // Web app reported focus on its mount element and never on the field.
  let focusedElement = document.activeElement;
  while (focusedElement && focusedElement.shadowRoot && focusedElement.shadowRoot.activeElement) {
    focusedElement = focusedElement.shadowRoot.activeElement;
  }
  // Descending is still not enough on Compose for Web: it takes keystrokes on a
  // 1px transparent input pinned to the caret, and that input is a SIBLING of
  // the accessibility tree rather than a node in it. DOM focus therefore never
  // reaches the semantics element carrying the test tag, so confirmFocus in
  // internal/runner/runner.go saw an unnamed element hold focus after every
  // focus tap and refused to type. Compose declares the caret's box in these
  // custom properties, which the input inherits from the container that
  // positions it, so the field being typed into is the innermost editable box
  // that caret sits in.
  const CARET_ORIGIN_PROPERTY = '--compose-internal-web-backing-input-left';
  function fieldBehindTheCaret(caretInput) {
    if (!caretInput || caretInput.tagName !== 'INPUT') return null;
    if (!getComputedStyle(caretInput).getPropertyValue(CARET_ORIGIN_PROPERTY).trim()) return null;
    const caret = caretInput.getBoundingClientRect();
    const x = (caret.left + caret.right) / 2;
    const y = (caret.top + caret.bottom) / 2;
    let field = null;
    let fieldArea = Infinity;
    for (const candidate of editableSet) {
      if (candidate === caretInput) continue;
      const box = candidate.getBoundingClientRect();
      const area = box.width * box.height;
      if (area <= 0 || area >= fieldArea) continue;
      if (x < box.left || x > box.right || y < box.top || y > box.bottom) continue;
      field = candidate;
      fieldArea = area;
    }
    return field;
  }
  focusedElement = fieldBehindTheCaret(focusedElement) || focusedElement;
  function buildTree(el, isRoot) {
    const rect = el.getBoundingClientRect();
    // Every attribute the markup wrote, keyed as written, which is what attrs
    // means on the native hosts and what rawAttributes in
    // pkg/spec/src/web-runtime.ts already gives the page-side handle. Emitting
    // only the standard set left a spec's data-* reads (folio-web's data-cents,
    // data-account-id, data-balance) undefined on the goja host and absent from
    // the trace, so an offline replay of the same step could not see them at
    // all. The derived keys below overwrite anything of the same name.
    const attrs = {};
    for (const attribute of el.attributes || []) {
      attrs[attribute.name] = attribute.value;
    }
    const bounds = '[' + Math.round(rect.left) + ',' + Math.round(rect.top) + ',' +
      Math.round(rect.right) + ',' + Math.round(rect.bottom) + ']';
    if (rect.width > 0 || rect.height > 0) attrs.bounds = bounds;
    const text = (el.textContent || '').trim().slice(0, 200);
    if (text) attrs.text = text;
    if (el.id) attrs['resource-id'] = el.id;
    // The V8 host names a target by data-testid (IDENTITY_KEYS in
    // pkg/spec/src/web-runtime.ts) and TapSelector translates the selector into
    // a CSS attribute match, so a dump without this attribute leaves the goja
    // host unable to resolve a target the other two resolve fine.
    const testid = el.getAttribute('data-testid');
    if (testid) attrs['data-testid'] = testid;
    const label = el.getAttribute('aria-label') || el.getAttribute('alt') || el.getAttribute('title') || '';
    if (label) attrs['content-desc'] = label;
    const tag = (el.tagName || '').toLowerCase();
    if (tag) attrs['tag'] = tag;
    if (el.className && typeof el.className === 'string' && el.className.trim()) {
      attrs['class'] = el.className.trim();
    }
    const hint = fieldHint(el);
    if (hint) attrs['hintText'] = hint;
    // The goja host reads scrollable off this attribute (internal/verifier
    // worker.go targets). Without it every web element looks unscrollable there,
    // so the goja-side enumeration offers no scroll while the V8 picker, which
    // computes the same overflow test in web-runtime.ts, offers plenty.
    if (el.scrollHeight > el.clientHeight || el.scrollWidth > el.clientWidth) {
      attrs['scrollable'] = 'true';
    }
    if (isRoot) attrs['sanderling-screen'] = route;
    const isClickable = clickableSet.has(el);
    const isEditable = editableSet.has(el);
    const children = [];
    // Shadow content first, then light children: the shadow tree is what the
    // host actually renders, and targetElements in web-runtime.ts walks the same
    // order, which is the order the two enumerations are compared in.
    if (el.shadowRoot) {
      for (const child of el.shadowRoot.children) {
        children.push(buildTree(child, false));
      }
    }
    for (const child of el.children) {
      if (child.tagName === 'HEAD') continue;
      children.push(buildTree(child, false));
    }
    return {
      attributes: attrs,
      children: children,
      // Emitted as plain booleans, never null: internal/hierarchy writes the
      // attribute a selector matches on only where the producer stated the
      // flag, so a state that arrives as null is one no selector can ask about.
      // {clickable: false} and {enabled: false} matched nothing at all here
      // while matching on android, which states every flag both ways.
      clickable: isClickable,
      enabled: isEnabled(el),
      focused: focusedElement === el,
      // A component keeps what it likes in these two properties, so what is
      // emitted is the flag the field declares and not the property's value.
      checked: el.checked === true,
      selected: el.selected === true,
      // Emitted as a plain boolean, never null, on every editable field: a
      // consumer deciding what a typed value may be recorded as has to tell
      // "not a secure entry" apart from "nobody said", and android says nothing.
      secure: isEditable ? el.type === 'password' : null,
      // Emitted as a plain boolean, never null: internal/hierarchy falls back to
      // the native heuristic when the field is absent, which reads any class
      // name containing "EditText" as an Android text widget. On web that is a
      // CSS class, so a page styling a div with it made the goja host offer
      // typing into a div the web runtime never calls editable.
      editable: isEditable,
    };
  }
  // Rooted at documentElement, not body, because collectTargets in
  // pkg/spec/src/web-runtime.ts walks querySelectorAll("*") and therefore sees
  // html. Page-level scrolling lives on html on a standard page, so a dump
  // rooted at body hides it from the goja host and the two enumerations
  // disagree on exactly the page scroll. The head subtree is skipped: it is all
  // zero-bounds, so it changes no eligible set, and it would otherwise pull
  // script and title text into the trace and the replay view.
  return buildTree(document.documentElement, true);
})()`

	var result any
	if err := chromedp.Run(runCtx, chromedp.Evaluate(script, &result)); err != nil {
		return "", fmt.Errorf("hierarchy: %w", err)
	}
	bytes, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("hierarchy marshal: %w", err)
	}
	return string(bytes), nil
}

func (d *Driver) Screenshot(ctx context.Context) (driver.Image, error) {
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	var buf []byte
	if err := chromedp.Run(runCtx, chromedp.CaptureScreenshot(&buf)); err != nil {
		return driver.Image{}, fmt.Errorf("screenshot: %w", err)
	}
	w, h := pngDimensions(buf)
	return driver.Image{PNG: buf, Width: w, Height: h}, nil
}

// Snapshot pairs hierarchy and screenshot back-to-back. The chromedp tab
// is single-threaded so the two CDP round-trips are already serialized:
// pairing them here matches the DeviceDriver contract without extra locking.
func (d *Driver) Snapshot(ctx context.Context) (string, driver.Image, error) {
	hierarchy, err := d.Hierarchy(ctx)
	if err != nil {
		return "", driver.Image{}, err
	}
	image, err := d.Screenshot(ctx)
	if err != nil {
		return hierarchy, driver.Image{}, err
	}
	return hierarchy, image, nil
}

func (d *Driver) RecentLogs(_ context.Context, since time.Time, minLevel string) ([]driver.LogEntry, error) {
	sinceMillis := since.UnixMilli()
	d.logsMu.Lock()
	defer d.logsMu.Unlock()
	var result []driver.LogEntry
	for _, entry := range d.logs {
		if entry.UnixMillis < sinceMillis {
			continue
		}
		if minLevel != "" && !meetsLevel(entry.Level, minLevel) {
			continue
		}
		result = append(result, entry)
	}
	return result, nil
}

// domQuietPeriod is how long the DOM must stop changing before the page counts
// as settled. Compose for Web syncs its accessibility DOM off the frame loop:
// measured at ~136 ms behind an InputText on the folio wasm build, so waiting
// for frames alone (~16 ms each) returns while the app still reports the old
// text, and the next step types into a field it believes is still empty.
const domQuietPeriod = 150 * time.Millisecond

// transitionSettlePeriod is how much longer the settle waits for a route
// transition to finish once the DOM has gone quiet. A canvas app's cross-fade
// is invisible to a mutation observer: Compose splices the incoming screen's
// accessibility nodes in when the animation STARTS and removes the outgoing
// screen's when it ends, and nothing in between touches the DOM, so the tree
// sits byte-identical (and quiet) with both routes live for the whole
// animation. Settling on quiet alone returns there, and the next step then
// verifies a tree that names the screen the app is leaving: on the folio wasm
// build a submit that landed on Home was recorded as still being on the
// transaction screen, so a property gated on where the action landed read the
// wrong route and went vacuous. The wait is bounded so a page that genuinely
// shows two *Screen ids at rest costs this much per step and no more.
const transitionSettlePeriod = 800 * time.Millisecond

// settleReturnMargin is what WaitForIdle holds back from the caller's timeout,
// so returning late by our own doing surfaces as a settled page rather than a
// context cancellation.
const settleReturnMargin = 100 * time.Millisecond

// settleScanMargin covers the in-page work the two waits do not themselves
// account for: liveScreens() walks the document and every shadow root on each
// 16 ms poll, and the whole script costs one CDP round trip.
const settleScanMargin = 250 * time.Millisecond

// MinIdleTimeout is the shortest timeout WaitForIdle can be handed and still
// spend the waits it is built from: the DOM quiet period, the route-transition
// window that only opens once that quiet period has elapsed, and the second
// quiet period the transition's own closing mutation starts. A caller that
// passes less caps the settle below its own budget, and the step then samples a
// page that is still mid-transition - which is the exact failure the transition
// wait exists to prevent. internal/runner raises a shorter caller timeout to
// this value.
func (d *Driver) MinIdleTimeout() time.Duration {
	return 2*domQuietPeriod + transitionSettlePeriod +
		settleScanMargin + settleReturnMargin
}

func (d *Driver) WaitForIdle(ctx context.Context, timeout time.Duration) error {
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	// Leave the caller's deadline some room: returning late by our own doing
	// would surface as a context cancellation instead of a settled page.
	budget := max(timeout-settleReturnMargin, domQuietPeriod)
	script := fmt.Sprintf(settleScript,
		domQuietPeriod.Milliseconds(),
		budget.Milliseconds(),
		transitionSettlePeriod.Milliseconds(),
	)
	return chromedp.Run(runCtx,
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Evaluate(script, nil, awaitPromise),
	)
}

// liveScreensFunction defines liveScreens(), the page-side count of live ids
// ending in "Screen". More than one is a route transition in flight: the same
// rule the tree parser applies (Transitional in internal/hierarchy), so the
// driver and the runner agree on what a settled route looks like. It descends
// shadow roots because a canvas app keeps its whole accessibility tree inside
// one.
const liveScreensFunction = `
  const liveScreens = () => {
    let count = 0;
    const visit = (root) => {
      count += root.querySelectorAll('[id$="Screen"]').length;
      for (const element of root.querySelectorAll('*')) {
        if (element.shadowRoot) visit(element.shadowRoot);
      }
    };
    visit(document);
    return count;
  };`

// settleScript resolves once the document has gone quiet for %d ms and is not
// mid route transition, or after %d ms whatever happens; the transition wait
// itself gives up after %d ms. Shadow roots get their own observer: a canvas
// app keeps its whole accessibility tree inside one, and mutations there do not
// reach an observer on the document.
//
// The transition window opens when the quiet period ends, not when the script
// starts. Anchored at the start it is already spent by the time the check can
// first run on any page that keeps mutating for longer than the window, so the
// wait resolves immediately with both routes still live - the mid-transition
// return this whole wait exists to prevent. Each mutation reopens it, and the
// budget above bounds the total either way.
const settleScript = `
new Promise(resolve => {
  const quietMillis = %d, budgetMillis = %d, transitionMillis = %d;
  const observers = [];
  let transitionDeadline = 0;
  let timer = null;
  const finish = () => {
    clearTimeout(timer);
    for (const observer of observers) observer.disconnect();
    resolve();
  };
` + liveScreensFunction + `
  const quiet = () => {
    if (transitionDeadline === 0) transitionDeadline = Date.now() + transitionMillis;
    if (liveScreens() > 1 && Date.now() < transitionDeadline) {
      timer = setTimeout(quiet, 16);
      return;
    }
    finish();
  };
  const restart = () => {
    clearTimeout(timer);
    transitionDeadline = 0;
    timer = setTimeout(quiet, quietMillis);
  };
  const watch = (root) => {
    const observer = new MutationObserver(restart);
    observer.observe(root, {subtree: true, childList: true, attributes: true, characterData: true});
    observers.push(observer);
    for (const element of root.querySelectorAll('*')) {
      if (element.shadowRoot) watch(element.shadowRoot);
    }
  };
  watch(document);
  setTimeout(finish, budgetMillis);
  restart();
})`

func awaitPromise(params *runtime.EvaluateParams) *runtime.EvaluateParams {
	return params.WithAwaitPromise(true)
}

func (d *Driver) Health(_ context.Context) (driver.Health, error) {
	select {
	case <-d.tabCtx.Done():
		return driver.Health{Ready: false, Version: "chrome", Platform: "web"}, nil
	default:
		return driver.Health{Ready: true, Version: "chrome", Platform: "web"}, nil
	}
}

func (d *Driver) Metrics(ctx context.Context, _ string) (driver.Metrics, error) {
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	var result map[string]any
	script := `
(function() {
  const mem = performance.memory || {};
  return {heap: mem.usedJSHeapSize || 0, totalMem: mem.totalJSHeapSize || 0};
})()`
	if err := chromedp.Run(runCtx, chromedp.Evaluate(script, &result)); err != nil {
		return driver.Metrics{}, nil
	}
	heap, _ := result["heap"].(float64)
	total, _ := result["totalMem"].(float64)
	return driver.Metrics{
		HeapBytes:        int64(heap),
		TotalMemoryBytes: int64(total),
	}, nil
}

// consoleLevel places a console call on driver.LogEntry's logcat scale. The
// verbs a spec acts on are all named here; the rest are info rather than "E"
// because promoting them would convict an app of an error it never logged.
func consoleLevel(apiType runtime.APIType) string {
	switch apiType {
	case runtime.APITypeError, runtime.APITypeAssert:
		return "E"
	case runtime.APITypeWarning:
		return "W"
	case runtime.APITypeDebug:
		return "D"
	default:
		return "I"
	}
}

// meetsLevel keeps an entry whose level the scale cannot rank. Ranking an
// unknown level below every threshold drops it, and a dropped entry is
// indistinguishable from a quiet app: the caller sees silence and reports it as
// health.
func meetsLevel(level, minLevel string) bool {
	order := map[string]int{"V": 0, "D": 1, "I": 2, "W": 3, "E": 4, "F": 5}
	rank, ranked := order[level]
	if !ranked {
		return true
	}
	return rank >= order[minLevel]
}

func pngDimensions(png []byte) (int, int) {
	if len(png) < 24 {
		return 0, 0
	}
	w := int(png[16])<<24 | int(png[17])<<16 | int(png[18])<<8 | int(png[19])
	h := int(png[20])<<24 | int(png[21])<<16 | int(png[22])<<8 | int(png[23])
	return w, h
}

var (
	_ driver.DeviceDriver = (*Driver)(nil)
	_ driver.WebDriver    = (*Driver)(nil)
)

// runCtx returns a chromedp-bound context that is also cancelled when the
// caller's ctx is cancelled. This is how step deadlines and Ctrl-C propagate
// into a CDP round-trip - chromedp.Run only honors the ctx it is given, and
// d.tabCtx alone has no link to the caller.
func (d *Driver) runCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	derived, cancel := context.WithCancel(d.tabCtx)
	if ctx == nil || ctx.Done() == nil {
		return derived, cancel
	}
	go func() {
		select {
		case <-ctx.Done():
			cancel()
		case <-derived.Done():
		}
	}()
	return derived, cancel
}

// InstallBundle registers the source so it runs at every freshly-navigated
// document context, then immediately evaluates it against the current page so
// the very first tick has access to the registered globals.
func (d *Driver) InstallBundle(ctx context.Context, source []byte) error {
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	return chromedp.Run(runCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			if _, err := page.AddScriptToEvaluateOnNewDocument(string(source)).Do(ctx); err != nil {
				return fmt.Errorf("addScriptToEvaluateOnNewDocument: %w", err)
			}
			_, exception, err := runtime.Evaluate(string(source)).Do(ctx)
			if err != nil {
				return fmt.Errorf("evaluate bundle: %w", err)
			}
			if exception != nil {
				return fmt.Errorf("bundle threw: %s", exceptionMessage(exception))
			}
			return nil
		}),
	)
}

// EvaluateExtractors invokes the bundle-installed extractor table and returns
// each extractor's JSON-encoded current value keyed by its registration index.
//
// The read waits out a route transition first, bounded by
// transitionSettlePeriod. The hierarchy fetch already re-fetches a transitional
// tree (fetchSyncedState in internal/runner); without the same rule here the
// two halves of one step describe different moments, and the spec's own
// extractors are the half that loses: on the folio wasm build the extractors
// sampled mid cross-fade and reported the route the app was leaving, so a
// property gated on where the action landed skipped the only step that action
// could be judged on.
func (d *Driver) EvaluateExtractors(ctx context.Context) (map[int]json.RawMessage, error) {
	script := fmt.Sprintf(extractorScript, transitionSettlePeriod.Milliseconds())
	var encoded string
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	if err := chromedp.Run(runCtx, chromedp.Evaluate(script, &encoded, awaitPromise)); err != nil {
		return nil, fmt.Errorf("evaluate extractors: %w", err)
	}
	if encoded == "" || encoded == "{}" {
		return map[int]json.RawMessage{}, nil
	}
	stringMap := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(encoded), &stringMap); err != nil {
		return nil, fmt.Errorf("decode extractor map: %w", err)
	}
	result := make(map[int]json.RawMessage, len(stringMap))
	for key, entry := range stringMap {
		index, err := strconv.Atoi(key)
		if err != nil {
			return nil, fmt.Errorf("non-integer extractor key %q", key)
		}
		reading, err := extractorReading(entry)
		if err != nil {
			return nil, fmt.Errorf("extractor %d: %w", index, err)
		}
		result[index] = reading
	}
	return result, nil
}

// extractorReading unwraps one entry of the page's extractor table. The page
// wraps every reading in a {"value": ...} envelope (evaluateExtractors in
// pkg/spec/src/web-runtime.ts) because JSON has no undefined: an absent `value`
// is the getter returning undefined, and returning it as an empty payload is
// what makes the goja host record undefined too. Reading it as JSON null would
// claim the getter returned null, so `x.current === undefined` would answer one
// thing on native and another on web.
func extractorReading(entry json.RawMessage) (json.RawMessage, error) {
	var envelope struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(entry, &envelope); err != nil {
		return nil, fmt.Errorf(
			"reading %s is not a {\"value\"} envelope; the page and the host are "+
				"running different bundles: %w", entry, err)
	}
	return envelope.Value, nil
}

// SetLastAction installs the previous step's action as state.lastAction inside
// the page runtime. The page cannot derive it: only the runner knows which
// action was actually applied. Without this call every web state.lastAction is
// null, so a property gated on what the last action did is vacuously true and
// reports a green run while checking nothing.
//
// The call is deliberately unguarded. A `setter && setter(...)` form evaluates
// to undefined on a page whose runtime does not define the setter, and chromedp
// reports that as success, so "the page cannot accept lastAction" would be
// indistinguishable from "installed". That page is reachable: a run resolving
// its web runtime from an older published @sanderling/spec would silently no-op
// every step. Unguarded, the missing global throws and the run fails loudly.
func (d *Driver) SetLastAction(ctx context.Context, encoded json.RawMessage) error {
	payload := strings.TrimSpace(string(encoded))
	if payload == "" {
		payload = "null"
	}
	script := fmt.Sprintf(`window.__sanderlingSetLastAction__(%s)`, payload)
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	if err := chromedp.Run(runCtx, chromedp.Evaluate(script, nil)); err != nil {
		return fmt.Errorf("set last action: %w", err)
	}
	return nil
}

// SetLogs installs the entries this step's log fetch returned as state.logs
// inside the page runtime. The page cannot derive them: console output reaches
// the driver over CDP and nothing in the page reads it back. Without this call
// every web state.logs is empty, and since the page's reading of an extractor
// replaces the host's, the default noLogcatErrors then reports green on a run
// whose console was full of errors.
//
// Unguarded for the same reason as SetLastAction: on a page with no setter,
// "the page cannot accept logs" has to fail the run rather than be reported as
// a successful install.
func (d *Driver) SetLogs(ctx context.Context, encoded json.RawMessage) error {
	payload := strings.TrimSpace(string(encoded))
	if payload == "" {
		payload = "[]"
	}
	script := fmt.Sprintf(`window.__sanderlingSetLogs__(%s)`, payload)
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	if err := chromedp.Run(runCtx, chromedp.Evaluate(script, nil)); err != nil {
		return fmt.Errorf("set logs: %w", err)
	}
	return nil
}

// extractorScript resolves the extractor table once the page is not mid route
// transition, giving up on that wait after %d ms.
//
// A missing table rejects rather than reporting {}, for the same reason
// SetLastAction no longer guards its call: an empty override map is what a
// spec with no extractors returns, so the guarded form made "this page has no
// sanderling runtime" read as a normal step whose properties then ran on
// goja's dump-derived values instead of the page's.
const extractorScript = `
new Promise((resolve, reject) => {
  const deadline = Date.now() + %d;` + liveScreensFunction + `
  const read = () => {
    if (liveScreens() > 1 && Date.now() < deadline) {
      setTimeout(read, 16);
      return;
    }
    if (typeof window.__sanderlingExtractors__ !== "function") {
      reject(new Error("__sanderlingExtractors__ is not installed in the page"));
      return;
    }
    resolve(JSON.stringify(window.__sanderlingExtractors__()));
  };
  read();
})`

// Exceptions returns the uncaught errors and unhandled rejections the page
// runtime has buffered so far. The buffer is cumulative, which is what
// state.exceptions means inside the page (buildState in
// pkg/spec/src/web-runtime.ts), so the host and the page read one list.
func (d *Driver) Exceptions(ctx context.Context) ([]driver.Exception, error) {
	const script = `JSON.stringify(window.__sanderlingExceptions__ ? window.__sanderlingExceptions__() : [])`
	var encoded string
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	if err := chromedp.Run(runCtx, chromedp.Evaluate(script, &encoded)); err != nil {
		return nil, fmt.Errorf("evaluate exceptions: %w", err)
	}
	if encoded == "" || encoded == "[]" {
		return nil, nil
	}
	var captured []struct {
		Class      string `json:"class"`
		Message    string `json:"message"`
		StackTrace string `json:"stackTrace"`
		UnixMillis int64  `json:"unixMillis"`
	}
	if err := json.Unmarshal([]byte(encoded), &captured); err != nil {
		return nil, fmt.Errorf("decode exceptions %s: %w", encoded, err)
	}
	result := make([]driver.Exception, 0, len(captured))
	for _, entry := range captured {
		result = append(result, driver.Exception{
			Class:      entry.Class,
			Message:    entry.Message,
			StackTrace: entry.StackTrace,
			UnixMillis: entry.UnixMillis,
		})
	}
	return result, nil
}

// nextActionScript puts the carried draw position back before the picker
// decides and reads the new one out afterwards, in the one evaluation, so no
// navigation can land between the restore and the draw.
const nextActionScript = `((carried) => {
  if (!window.__sanderlingNextAction__) return "{}";
  if (carried !== "" && window.__sanderlingRestorePickerState__) {
    window.__sanderlingRestorePickerState__(carried);
  }
  const action = window.__sanderlingNextAction__();
  const state = window.__sanderlingPickerState__ ? window.__sanderlingPickerState__() : "";
  return JSON.stringify({action, state});
})(%s)`

// NextActionFromV8 invokes the bundle-installed action generator and returns
// the resulting Action JSON. Returns an empty json.RawMessage when the
// generator declines to act this tick.
//
// The picker's draw position rides along: it lives here rather than in the
// page, because a page that navigates gets a fresh runtime whose picker would
// otherwise start the seed's stream over at its first draw on every reload.
func (d *Driver) NextActionFromV8(ctx context.Context) (json.RawMessage, error) {
	script := fmt.Sprintf(nextActionScript, strconv.Quote(d.pickerState))
	var encoded string
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	if err := chromedp.Run(runCtx, chromedp.Evaluate(script, &encoded)); err != nil {
		return nil, fmt.Errorf("evaluate next action: %w", err)
	}
	if encoded == "" {
		return nil, nil
	}
	var decoded struct {
		Action json.RawMessage `json:"action"`
		State  string          `json:"state"`
	}
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		return nil, fmt.Errorf("decode next action %s: %w", encoded, err)
	}
	// An empty state means the page had no runtime to ask, so the position we
	// already hold is still the run's position.
	if decoded.State != "" {
		d.pickerState = decoded.State
	}
	if len(decoded.Action) == 0 || string(decoded.Action) == "null" {
		return nil, nil
	}
	return decoded.Action, nil
}
