// Package transport is the only layer that touches the generated companion
// gRPC stubs. Everything above it speaks the brand-free Companion interface,
// so the companion binary stays swappable behind this boundary.
package transport

import (
	"context"
	"errors"

	pb "github.com/priyanshujain/sanderling/internal/driver/ioscompanion/companionpb"
)

// Companion drives a single booted iOS simulator through the companion.
// Callers own per-call deadlines by passing a context.
type Companion interface {
	// AccessibilityInfo returns the describe-all accessibility tree as the
	// raw flat-format JSON string the companion emits.
	AccessibilityInfo(ctx context.Context) (string, error)

	// Describe reports the target's screen dimensions in points, along with
	// the pixel scale when the companion supplies one.
	Describe(ctx context.Context) (ScreenDescription, error)

	// SendHID opens the HID stream, sends every event in order, then closes.
	SendHID(ctx context.Context, events ...HIDEvent) error

	// Screenshot captures the current screen, returning the encoded image
	// bytes and the image format string the companion reports.
	Screenshot(ctx context.Context) (imageData []byte, imageFormat string, err error)

	// Launch brings the app to the foreground, starting it if needed.
	Launch(ctx context.Context, bundleID string, foregroundIfRunning bool) error

	// Terminate stops the running app with the given bundle identifier.
	Terminate(ctx context.Context, bundleID string) error

	// ListApps reports every installed app and its current process state.
	ListApps(ctx context.Context) ([]InstalledApp, error)

	// Install installs a .app bundle directory by streaming it to the
	// companion.
	Install(ctx context.Context, appPath string) error

	// Uninstall removes the app with the given bundle identifier.
	Uninstall(ctx context.Context, bundleID string) error

	// Close releases the underlying connection.
	Close() error
}

// TextEditor is an optional companion capability: the transport edits text
// natively on the device instead of the driver composing keyboard HID streams.
// The driver routes text input through it when the companion implements it.
type TextEditor interface {
	// InputText replaces the focused field's content with text.
	InputText(ctx context.Context, text string) error

	// EraseText deletes characterCount characters from the focused field.
	EraseText(ctx context.Context, characterCount int) error

	// PressKey presses the named logical key (return/enter and escape).
	PressKey(ctx context.Context, key string) error
}

// TextTyper is an optional companion capability: the transport types text
// natively into whatever holds keyboard focus. Unlike TextEditor it exposes
// the replace flag, so a caller can clear the field through another channel
// and append with replace false.
type TextTyper interface {
	TypeText(ctx context.Context, text string, replace bool) error
}

// ErrCompanionUnavailable marks a connection-level failure that a companion
// restart can recover from. Transports wrap dropped-connection errors with it.
var ErrCompanionUnavailable = errors.New("companion connection unavailable")

// ScreenDescription carries the target screen geometry. Width and Height are
// in points (the coordinate space HID events and the accessibility frames use).
// Scale is the pixel-per-point density, or 0 when the companion did not report
// one.
type ScreenDescription struct {
	WidthPoints  int
	HeightPoints int
	Scale        float64
}

// ProcessState mirrors the companion's notion of whether an app is running.
type ProcessState int

const (
	ProcessStateUnknown ProcessState = iota
	ProcessStateNotRunning
	ProcessStateRunning
)

// InstalledApp describes one installed app. ProcessState and ProcessIdentifier
// together answer whether the app is currently running and in the foreground.
type InstalledApp struct {
	BundleID          string
	Name              string
	InstallType       string
	ProcessState      ProcessState
	Debuggable        bool
	ProcessIdentifier uint64
}

func processStateFromProto(s pb.InstalledAppInfo_AppProcessState) ProcessState {
	switch s {
	case pb.InstalledAppInfo_RUNNING:
		return ProcessStateRunning
	case pb.InstalledAppInfo_NOT_RUNNING:
		return ProcessStateNotRunning
	default:
		return ProcessStateUnknown
	}
}
