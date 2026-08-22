// Package mock provides an in-memory device driver that records actions for tests.
package mock

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/priyanshujain/sanderling/internal/driver"
)

type ActionKind string

const (
	ActionLaunch            ActionKind = "launch"
	ActionTerminate         ActionKind = "terminate"
	ActionTap               ActionKind = "tap"
	ActionTapSelector       ActionKind = "tap_selector"
	ActionDoubleTap         ActionKind = "double_tap"
	ActionDoubleTapSelector ActionKind = "double_tap_selector"
	ActionInputText         ActionKind = "input_text"
	ActionEraseText         ActionKind = "erase_text"
	ActionSwipe             ActionKind = "swipe"
	ActionPressKey          ActionKind = "press_key"
	ActionLongPress         ActionKind = "long_press"
	ActionHierarchy         ActionKind = "hierarchy"
	ActionScreenshot        ActionKind = "screenshot"
	ActionSnapshot          ActionKind = "snapshot"
	ActionRecentLogs        ActionKind = "recent_logs"
	ActionWaitForIdle       ActionKind = "wait_for_idle"
	ActionHealth            ActionKind = "health"
	ActionMetrics           ActionKind = "metrics"
)

type Action struct {
	Kind           ActionKind
	BundleID       string
	ClearState     bool
	X, Y           int
	FromX, FromY   int
	ToX, ToY       int
	Duration       time.Duration
	Selector       string
	Text           string
	CharacterCount int
	Key            string
	LogLevel       string
	LogSince       time.Time
	Idle           time.Duration
}

// FailurePlan makes one method fail. OnCalls names the 1-based calls that fail
// and every other call succeeds; an empty OnCalls fails every call. It exists
// because "the first read times out and the next one works" is the shape of
// nearly every device fault the runner has to survive, and expressing it by
// embedding the mock in a one-off wrapper puts a different seven-line method in
// every test that needs one.
type FailurePlan struct {
	Err     error
	OnCalls []int
}

// Driver is an in-memory Driver implementation for unit tests.
// Tests can program HierarchyJSON, ImageData, HealthInfo, and per-method
// Failures, and read back Actions to assert what the runner asked for.
type Driver struct {
	mutex      sync.Mutex
	actions    []Action
	callCounts map[ActionKind]int

	HierarchyJSON string
	ImageData     driver.Image
	HealthInfo    driver.Health
	LogEntries    []driver.LogEntry
	MetricsData   driver.Metrics
	Failures      map[ActionKind]FailurePlan

	// ReplacesText makes the mock assert the TextReplacer capability, so
	// tests cover both the erase-before-type and replace-on-input paths.
	ReplacesText bool

	// ForegroundResults is consumed one entry per ForegroundApp call (the
	// last entry repeats). Empty yields "", which disables the runner's
	// app-scope guard so tests that don't care are unaffected.
	ForegroundResults []string
	foregroundIndex   int

	// ForegroundErr and FocusedWindowErr, when set, make the respective check
	// return that error so tests can cover the guard's transient-read paths.
	ForegroundErr    error
	FocusedWindowErr error

	// FocusedWindowResults is consumed one entry per FocusedWindowApp call
	// (the last entry repeats). When empty, FocusedWindowApp mirrors the
	// last ForegroundApp result, so the startup gate treats the window as
	// already drawn and tests that don't care are unaffected.
	FocusedWindowResults []string
	focusedWindowIndex   int
	focusedWindowCalls   int
	lastForeground       string
}

func New() *Driver {
	return &Driver{
		Failures:   map[ActionKind]FailurePlan{},
		callCounts: map[ActionKind]int{},
		HealthInfo: driver.Health{
			Ready:    true,
			Version:  "mock",
			Platform: "android",
		},
		HierarchyJSON: `{"children":[]}`,
		ImageData:     driver.Image{PNG: []byte{}, Width: 0, Height: 0},
	}
}

func (d *Driver) Actions() []Action {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return append([]Action(nil), d.actions...)
}

func (d *Driver) record(action Action) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.actions = append(d.actions, action)
}

func (d *Driver) failure(kind ActionKind) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.callCounts[kind]++
	plan, planned := d.Failures[kind]
	if !planned {
		return nil
	}
	if len(plan.OnCalls) == 0 || slices.Contains(plan.OnCalls, d.callCounts[kind]) {
		return plan.Err
	}
	return nil
}

func (d *Driver) Launch(_ context.Context, bundleID string, clearState bool, _ map[string]string) error {
	if err := d.failure(ActionLaunch); err != nil {
		return err
	}
	d.record(Action{Kind: ActionLaunch, BundleID: bundleID, ClearState: clearState})
	return nil
}

func (d *Driver) ForegroundApp(_ context.Context) (string, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.ForegroundErr != nil {
		return "", d.ForegroundErr
	}
	if len(d.ForegroundResults) == 0 {
		d.lastForeground = ""
		return "", nil
	}
	index := d.foregroundIndex
	if index >= len(d.ForegroundResults) {
		index = len(d.ForegroundResults) - 1
	}
	d.foregroundIndex++
	d.lastForeground = d.ForegroundResults[index]
	return d.lastForeground, nil
}

func (d *Driver) FocusedWindowApp(_ context.Context) (string, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.focusedWindowCalls++
	if d.FocusedWindowErr != nil {
		return "", d.FocusedWindowErr
	}
	if len(d.FocusedWindowResults) == 0 {
		return d.lastForeground, nil
	}
	index := d.focusedWindowIndex
	if index >= len(d.FocusedWindowResults) {
		index = len(d.FocusedWindowResults) - 1
	}
	d.focusedWindowIndex++
	return d.FocusedWindowResults[index], nil
}

// FocusedWindowCalls reports how many times FocusedWindowApp has been called.
func (d *Driver) FocusedWindowCalls() int {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.focusedWindowCalls
}

func (d *Driver) Terminate(ctx context.Context) error {
	if err := d.failure(ActionTerminate); err != nil {
		return err
	}
	d.record(Action{Kind: ActionTerminate})
	return nil
}

func (d *Driver) Tap(ctx context.Context, x, y int) error {
	if err := d.failure(ActionTap); err != nil {
		return err
	}
	d.record(Action{Kind: ActionTap, X: x, Y: y})
	return nil
}

func (d *Driver) LongPress(ctx context.Context, x, y int) error {
	if err := d.failure(ActionLongPress); err != nil {
		return err
	}
	d.record(Action{Kind: ActionLongPress, X: x, Y: y})
	return nil
}

func (d *Driver) TapSelector(ctx context.Context, selector string) error {
	if err := d.failure(ActionTapSelector); err != nil {
		return err
	}
	d.record(Action{Kind: ActionTapSelector, Selector: selector})
	return nil
}

func (d *Driver) DoubleTap(ctx context.Context, x, y int) error {
	if err := d.failure(ActionDoubleTap); err != nil {
		return err
	}
	d.record(Action{Kind: ActionDoubleTap, X: x, Y: y})
	return nil
}

func (d *Driver) DoubleTapSelector(ctx context.Context, selector string) error {
	if err := d.failure(ActionDoubleTapSelector); err != nil {
		return err
	}
	d.record(Action{Kind: ActionDoubleTapSelector, Selector: selector})
	return nil
}

func (d *Driver) InputText(ctx context.Context, text string) error {
	if err := d.failure(ActionInputText); err != nil {
		return err
	}
	d.record(Action{Kind: ActionInputText, Text: text})
	return nil
}

func (d *Driver) ReplacesTextOnInput() bool {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.ReplacesText
}

func (d *Driver) EraseText(ctx context.Context, characterCount int) error {
	if err := d.failure(ActionEraseText); err != nil {
		return err
	}
	d.record(Action{Kind: ActionEraseText, CharacterCount: characterCount})
	return nil
}

func (d *Driver) Swipe(ctx context.Context, fromX, fromY, toX, toY int, duration time.Duration) error {
	if err := d.failure(ActionSwipe); err != nil {
		return err
	}
	d.record(Action{
		Kind:     ActionSwipe,
		FromX:    fromX,
		FromY:    fromY,
		ToX:      toX,
		ToY:      toY,
		Duration: duration,
	})
	return nil
}

func (d *Driver) PressKey(ctx context.Context, key string) error {
	if err := d.failure(ActionPressKey); err != nil {
		return err
	}
	d.record(Action{Kind: ActionPressKey, Key: key})
	return nil
}

func (d *Driver) RecentLogs(ctx context.Context, since time.Time, minLevel string) ([]driver.LogEntry, error) {
	if err := d.failure(ActionRecentLogs); err != nil {
		return nil, err
	}
	d.record(Action{Kind: ActionRecentLogs, LogSince: since, LogLevel: minLevel})
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return append([]driver.LogEntry(nil), d.LogEntries...), nil
}

func (d *Driver) Hierarchy(ctx context.Context) (string, error) {
	if err := d.failure(ActionHierarchy); err != nil {
		return "", err
	}
	d.record(Action{Kind: ActionHierarchy})
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.HierarchyJSON, nil
}

func (d *Driver) Screenshot(ctx context.Context) (driver.Image, error) {
	if err := d.failure(ActionScreenshot); err != nil {
		return driver.Image{}, err
	}
	d.record(Action{Kind: ActionScreenshot})
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.ImageData, nil
}

// Snapshot returns the hierarchy + screenshot pair atomically, mirroring
// the real driver's contract. It records a single ActionSnapshot so tests
// can assert the runner reached for the paired RPC instead of racing the
// two reads.
func (d *Driver) Snapshot(ctx context.Context) (string, driver.Image, error) {
	if err := d.failure(ActionSnapshot); err != nil {
		return "", driver.Image{}, err
	}
	d.record(Action{Kind: ActionSnapshot})
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.HierarchyJSON, d.ImageData, nil
}

func (d *Driver) WaitForIdle(ctx context.Context, duration time.Duration) error {
	if err := d.failure(ActionWaitForIdle); err != nil {
		return err
	}
	d.record(Action{Kind: ActionWaitForIdle, Idle: duration})
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (d *Driver) Health(ctx context.Context) (driver.Health, error) {
	if err := d.failure(ActionHealth); err != nil {
		return driver.Health{}, err
	}
	d.record(Action{Kind: ActionHealth})
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.HealthInfo, nil
}

func (d *Driver) Metrics(ctx context.Context, bundleID string) (driver.Metrics, error) {
	if err := d.failure(ActionMetrics); err != nil {
		return driver.Metrics{}, err
	}
	d.record(Action{Kind: ActionMetrics, BundleID: bundleID})
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.MetricsData, nil
}

var _ driver.DeviceDriver = (*Driver)(nil)
