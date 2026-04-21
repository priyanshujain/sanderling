package driver

import (
	"context"
	"time"
)

// Driver abstracts the platform-specific UI automation backend. v0.1 surface
// matches proto/driverpb/driver.proto. The Maestro sidecar implementation
// lives under driver/maestro; tests use driver/mock.
type Driver interface {
	// Launch asks the backend to bring the target app to the foreground.
	// launcherActivity is an optional "<pkg>/<activity>" component that
	// overrides the backend's default launcher resolution — needed for
	// apps that declare multiple MAIN+LAUNCHER activities.
	Launch(ctx context.Context, bundleID, launcherActivity string, clearState bool) error
	Terminate(ctx context.Context) error

	Tap(ctx context.Context, x, y int) error
	TapSelector(ctx context.Context, selector string) error
	InputText(ctx context.Context, text string) error
	Swipe(ctx context.Context, fromX, fromY, toX, toY int, duration time.Duration) error
	PressKey(ctx context.Context, key string) error

	Hierarchy(ctx context.Context) (string, error)
	Screenshot(ctx context.Context) (Image, error)
	// RecentLogs returns logcat entries at or after `since`, filtered to
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
