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
	// Document returns the current page outerHTML for trace capture.
	Document(ctx context.Context) (string, error)
}
