package driver

import (
	"context"
	"encoding/json"
	"time"
)

// DeviceDriver abstracts the platform-specific UI automation backend. v0.1
// surface matches proto/driverpb/driver.proto. The sidecar implementation
// lives under driver/sidecar; the web implementation under driver/chrome;
// tests use driver/mock.
type DeviceDriver interface {
	Launch(ctx context.Context, bundleID string, clearState bool, env map[string]string) error
	Terminate(ctx context.Context) error

	Tap(ctx context.Context, x, y int) error
	TapSelector(ctx context.Context, selector string) error
	InputText(ctx context.Context, text string) error
	Swipe(ctx context.Context, fromX, fromY, toX, toY int, duration time.Duration) error
	PressKey(ctx context.Context, key string) error

	Hierarchy(ctx context.Context) (string, error)
	Screenshot(ctx context.Context) (Image, error)
	// Snapshot returns the hierarchy and screenshot captured back-to-back
	// under a backend-side mutex, so the pair describes the same on-device
	// frame. Prefer this over calling Hierarchy and Screenshot separately:
	// independent reads can land on different frames during transitions.
	Snapshot(ctx context.Context) (string, Image, error)
	// RecentLogs returns log entries at or after `since`, filtered to
	// `minLevel` or above. An empty minLevel defaults to "E".
	RecentLogs(ctx context.Context, since time.Time, minLevel string) ([]LogEntry, error)

	WaitForIdle(ctx context.Context, duration time.Duration) error
	Health(ctx context.Context) (Health, error)
	// Metrics samples the app's CPU and memory at the time of the call.
	// CPUPercent is percent of a single core (multi-core apps can exceed
	// 100). HeapBytes is resident set size; TotalMemoryBytes includes
	// native allocations.
	Metrics(ctx context.Context, bundleID string) (Metrics, error)
}

// ForegroundChecker is the optional capability for reporting which app is
// currently in the foreground. The runner uses it to keep exploration scoped
// to the app under test: when an action backs out of (or otherwise leaves) the
// app, the runner relaunches it before acting again. Drivers that cannot
// determine the foreground app simply do not implement this interface.
type ForegroundChecker interface {
	// ForegroundApp returns the bundle id / package of the app currently in
	// the foreground. An empty string means "unknown" and the runner skips
	// enforcement for that step rather than relaunching blindly.
	ForegroundApp(ctx context.Context) (string, error)
}

// FocusedWindowChecker is the optional capability for reporting which app owns
// the focused (on-screen) window. The startup gate prefers it over
// ForegroundChecker: the resumed-activity signal flips to a freshly launched
// app before its first frame draws, so observing on it alone can capture the
// previous app's screen. The focused window only names the app once its window
// is actually up.
type FocusedWindowChecker interface {
	// FocusedWindowApp returns the package owning the focused window, or ""
	// when no window is focused yet (e.g. mid-launch transition).
	FocusedWindowApp(ctx context.Context) (string, error)
}

type LogEntry struct {
	UnixMillis int64
	Level      string
	Tag        string
	Message    string
}

type Image struct {
	PNG    []byte
	Width  int
	Height int
}

type Health struct {
	Ready    bool
	Version  string
	Platform string
}

type Metrics struct {
	CPUPercent       float64
	HeapBytes        int64
	TotalMemoryBytes int64
}

// WebDriver is the optional capability surface exposed by the chrome driver
// for the V8-native tick path. The runner type-asserts on this interface;
// mobile drivers stay binary-compatible by simply not implementing it.
//
// Element references never cross V8/host. V8 serializes targets as {x, y}
// (or bounds) into the returned WebAction JSON; the host dispatches via the
// normal DeviceDriver methods (Tap, InputText, etc.).
type WebDriver interface {
	// InstallBundle injects the given JS source so it runs once per
	// freshly-navigated document, plus immediately in the current page.
	// The bundle is expected to register globals
	// `__sanderlingExtractors__` and `__sanderlingNextAction__` on
	// `window`.
	InstallBundle(ctx context.Context, source []byte) error
	// EvaluateExtractors invokes the extractor table installed by the
	// bundle and returns each extractor's JSON-encoded current value
	// keyed by its registration index.
	EvaluateExtractors(ctx context.Context) (map[int]json.RawMessage, error)
	// NextActionFromV8 invokes the action generator installed by the
	// bundle and returns the resulting Action JSON for the host to
	// dispatch. The shape mirrors verifier.Action's JSON form.
	NextActionFromV8(ctx context.Context) (json.RawMessage, error)
}
