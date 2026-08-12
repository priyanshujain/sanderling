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
			if arg.Value != nil {
				var s string
				if err := json.Unmarshal(arg.Value, &s); err == nil {
					parts = append(parts, s)
				} else {
					parts = append(parts, string(arg.Value))
				}
			}
		}
		level := strings.ToUpper(string(e.Type))
		if level == "LOG" {
			level = "I"
		}
		d.logsMu.Lock()
		d.logs = append(d.logs, driver.LogEntry{
			UnixMillis: int64(e.Timestamp.Time().UnixMilli()),
			Level:      level,
			Tag:        "console",
			Message:    strings.Join(parts, " "),
		})
		d.logsMu.Unlock()
	})

	return d
}

func (d *Driver) Launch(ctx context.Context, bundleID string, clearState bool, _ map[string]string) error {
	if clearState {
		if err := d.clearState(bundleID); err != nil {
			return err
		}
	}
	if err := chromedp.Run(d.tabCtx, chromedp.Navigate(bundleID)); err != nil {
		return err
	}
	// After navigation, read CSS custom properties --frame-w / --frame-h (common
	// mobile-frame convention) so screenshots fit the app without grey borders.
	// Falls back to the body scroll dimensions if the properties are absent.
	var dims [2]int64
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(`
		(function() {
			const s = getComputedStyle(document.documentElement);
			const pw = parseInt(s.getPropertyValue('--frame-w'), 10);
			const ph = parseInt(s.getPropertyValue('--frame-h'), 10);
			const w = isNaN(pw) ? document.body.scrollWidth : pw;
			const h = isNaN(ph) ? document.body.scrollHeight : ph;
			return [w, h];
		})()`, &dims)); err == nil && dims[0] > 0 && dims[1] > 0 {
		_ = chromedp.Run(d.tabCtx, chromedp.EmulateViewport(dims[0], dims[1]))
	}
	return nil
}

// clearState wipes the target's stored data before the application loads.
// Script cannot do it: the tab still sits on about:blank, whose opaque origin
// denies storage access, so `localStorage.clear()` throws SecurityError and
// every web run dies at launch. The Storage domain clears by origin instead,
// which needs no navigation. sessionStorage is per-tab and outside that
// domain's reach; it only survives when a relaunch reuses a tab already on
// the target origin, which is the one case where script can reach it.
func (d *Driver) clearState(bundleID string) error {
	if err := chromedp.Run(d.tabCtx, network.ClearBrowserCookies()); err != nil {
		return fmt.Errorf("clear cookies: %w", err)
	}
	origin := securityOrigin(bundleID)
	if origin == "" {
		return nil
	}
	clearForOrigin := storage.ClearDataForOrigin(origin, string(storage.TypeAll))
	if err := chromedp.Run(d.tabCtx, clearForOrigin); err != nil {
		return fmt.Errorf("clear storage for %s: %w", origin, err)
	}
	script := fmt.Sprintf(
		`location.origin === %q && (sessionStorage.clear(), true)`, origin)
	return chromedp.Run(d.tabCtx, chromedp.ActionFunc(func(ctx context.Context) error {
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

func (d *Driver) Tap(ctx context.Context, x, y int) error {
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	return chromedp.Run(runCtx,
		chromedp.MouseClickXY(float64(x), float64(y)),
	)
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
	if isXPath {
		return chromedp.Run(runCtx, chromedp.Click(target, chromedp.NodeVisible, chromedp.BySearch))
	}
	return chromedp.Run(runCtx, chromedp.Click(target, chromedp.NodeVisible))
}

// doubleTapGap is the inter-tap delay for DoubleTap: short enough to land both
// events inside a sub-100 ms race window. The browser has no single double-tap
// primitive, so the gesture is two taps with this gap.
const doubleTapGap = 50 * time.Millisecond

func (d *Driver) DoubleTap(ctx context.Context, x, y int) error {
	return webDoubleTap(ctx, func() error { return d.Tap(ctx, x, y) })
}

func (d *Driver) DoubleTapSelector(ctx context.Context, selector string) error {
	return webDoubleTap(ctx, func() error { return d.TapSelector(ctx, selector) })
}

func webDoubleTap(ctx context.Context, tap func() error) error {
	if err := tap(); err != nil {
		return err
	}
	timer := time.NewTimer(doubleTapGap)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	return tap()
}

func (d *Driver) InputText(callerCtx context.Context, text string) error {
	runCtx, cancel := d.runCtx(callerCtx)
	defer cancel()
	return chromedp.Run(runCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			// Select any existing content so InsertText replaces rather than appends.
			if err := chromedp.Evaluate(`
				(function() {
					const el = document.activeElement;
					if (el && typeof el.select === 'function') el.select();
				})()`, nil).Do(ctx); err != nil {
				return err
			}
			return input.InsertText(text).Do(ctx)
		}),
	)
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
			if err := chromedp.Evaluate(`
				(function() {
					const el = document.activeElement;
					if (el && typeof el.select === 'function') el.select();
				})()`, nil).Do(ctx); err != nil {
				return err
			}
			return input.InsertText("").Do(ctx)
		}),
	)
}

func (d *Driver) Swipe(ctx context.Context, fromX, fromY, toX, toY int, duration time.Duration) error {
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	millis := max(duration.Milliseconds(), 50)
	script := fmt.Sprintf(`
(function() {
  const el = document.elementFromPoint(%d, %d);
  if (!el) return;
  const steps = Math.max(1, Math.floor(%d / 16));
  const dx = (%d - %d) / steps;
  const dy = (%d - %d) / steps;
  el.dispatchEvent(new PointerEvent('pointerdown', {clientX: %d, clientY: %d, bubbles: true}));
  for (let i = 1; i <= steps; i++) {
    el.dispatchEvent(new PointerEvent('pointermove', {clientX: %d + dx*i, clientY: %d + dy*i, bubbles: true}));
  }
  el.dispatchEvent(new PointerEvent('pointerup', {clientX: %d, clientY: %d, bubbles: true}));
})();`,
		fromX, fromY,
		millis,
		toX, fromX, toY, fromY,
		fromX, fromY,
		fromX, fromY,
		toX, toY,
	)
	return chromedp.Run(runCtx, chromedp.Evaluate(script, nil))
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
  const route = window.location.hash.replace(/^#/, '').split('?')[0] || '/';
  function buildTree(el, isRoot) {
    const rect = el.getBoundingClientRect();
    const attrs = {};
    const bounds = '[' + Math.round(rect.left) + ',' + Math.round(rect.top) + ',' +
      Math.round(rect.right) + ',' + Math.round(rect.bottom) + ']';
    if (rect.width > 0 || rect.height > 0) attrs.bounds = bounds;
    const text = (el.textContent || '').trim().slice(0, 200);
    if (text) attrs.text = text;
    if (el.id) attrs['resource-id'] = el.id;
    const label = el.getAttribute('aria-label') || el.getAttribute('alt') || el.getAttribute('title') || '';
    if (label) attrs['content-desc'] = label;
    const tag = (el.tagName || '').toLowerCase();
    if (tag) attrs['tag'] = tag;
    if (el.className && typeof el.className === 'string' && el.className.trim()) {
      attrs['class'] = el.className.trim();
    }
    if (isRoot) attrs['sanderling-screen'] = route;
    const isClickable = !!(el.onclick || el.tagName === 'A' || el.tagName === 'BUTTON' ||
      el.tagName === 'INPUT' || el.tagName === 'SELECT' ||
      el.getAttribute('role') === 'button' || el.getAttribute('onclick'));
    const isEditable = el.isContentEditable || tag === 'textarea' ||
      (tag === 'input' && !['button','submit','checkbox','radio','range','color','file','image','reset']
        .includes((el.type || '').toLowerCase()));
    const children = [];
    for (const child of el.children) {
      children.push(buildTree(child, false));
    }
    return {
      attributes: attrs,
      children: children,
      clickable: isClickable || null,
      enabled: (!el.disabled) || null,
      focused: document.activeElement === el || null,
      checked: el.checked || null,
      selected: el.selected || null,
      editable: isEditable || null,
    };
  }
  return buildTree(document.body, true);
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

func (d *Driver) WaitForIdle(ctx context.Context, _ time.Duration) error {
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	return chromedp.Run(runCtx, chromedp.WaitReady("body", chromedp.ByQuery))
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

func meetsLevel(level, minLevel string) bool {
	order := map[string]int{"V": 0, "D": 1, "I": 2, "W": 3, "E": 4, "F": 5}
	return order[level] >= order[minLevel]
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
func (d *Driver) EvaluateExtractors(ctx context.Context) (map[int]json.RawMessage, error) {
	const script = `JSON.stringify(window.__sanderlingExtractors__ ? window.__sanderlingExtractors__() : {})`
	var encoded string
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	if err := chromedp.Run(runCtx, chromedp.Evaluate(script, &encoded)); err != nil {
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
	for key, value := range stringMap {
		index, err := strconv.Atoi(key)
		if err != nil {
			return nil, fmt.Errorf("non-integer extractor key %q", key)
		}
		result[index] = value
	}
	return result, nil
}

// NextActionFromV8 invokes the bundle-installed action generator and returns
// the resulting Action JSON. Returns an empty json.RawMessage when the
// generator declines to act this tick.
func (d *Driver) NextActionFromV8(ctx context.Context) (json.RawMessage, error) {
	const script = `JSON.stringify(window.__sanderlingNextAction__ ? window.__sanderlingNextAction__() : null)`
	var encoded string
	runCtx, cancel := d.runCtx(ctx)
	defer cancel()
	if err := chromedp.Run(runCtx, chromedp.Evaluate(script, &encoded)); err != nil {
		return nil, fmt.Errorf("evaluate next action: %w", err)
	}
	if encoded == "" || encoded == "null" {
		return nil, nil
	}
	return json.RawMessage(encoded), nil
}
