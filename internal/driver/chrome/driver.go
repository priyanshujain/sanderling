package chrome

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

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

	bundleMu     sync.Mutex
	bundleSource string
}

// New creates a new ChromeDriver. Call Terminate when done.
func New() *Driver {
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", true),
			chromedp.Flag("disable-gpu", true),
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
		if err := chromedp.Run(d.tabCtx, network.ClearBrowserCookies()); err != nil {
			return fmt.Errorf("clear cookies: %w", err)
		}
		if err := chromedp.Run(d.tabCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			_, exp, err := runtime.Evaluate(`localStorage.clear(); sessionStorage.clear();`).Do(ctx)
			if exp != nil {
				return fmt.Errorf("clear storage: %s", exp.Text)
			}
			return err
		})); err != nil {
			return fmt.Errorf("clear storage: %w", err)
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

func (d *Driver) Terminate(_ context.Context) error {
	d.tabCancel()
	d.allocCancel()
	return nil
}

func (d *Driver) Tap(_ context.Context, x, y int) error {
	return chromedp.Run(d.tabCtx,
		chromedp.MouseClickXY(float64(x), float64(y)),
	)
}

func (d *Driver) TapSelector(_ context.Context, selector string) error {
	target, isXPath, err := TranslateStringSelector(selector)
	if err != nil {
		// Fall back to passing the string straight through; chromedp will
		// reject it loudly if it isn't a valid CSS selector.
		target = selector
	}
	if isXPath {
		return chromedp.Run(d.tabCtx, chromedp.Click(target, chromedp.NodeVisible, chromedp.BySearch))
	}
	return chromedp.Run(d.tabCtx, chromedp.Click(target, chromedp.NodeVisible))
}

func (d *Driver) InputText(_ context.Context, text string) error {
	return chromedp.Run(d.tabCtx,
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

func (d *Driver) Swipe(_ context.Context, fromX, fromY, toX, toY int, duration time.Duration) error {
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
	return chromedp.Run(d.tabCtx, chromedp.Evaluate(script, nil))
}

func (d *Driver) PressKey(_ context.Context, key string) error {
	k, ok := keyMap[key]
	if !ok {
		return fmt.Errorf("unsupported key: %q", key)
	}
	return chromedp.Run(d.tabCtx, chromedp.KeyEvent(k))
}

var keyMap = map[string]string{
	"back":  "\b",
	"home":  "\x00",
	"enter": "\r",
	"tab":   "\t",
	"up":    "\x26",
	"down":  "\x28",
	"left":  "\x25",
	"right": "\x27",
}

func (d *Driver) Hierarchy(_ context.Context) (string, error) {
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
    if (el.tagName) attrs['tag'] = el.tagName.toLowerCase();
    if (el.className && typeof el.className === 'string' && el.className.trim()) {
      attrs['class'] = el.className.trim();
    }
    if (isRoot) attrs['sanderling-screen'] = route;
    const isClickable = !!(el.onclick || el.tagName === 'A' || el.tagName === 'BUTTON' ||
      el.tagName === 'INPUT' || el.tagName === 'SELECT' ||
      el.getAttribute('role') === 'button' || el.getAttribute('onclick'));
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
    };
  }
  return buildTree(document.body, true);
})()`

	var result any
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(script, &result)); err != nil {
		return "", fmt.Errorf("hierarchy: %w", err)
	}
	bytes, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("hierarchy marshal: %w", err)
	}
	return string(bytes), nil
}

func (d *Driver) Screenshot(_ context.Context) (driver.Image, error) {
	var buf []byte
	if err := chromedp.Run(d.tabCtx, chromedp.CaptureScreenshot(&buf)); err != nil {
		return driver.Image{}, fmt.Errorf("screenshot: %w", err)
	}
	w, h := pngDimensions(buf)
	return driver.Image{PNG: buf, Width: w, Height: h}, nil
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

func (d *Driver) WaitForIdle(_ context.Context, _ time.Duration) error {
	return chromedp.Run(d.tabCtx, chromedp.WaitReady("body", chromedp.ByQuery))
}

func (d *Driver) Health(_ context.Context) (driver.Health, error) {
	select {
	case <-d.tabCtx.Done():
		return driver.Health{Ready: false, Version: "chrome", Platform: "web"}, nil
	default:
		return driver.Health{Ready: true, Version: "chrome", Platform: "web"}, nil
	}
}

func (d *Driver) Metrics(_ context.Context, _ string) (driver.Metrics, error) {
	var result map[string]any
	script := `
(function() {
  const mem = performance.memory || {};
  return {heap: mem.usedJSHeapSize || 0, totalMem: mem.totalJSHeapSize || 0};
})()`
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(script, &result)); err != nil {
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

// InstallBundle registers the source so it runs at every freshly-navigated
// document context, then immediately evaluates it against the current page so
// the very first tick has access to the registered globals.
func (d *Driver) InstallBundle(_ context.Context, source []byte) error {
	d.bundleMu.Lock()
	d.bundleSource = string(source)
	d.bundleMu.Unlock()

	return chromedp.Run(d.tabCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			if _, err := page.AddScriptToEvaluateOnNewDocument(string(source)).Do(ctx); err != nil {
				return fmt.Errorf("addScriptToEvaluateOnNewDocument: %w", err)
			}
			_, exception, err := runtime.Evaluate(string(source)).Do(ctx)
			if err != nil {
				return fmt.Errorf("evaluate bundle: %w", err)
			}
			if exception != nil {
				return fmt.Errorf("bundle threw: %s", exception.Text)
			}
			return nil
		}),
	)
}

// EvaluateExtractors invokes the bundle-installed extractor table and returns
// each extractor's JSON-encoded current value keyed by its registration index.
func (d *Driver) EvaluateExtractors(_ context.Context) (map[int]json.RawMessage, error) {
	const script = `JSON.stringify(window.__sanderlingExtractors__ ? window.__sanderlingExtractors__() : {})`
	var encoded string
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(script, &encoded)); err != nil {
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
		index := 0
		if _, err := fmt.Sscanf(key, "%d", &index); err != nil {
			return nil, fmt.Errorf("non-integer extractor key %q", key)
		}
		result[index] = value
	}
	return result, nil
}

// NextActionFromV8 invokes the bundle-installed action generator and returns
// the resulting Action JSON. Returns an empty json.RawMessage when the
// generator declines to act this tick.
func (d *Driver) NextActionFromV8(_ context.Context) (json.RawMessage, error) {
	const script = `JSON.stringify(window.__sanderlingNextAction__ ? window.__sanderlingNextAction__() : null)`
	var encoded string
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(script, &encoded)); err != nil {
		return nil, fmt.Errorf("evaluate next action: %w", err)
	}
	if encoded == "" || encoded == "null" {
		return nil, nil
	}
	return json.RawMessage(encoded), nil
}

// Document returns the current page's outer HTML for trace capture.
func (d *Driver) Document(_ context.Context) (string, error) {
	var html string
	if err := chromedp.Run(d.tabCtx, chromedp.OuterHTML("html", &html, chromedp.ByQuery)); err != nil {
		return "", fmt.Errorf("document: %w", err)
	}
	return html, nil
}
