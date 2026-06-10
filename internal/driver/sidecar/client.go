// Package sidecar implements the device driver by talking to the native sidecar over gRPC.
package sidecar

import (
	"context"
	"fmt"
	"io"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/priyanshujain/sanderling/internal/android"
	"github.com/priyanshujain/sanderling/internal/driver"
	driverpb "github.com/priyanshujain/sanderling/proto/driverpb"
)

type Client struct {
	connection *grpc.ClientConn
	stub       driverpb.DriverClient
	platform   string

	// serial, apkPath, and output drive the Android clear-state reinstall: when
	// apkPath is set, Launch resets state by uninstalling and reinstalling the
	// APK instead of asking the sidecar to `pm clear`, which hardened OEM builds
	// deny. Empty apkPath leaves the legacy `pm clear` path in place.
	serial  string
	apkPath string
	output  io.Writer

	// reinstallApp resets an Android app to first-launch state. A seam so tests
	// exercise the clear-state branch without a connected device; defaults to
	// android.ReinstallApp.
	reinstallApp func(ctx context.Context, serial, bundleID, apkPath string, output io.Writer) error
}

// SetPlatform records the target platform so capability methods (e.g.
// ForegroundApp) can pick the right backend. The caller sets this right after
// Dial.
func (c *Client) SetPlatform(platform string) { c.platform = platform }

// SetClearStateReinstall makes Android clear-state reset the app by reinstalling
// the APK at apkPath (uninstall+install) rather than `pm clear`. serial targets
// the device when several are connected; output receives progress lines.
func (c *Client) SetClearStateReinstall(serial, apkPath string, output io.Writer) {
	c.serial = serial
	c.apkPath = apkPath
	c.output = output
}

// ForegroundApp reports the foreground package. Only Android is supported (via
// adb); other platforms return "" so the runner skips app-scope enforcement.
func (c *Client) ForegroundApp(ctx context.Context) (string, error) {
	if c.platform != "android" {
		return "", nil
	}
	return android.ForegroundPackage(ctx, c.serial)
}

// FocusedWindowApp reports the package owning the focused window. Only Android
// is supported (via adb); other platforms return "" so the startup gate falls
// back to the foreground-app signal.
func (c *Client) FocusedWindowApp(ctx context.Context) (string, error) {
	if c.platform != "android" {
		return "", nil
	}
	return android.FocusedWindowPackage(ctx, c.serial)
}

// Dial connects to the sidecar gRPC server at the given address.
// Address must be a host:port pair, typically "127.0.0.1:<sidecar-port>".
func Dial(address string) (*Client, error) {
	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial sidecar: %w", err)
	}
	return &Client{
		connection:   connection,
		stub:         driverpb.NewDriverClient(connection),
		reinstallApp: android.ReinstallApp,
	}, nil
}

func (c *Client) Close() error { return c.connection.Close() }

// WaitForHealth polls the sidecar's Health RPC until it returns Ready=true
// or the context is canceled.
func (c *Client) WaitForHealth(ctx context.Context, pollInterval time.Duration) error {
	if pollInterval <= 0 {
		pollInterval = 100 * time.Millisecond
	}
	for {
		response, err := c.stub.Health(ctx, &driverpb.Empty{})
		if err == nil && response.GetReady() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func (c *Client) Launch(ctx context.Context, bundleID string, clearState bool, env map[string]string) error {
	sidecarClearState := clearState
	if clearState && c.platform == "android" && c.apkPath != "" {
		if c.output != nil {
			fmt.Fprintf(c.output, "clear-state: reinstalling %s from %s\n", bundleID, c.apkPath)
		}
		if err := c.reinstallApp(ctx, c.serial, bundleID, c.apkPath, c.output); err != nil {
			return fmt.Errorf("clear-state reinstall: %w", err)
		}
		sidecarClearState = false
	}
	_, err := c.stub.Launch(ctx, &driverpb.LaunchRequest{
		BundleId:   bundleID,
		ClearState: sidecarClearState,
		Env:        env,
	})
	return err
}

func (c *Client) Terminate(ctx context.Context) error {
	_, err := c.stub.Terminate(ctx, &driverpb.Empty{})
	return err
}

func (c *Client) Tap(ctx context.Context, x, y int) error {
	_, err := c.stub.Tap(ctx, &driverpb.Point{X: int32(x), Y: int32(y)})
	return err
}

func (c *Client) LongPress(ctx context.Context, x, y int) error {
	_, err := c.stub.LongPress(ctx, &driverpb.Point{X: int32(x), Y: int32(y)})
	return err
}

func (c *Client) TapSelector(ctx context.Context, selector string) error {
	_, err := c.stub.TapSelector(ctx, &driverpb.Selector{Value: selector})
	return err
}

// doubleTapGap is the inter-tap delay for the selector fallback: short enough
// to land both events inside a sub-100 ms race window, long enough for the
// sidecar to serialize two MotionEvent streams.
const doubleTapGap = 50 * time.Millisecond

// DoubleTap dispatches the native RPC so the backend can land both taps as
// close together as the platform allows. Composing two Tap round trips from
// here spreads them by hundreds of milliseconds on iOS, wide enough for
// navigation to interleave between the taps.
func (c *Client) DoubleTap(ctx context.Context, x, y int) error {
	_, err := c.stub.DoubleTap(ctx, &driverpb.Point{X: int32(x), Y: int32(y)})
	return err
}

func (c *Client) DoubleTapSelector(ctx context.Context, selector string) error {
	return doubleTap(ctx, func() error { return c.TapSelector(ctx, selector) })
}

func doubleTap(ctx context.Context, tap func() error) error {
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

func (c *Client) InputText(ctx context.Context, text string) error {
	_, err := c.stub.InputText(ctx, &driverpb.Text{Value: text})
	return err
}

func (c *Client) EraseText(ctx context.Context, characterCount int) error {
	_, err := c.stub.EraseText(ctx, &driverpb.EraseTextRequest{CharacterCount: int32(characterCount)})
	return err
}

func (c *Client) Swipe(ctx context.Context, fromX, fromY, toX, toY int, duration time.Duration) error {
	_, err := c.stub.Swipe(ctx, &driverpb.SwipeRequest{
		From:           &driverpb.Point{X: int32(fromX), Y: int32(fromY)},
		To:             &driverpb.Point{X: int32(toX), Y: int32(toY)},
		DurationMillis: duration.Milliseconds(),
	})
	return err
}

func (c *Client) PressKey(ctx context.Context, key string) error {
	_, err := c.stub.PressKey(ctx, &driverpb.PressKeyRequest{Key: key})
	return err
}

func (c *Client) RecentLogs(ctx context.Context, since time.Time, minLevel string) ([]driver.LogEntry, error) {
	sinceMillis := int64(0)
	if !since.IsZero() {
		sinceMillis = since.UnixMilli()
	}
	response, err := c.stub.RecentLogs(ctx, &driverpb.RecentLogsRequest{
		SinceUnixMillis: sinceMillis,
		LevelAtLeast:    minLevel,
	})
	if err != nil {
		return nil, err
	}
	entries := response.GetEntries()
	result := make([]driver.LogEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, driver.LogEntry{
			UnixMillis: entry.GetUnixMillis(),
			Level:      entry.GetLevel(),
			Tag:        entry.GetTag(),
			Message:    entry.GetMessage(),
		})
	}
	return result, nil
}

func (c *Client) Hierarchy(ctx context.Context) (string, error) {
	response, err := c.stub.Hierarchy(ctx, &driverpb.Empty{})
	if err != nil {
		return "", err
	}
	return response.GetJson(), nil
}

func (c *Client) Screenshot(ctx context.Context) (driver.Image, error) {
	response, err := c.stub.Screenshot(ctx, &driverpb.Empty{})
	if err != nil {
		return driver.Image{}, err
	}
	return driver.Image{
		PNG:    response.GetPng(),
		Width:  int(response.GetWidth()),
		Height: int(response.GetHeight()),
	}, nil
}

// Snapshot fetches hierarchy and screenshot in a single sidecar round-trip.
// The sidecar serializes the two reads behind a mutex so the returned pair
// describes the same on-device frame, removing the cross-fade race the
// runner used to see when fetching them as independent goroutines.
func (c *Client) Snapshot(ctx context.Context) (string, driver.Image, error) {
	response, err := c.stub.Snapshot(ctx, &driverpb.Empty{})
	if err != nil {
		return "", driver.Image{}, err
	}
	image := response.GetScreenshot()
	return response.GetHierarchy().GetJson(), driver.Image{
		PNG:    image.GetPng(),
		Width:  int(image.GetWidth()),
		Height: int(image.GetHeight()),
	}, nil
}

func (c *Client) WaitForIdle(ctx context.Context, duration time.Duration) error {
	_, err := c.stub.WaitForIdle(ctx, &driverpb.Duration{Millis: duration.Milliseconds()})
	return err
}

func (c *Client) Health(ctx context.Context) (driver.Health, error) {
	response, err := c.stub.Health(ctx, &driverpb.Empty{})
	if err != nil {
		return driver.Health{}, err
	}
	return driver.Health{
		Ready:    response.GetReady(),
		Version:  response.GetVersion(),
		Platform: response.GetPlatform(),
	}, nil
}

func (c *Client) Metrics(ctx context.Context, bundleID string) (driver.Metrics, error) {
	response, err := c.stub.Metrics(ctx, &driverpb.MetricsRequest{BundleId: bundleID})
	if err != nil {
		return driver.Metrics{}, err
	}
	return driver.Metrics{
		CPUPercent:       response.GetCpuPercent(),
		HeapBytes:        response.GetHeapBytes(),
		TotalMemoryBytes: response.GetTotalMemoryBytes(),
	}, nil
}

var _ driver.DeviceDriver = (*Client)(nil)
