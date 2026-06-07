package transport

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"sync"
	"time"
)

// deadlineImmediate is a deadline already in the past, used to interrupt a
// blocked read or write when the context is cancelled.
var deadlineImmediate = time.Unix(1, 0)

// runnerCompanion speaks newline-delimited JSON over a single persistent TCP
// connection to the in-simulator runner. One request is in flight at a time:
// the mutex serializes call/response pairs over the shared connection.
type runnerCompanion struct {
	uniqueDeviceIdentifier string
	bundleID               string

	mutex  sync.Mutex
	conn   net.Conn
	reader *bufio.Reader
	nextID int
}

// DialRunner opens one persistent TCP connection to the simulator runner at
// address and returns a Companion that also implements TextEditor. The
// uniqueDeviceIdentifier targets simctl shell-outs; bundleID is the app the
// snapshot and app-state queries default to.
func DialRunner(address, uniqueDeviceIdentifier, bundleID string) (Companion, error) {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("runner transport: %w: dial %s: %v", ErrCompanionUnavailable, address, err)
	}
	return &runnerCompanion{
		uniqueDeviceIdentifier: uniqueDeviceIdentifier,
		bundleID:               bundleID,
		conn:                   conn,
		reader:                 bufio.NewReader(conn),
	}, nil
}

func (c *runnerCompanion) Close() error { return c.conn.Close() }

// runnerRequest and runnerResponse are the wire envelopes. A response carries
// either result or error, never both.
type runnerRequest struct {
	ID     int            `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

type runnerResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

// call sends one request and returns its result payload. Transport-level
// failures wrap ErrCompanionUnavailable; a server-reported error does not.
func (c *runnerCompanion) call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if params == nil {
		params = map[string]any{}
	}
	c.nextID++
	id := c.nextID

	// A blocked read or write cannot observe context cancellation directly, so
	// AfterFunc trips an immediate deadline to interrupt it. The deadline is
	// cleared once the call returns so the next call starts fresh.
	stop := context.AfterFunc(ctx, func() {
		c.conn.SetDeadline(deadlineImmediate)
	})
	// Clearing the deadline must happen after stop() so a late-firing AfterFunc
	// cannot re-arm the deadline once the call has completed. Defers run LIFO.
	defer c.conn.SetDeadline(time.Time{})
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		c.conn.SetDeadline(deadline)
	}

	payload, err := json.Marshal(runnerRequest{ID: id, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("runner transport: %w: marshal %s request: %v", ErrCompanionUnavailable, method, err)
	}
	payload = append(payload, '\n')
	if _, err := c.conn.Write(payload); err != nil {
		return nil, wrapTransport(ctx, "write", method, err)
	}

	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		return nil, wrapTransport(ctx, "read", method, err)
	}

	var response runnerResponse
	if err := json.Unmarshal(line, &response); err != nil {
		return nil, fmt.Errorf("runner transport: %w: decode %s response: %v", ErrCompanionUnavailable, method, err)
	}
	if response.ID != id {
		return nil, fmt.Errorf("runner transport: %w: response id %d does not match request id %d", ErrCompanionUnavailable, response.ID, id)
	}
	if response.Error != "" {
		return nil, fmt.Errorf("runner %s: %s", method, response.Error)
	}
	return response.Result, nil
}

// wrapTransport classifies a read/write failure. A cancelled or expired context
// is reported as such so callers see why the call was interrupted; either way
// the error wraps ErrCompanionUnavailable.
func wrapTransport(ctx context.Context, stage, method string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("runner transport: %w: %s %s interrupted: %v", ErrCompanionUnavailable, stage, method, ctxErr)
	}
	return fmt.Errorf("runner transport: %w: %s %s: %v", ErrCompanionUnavailable, stage, method, err)
}

func (c *runnerCompanion) AccessibilityInfo(ctx context.Context) (string, error) {
	result, err := c.call(ctx, "snapshot", map[string]any{"bundleId": c.bundleID})
	if err != nil {
		return "", err
	}
	var payload struct {
		Elements []json.RawMessage `json:"elements"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return "", fmt.Errorf("runner transport: %w: decode snapshot elements: %v", ErrCompanionUnavailable, err)
	}
	elements, err := json.Marshal(payload.Elements)
	if err != nil {
		return "", fmt.Errorf("runner transport: %w: re-marshal snapshot elements: %v", ErrCompanionUnavailable, err)
	}
	return string(elements), nil
}

func (c *runnerCompanion) Describe(ctx context.Context) (ScreenDescription, error) {
	result, err := c.call(ctx, "describe", nil)
	if err != nil {
		return ScreenDescription{}, err
	}
	var payload struct {
		WidthPoints  int     `json:"widthPoints"`
		HeightPoints int     `json:"heightPoints"`
		Scale        float64 `json:"scale"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return ScreenDescription{}, fmt.Errorf("runner transport: %w: decode describe response: %v", ErrCompanionUnavailable, err)
	}
	return ScreenDescription{
		WidthPoints:  payload.WidthPoints,
		HeightPoints: payload.HeightPoints,
		Scale:        payload.Scale,
	}, nil
}

func (c *runnerCompanion) SendHID(ctx context.Context, events ...HIDEvent) error {
	encoded := make([]map[string]any, 0, len(events))
	for _, event := range events {
		object, err := hidEventToObject(event)
		if err != nil {
			return err
		}
		encoded = append(encoded, object)
	}
	_, err := c.call(ctx, "gesture", map[string]any{"events": encoded})
	return err
}

// hidEventToObject encodes a neutral HID event as a runner gesture event. The
// runner edits text natively, so keyboard HID events are rejected with an
// ordinary error rather than a connection-level one.
func hidEventToObject(event HIDEvent) (map[string]any, error) {
	switch event.Kind {
	case HIDKindTouchDown:
		return map[string]any{"kind": "touchDown", "x": event.X, "y": event.Y}, nil
	case HIDKindTouchUp:
		return map[string]any{"kind": "touchUp", "x": event.X, "y": event.Y}, nil
	case HIDKindDelay:
		return map[string]any{"kind": "delay", "milliseconds": event.Milliseconds}, nil
	case HIDKindSwipe:
		return map[string]any{
			"kind":    "swipe",
			"fromX":   event.FromX,
			"fromY":   event.FromY,
			"toX":     event.ToX,
			"toY":     event.ToY,
			"seconds": event.Seconds,
		}, nil
	case HIDKindKeyDown, HIDKindKeyUp:
		return nil, errors.New("runner companion does not synthesize keyboard HID events; route text through the text editor")
	}
	return nil, fmt.Errorf("unknown HID event kind %d", event.Kind)
}

func (c *runnerCompanion) Screenshot(ctx context.Context) ([]byte, string, error) {
	result, err := c.call(ctx, "screenshot", nil)
	if err != nil {
		return nil, "", err
	}
	var payload struct {
		PNGBase64 string `json:"pngBase64"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, "", fmt.Errorf("runner transport: %w: decode screenshot response: %v", ErrCompanionUnavailable, err)
	}
	data, err := base64.StdEncoding.DecodeString(payload.PNGBase64)
	if err != nil {
		return nil, "", fmt.Errorf("runner transport: %w: decode screenshot png: %v", ErrCompanionUnavailable, err)
	}
	return data, "png", nil
}

func (c *runnerCompanion) Launch(ctx context.Context, bundleID string, foregroundIfRunning bool) error {
	_, err := c.call(ctx, "launch", map[string]any{
		"bundleId":            bundleID,
		"foregroundIfRunning": foregroundIfRunning,
	})
	return err
}

func (c *runnerCompanion) Terminate(ctx context.Context, bundleID string) error {
	_, err := c.call(ctx, "terminate", map[string]any{"bundleId": bundleID})
	return err
}

func (c *runnerCompanion) ListApps(ctx context.Context) ([]InstalledApp, error) {
	result, err := c.call(ctx, "appState", map[string]any{"bundleId": c.bundleID})
	if err != nil {
		return nil, err
	}
	var payload struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("runner transport: %w: decode appState response: %v", ErrCompanionUnavailable, err)
	}
	return []InstalledApp{{
		BundleID:     c.bundleID,
		InstallType:  "user",
		ProcessState: processStateFromAppState(payload.State),
	}}, nil
}

func processStateFromAppState(state string) ProcessState {
	switch state {
	case "foreground", "background":
		return ProcessStateRunning
	case "notRunning":
		return ProcessStateNotRunning
	default:
		return ProcessStateUnknown
	}
}

// Install and Uninstall shell out to simctl. The driver does not call these
// today; they complete the Companion interface.
func (c *runnerCompanion) Install(ctx context.Context, appPath string) error {
	command := exec.CommandContext(ctx, "xcrun", "simctl", "install", c.uniqueDeviceIdentifier, appPath)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("simctl install: %v: %s", err, output)
	}
	return nil
}

func (c *runnerCompanion) Uninstall(ctx context.Context, bundleID string) error {
	command := exec.CommandContext(ctx, "xcrun", "simctl", "uninstall", c.uniqueDeviceIdentifier, bundleID)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("simctl uninstall: %v: %s", err, output)
	}
	return nil
}

func (c *runnerCompanion) InputText(ctx context.Context, text string) error {
	return c.TypeText(ctx, text, true)
}

func (c *runnerCompanion) TypeText(ctx context.Context, text string, replace bool) error {
	_, err := c.call(ctx, "typeText", map[string]any{"text": text, "replace": replace})
	return err
}

func (c *runnerCompanion) EraseText(ctx context.Context, characterCount int) error {
	_, err := c.call(ctx, "eraseText", map[string]any{"count": characterCount})
	return err
}

func (c *runnerCompanion) PressKey(ctx context.Context, key string) error {
	switch key {
	case "enter", "return", "Enter", "Return":
		_, err := c.call(ctx, "pressKey", map[string]any{"key": "return"})
		return err
	default:
		return fmt.Errorf("runner companion cannot press key %q; only return is supported", key)
	}
}
