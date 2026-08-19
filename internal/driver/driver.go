// Package driver defines the platform-agnostic device automation interface and shared types.
package driver

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrGestureUndelivered reports a coordinate gesture that reached no element at
// all, so the app cannot have responded to it. It is not a device fault and
// says nothing about the device's health: the runner records it on the step
// rather than counting it toward the apply-failure streak, which is what makes
// a gesture that did nothing distinguishable from one the app ignored.
var ErrGestureUndelivered = errors.New("gesture reached no element")

// ErrSelectorMatchedNothing reports an action dispatched by selector whose
// selector named nothing on the current screen. It is a resolution failure, not
// a delivery one: no point was ever computed, so it stays separate from
// ErrGestureUndelivered and the runner records it as an unresolved selector.
var ErrSelectorMatchedNothing = errors.New("selector matched no element")

// DeviceDriver abstracts the platform-specific UI automation backend. v0.1
// surface matches proto/driverpb/driver.proto. The sidecar implementation
// lives under driver/sidecar; the web implementation under driver/chrome;
// tests use driver/mock.
type DeviceDriver interface {
	Launch(ctx context.Context, bundleID string, clearState bool, env map[string]string) error
	Terminate(ctx context.Context) error

	Tap(ctx context.Context, x, y int) error
	TapSelector(ctx context.Context, selector string) error
	DoubleTap(ctx context.Context, x, y int) error
	DoubleTapSelector(ctx context.Context, selector string) error
	InputText(ctx context.Context, text string) error
	// EraseText deletes characterCount characters from the focused field.
	// The runner calls it before InputText so the verb replaces existing
	// content instead of appending to it.
	EraseText(ctx context.Context, characterCount int) error
	Swipe(ctx context.Context, fromX, fromY, toX, toY int, duration time.Duration) error
	PressKey(ctx context.Context, key string) error
	LongPress(ctx context.Context, x, y int) error

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

// Scroller is the optional capability for drivers whose scroll interaction is
// not a finger drag. On a touch device the two are the same gesture, so a
// driver that does not implement this gets its Scroll actions as a Swipe. A
// browser scrolls on wheel input instead, and treats a drag as a drag.
type Scroller interface {
	// Scroll moves the content under (fromX, fromY) by the vector to the
	// destination point, the same endpoints Swipe takes.
	Scroll(
		ctx context.Context,
		fromX, fromY, toX, toY int,
		duration time.Duration,
	) error
}

// TextReplacer is the optional capability for drivers whose InputText already
// replaces the field's content instead of appending to it. The runner must
// skip its pre-erase for such drivers: the erase would be a redundant
// round-trip on every InputText.
type TextReplacer interface {
	// ReplacesTextOnInput reports whether InputText replaces existing
	// content, making the runner's pre-erase unnecessary.
	ReplacesTextOnInput() bool
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

// LogEntry is one line of device log. Level is logcat's single-letter scale on
// every platform: "V", "D", "I", "W", "E", "F", ordered as written. The runner
// fetches at "E" and the default properties count entries whose level equals
// "E", so a driver that spells a level any other way empties the channel
// without failing anything: the entries never arrive and every property reading
// state.logs holds vacuously.
type LogEntry struct {
	UnixMillis int64
	Level      string
	Tag        string
	Message    string
}

// ExceptionReporter is the optional capability for reporting the uncaught
// errors an app has captured so far. The runner feeds them to state.exceptions,
// which the default noUncaughtExceptions property reads. Drivers with no way to
// observe them simply do not implement it and the property stays vacuous there.
type ExceptionReporter interface {
	Exceptions(ctx context.Context) ([]Exception, error)
}

// NavigationReporter is the optional capability for reporting the
// document-replacing navigations seen since the last call. A navigation
// restarts the app's own runtime, so a trace without them cannot separate an
// app that reloaded from a generator that repeated itself.
type NavigationReporter interface {
	Navigations(ctx context.Context) ([]Navigation, error)
}

// Navigation is one document-replacing navigation: a reload, a form submit, a
// route change that swapped the document.
type Navigation struct {
	URL        string
	UnixMillis int64
}

// Exception is one uncaught throwable the app captured.
type Exception struct {
	Class      string
	Message    string
	StackTrace string
	UnixMillis int64
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
