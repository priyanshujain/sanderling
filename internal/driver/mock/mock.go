package mock

import (
	"context"
	"sync"
	"time"

	"github.com/priyanshujain/uatu/internal/driver"
)

type ActionKind string

const (
	ActionLaunch      ActionKind = "launch"
	ActionTerminate   ActionKind = "terminate"
	ActionTap         ActionKind = "tap"
	ActionTapSelector ActionKind = "tap_selector"
	ActionInputText   ActionKind = "input_text"
	ActionHierarchy   ActionKind = "hierarchy"
	ActionScreenshot  ActionKind = "screenshot"
	ActionWaitForIdle ActionKind = "wait_for_idle"
	ActionHealth      ActionKind = "health"
)

type Action struct {
	Kind       ActionKind
	BundleID   string
	ClearState bool
	X, Y       int
	Selector   string
	Text       string
	Idle       time.Duration
}

// Driver is an in-memory Driver implementation for unit tests.
// Tests can program HierarchyJSON, ImageData, HealthInfo, and per-method
// Failures, and read back Actions to assert what the runner asked for.
type Driver struct {
	mutex   sync.Mutex
	actions []Action

	HierarchyJSON string
	ImageData     driver.Image
	HealthInfo    driver.Health
	Failures      map[ActionKind]error
}

func New() *Driver {
	return &Driver{
		Failures: map[ActionKind]error{},
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
	return d.Failures[kind]
}

func (d *Driver) Launch(ctx context.Context, bundleID string, clearState bool) error {
	if err := d.failure(ActionLaunch); err != nil {
		return err
	}
	d.record(Action{Kind: ActionLaunch, BundleID: bundleID, ClearState: clearState})
	return nil
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

func (d *Driver) TapSelector(ctx context.Context, selector string) error {
	if err := d.failure(ActionTapSelector); err != nil {
		return err
	}
	d.record(Action{Kind: ActionTapSelector, Selector: selector})
	return nil
}

func (d *Driver) InputText(ctx context.Context, text string) error {
	if err := d.failure(ActionInputText); err != nil {
		return err
	}
	d.record(Action{Kind: ActionInputText, Text: text})
	return nil
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

var _ driver.Driver = (*Driver)(nil)
