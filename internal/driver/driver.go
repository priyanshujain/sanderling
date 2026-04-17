package driver

import (
	"context"
	"time"
)

// Driver abstracts the platform-specific UI automation backend. v0.1 surface
// matches proto/driverpb/driver.proto. The Maestro sidecar implementation
// lives under driver/maestro; tests use driver/mock.
type Driver interface {
	Launch(ctx context.Context, bundleID string, clearState bool) error
	Terminate(ctx context.Context) error

	Tap(ctx context.Context, x, y int) error
	InputText(ctx context.Context, text string) error

	Hierarchy(ctx context.Context) (string, error)
	Screenshot(ctx context.Context) (Image, error)

	WaitForIdle(ctx context.Context, duration time.Duration) error
	Health(ctx context.Context) (Health, error)
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
