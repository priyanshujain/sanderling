package ioscompanion

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/priyanshujain/sanderling/internal/driver"
	"github.com/priyanshujain/sanderling/internal/driver/ioscompanion/transport"
)

// fakeCompanion is an in-package stand-in for transport.Companion. It records
// the order of calls and returns scripted results, so the driver's decision
// logic is testable without a live simulator.
type fakeCompanion struct {
	calls []string

	accessibilityJSON string
	accessibilityErr  error
	screenshotData    []byte
	describe          transport.ScreenDescription
	apps              []transport.InstalledApp

	// hidErr fires on the next SendHID then clears, letting a test inject one
	// transient failure.
	hidErr error
}

func (f *fakeCompanion) record(name string) { f.calls = append(f.calls, name) }

func (f *fakeCompanion) AccessibilityInfo(context.Context) (string, error) {
	f.record("accessibility")
	return f.accessibilityJSON, f.accessibilityErr
}

func (f *fakeCompanion) Describe(context.Context) (transport.ScreenDescription, error) {
	f.record("describe")
	return f.describe, nil
}

func (f *fakeCompanion) SendHID(context.Context, ...transport.HIDEvent) error {
	f.record("hid")
	if f.hidErr != nil {
		err := f.hidErr
		f.hidErr = nil
		return err
	}
	return nil
}

func (f *fakeCompanion) Screenshot(context.Context) ([]byte, string, error) {
	f.record("screenshot")
	return f.screenshotData, "", nil
}

func (f *fakeCompanion) Launch(_ context.Context, _ string, _ bool) error {
	f.record("launch")
	return nil
}

func (f *fakeCompanion) Terminate(context.Context, string) error {
	f.record("terminate")
	return nil
}

func (f *fakeCompanion) ListApps(context.Context) ([]transport.InstalledApp, error) {
	f.record("listapps")
	return f.apps, nil
}

func (f *fakeCompanion) Install(context.Context, string) error {
	f.record("install")
	return nil
}

func (f *fakeCompanion) Uninstall(context.Context, string) error {
	f.record("uninstall")
	return nil
}

func (f *fakeCompanion) Close() error {
	f.record("close")
	return nil
}

var _ transport.Companion = (*fakeCompanion)(nil)

// newTestDriver returns a Driver wired to companion with no child process and a
// no-op container reset, so tests never touch the filesystem or spawn anything.
func newTestDriver(companion transport.Companion) *Driver {
	d := &Driver{
		companion:                companion,
		bundleID:                 "com.example.app",
		output:                   &bytes.Buffer{},
		doubleTapGapMilliseconds: DefaultDoubleTapGapMilliseconds,
		screenWidth:              390,
		screenHeight:             844,
	}
	d.resetContainer = func(context.Context) error { return nil }
	return d
}

func samplePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 1, A: 255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buffer.Bytes()
}

func TestLaunchTerminatesFirstThenLaunches(t *testing.T) {
	companion := &fakeCompanion{accessibilityJSON: "[]"}
	d := newTestDriver(companion)
	if err := d.Launch(context.Background(), "", false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if companion.calls[0] != "terminate" {
		t.Fatalf("first call = %q, want terminate", companion.calls[0])
	}
	if indexOf(companion.calls, "launch") < indexOf(companion.calls, "terminate") {
		t.Fatalf("launch must follow terminate; got %v", companion.calls)
	}
}

func TestLaunchClearStateReinstallsWithAppPath(t *testing.T) {
	companion := &fakeCompanion{accessibilityJSON: "[]"}
	d := newTestDriver(companion)
	d.appPath = "/tmp/Sample.app"
	if err := d.Launch(context.Background(), "", true, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if indexOf(companion.calls, "uninstall") < 0 || indexOf(companion.calls, "install") < 0 {
		t.Fatalf("clear-state with app path must uninstall then install; got %v", companion.calls)
	}
	if indexOf(companion.calls, "uninstall") > indexOf(companion.calls, "install") {
		t.Fatalf("uninstall must precede install; got %v", companion.calls)
	}
}

func TestLaunchClearStateFallbackWarnsOnce(t *testing.T) {
	companion := &fakeCompanion{accessibilityJSON: "[]"}
	output := &bytes.Buffer{}
	d := newTestDriver(companion)
	d.output = output
	resets := 0
	d.resetContainer = func(context.Context) error { resets++; return nil }

	for i := 0; i < 2; i++ {
		if err := d.Launch(context.Background(), "", true, nil); err != nil {
			t.Fatalf("Launch %d: %v", i, err)
		}
	}
	if resets != 2 {
		t.Fatalf("resetContainer called %d times, want 2", resets)
	}
	warnings := strings.Count(output.String(), "resetting the data container only")
	if warnings != 1 {
		t.Fatalf("warning emitted %d times, want once", warnings)
	}
	for _, call := range companion.calls {
		if call == "install" || call == "uninstall" {
			t.Fatalf("fallback path must not install/uninstall; got %v", companion.calls)
		}
	}
}

func TestLaunchRejectsEnvironment(t *testing.T) {
	d := newTestDriver(&fakeCompanion{accessibilityJSON: "[]"})
	err := d.Launch(context.Background(), "", false, map[string]string{"K": "V"})
	if err == nil || !strings.Contains(err.Error(), "environment") {
		t.Fatalf("Launch with env: err = %v, want unsupported-environment error", err)
	}
}

func TestSnapshotPairsHierarchyThenScreenshot(t *testing.T) {
	companion := &fakeCompanion{
		accessibilityJSON: "[]",
		screenshotData:    samplePNG(t, 390, 844),
	}
	d := newTestDriver(companion)
	_, image, err := d.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if image.Width != 390 || image.Height != 844 {
		t.Fatalf("image dims = %dx%d, want 390x844", image.Width, image.Height)
	}
	if indexOf(companion.calls, "accessibility") > indexOf(companion.calls, "screenshot") {
		t.Fatalf("accessibility must precede screenshot; got %v", companion.calls)
	}
}

func TestScreenshotRejectsNonPNG(t *testing.T) {
	d := newTestDriver(&fakeCompanion{screenshotData: []byte("not a png")})
	if _, err := d.Screenshot(context.Background()); err == nil {
		t.Fatal("Screenshot of non-PNG should error")
	}
}

func TestInputTextFastPathSkipsFieldResolution(t *testing.T) {
	companion := &fakeCompanion{accessibilityJSON: "[]"}
	d := newTestDriver(companion)
	// "hello" is fully mappable, so the fast keyboard path runs and never reads
	// the accessibility dump for a field target.
	if err := d.InputText(context.Background(), "hello"); err != nil {
		t.Fatalf("InputText: %v", err)
	}
	if indexOf(companion.calls, "hid") < 0 {
		t.Fatalf("fast path must send HID; got %v", companion.calls)
	}
	if indexOf(companion.calls, "accessibility") >= 0 {
		t.Fatalf("fast path must not resolve a field via accessibility; got %v", companion.calls)
	}
}

func TestInputTextPasteboardResolvesFieldFromLastTap(t *testing.T) {
	// The dump carries one editable field whose frame contains the last tap.
	dump := `[{"type":"TextField","AXUniqueId":"field-1","frame":{"x":10,"y":100,"width":200,"height":40}}]`
	companion := &fakeCompanion{accessibilityJSON: dump, screenshotData: samplePNG(t, 10, 10)}
	d := newTestDriver(companion)
	// Tap inside the field so lastTap lands on it.
	if err := d.Tap(context.Background(), 50, 120); err != nil {
		t.Fatalf("Tap: %v", err)
	}
	field := d.resolveInputField(context.Background())
	if field.identifier != "field-1" {
		t.Fatalf("resolved field id = %q, want field-1", field.identifier)
	}
	if field.centerX != 110 || field.centerY != 120 {
		t.Fatalf("field center = (%v,%v), want (110,120)", field.centerX, field.centerY)
	}
}

func TestResolveInputFieldEmptyWhenTapOutsideAnyField(t *testing.T) {
	dump := `[{"type":"TextField","AXUniqueId":"field-1","frame":{"x":10,"y":100,"width":200,"height":40}}]`
	d := newTestDriver(&fakeCompanion{accessibilityJSON: dump})
	if err := d.Tap(context.Background(), 5, 5); err != nil {
		t.Fatalf("Tap: %v", err)
	}
	if field := d.resolveInputField(context.Background()); field != (fieldTarget{}) {
		t.Fatalf("field = %+v, want empty", field)
	}
}

func TestSupervisionRestartsOnceThenSucceeds(t *testing.T) {
	companion := &fakeCompanion{
		accessibilityJSON: "[]",
		hidErr:            status.Error(codes.Unavailable, "connection refused"),
	}
	d := newTestDriver(companion)
	restarts := 0
	d.restart = func(context.Context) error {
		restarts++
		return nil
	}
	// First SendHID fails with Unavailable; the recovery restarts once and the
	// retry succeeds (hidErr cleared itself).
	if err := d.Tap(context.Background(), 10, 10); err != nil {
		t.Fatalf("Tap should recover: %v", err)
	}
	if restarts != 1 {
		t.Fatalf("restarts = %d, want exactly 1", restarts)
	}
}

func TestSupervisionDoesNotRestartOnNormalError(t *testing.T) {
	companion := &fakeCompanion{hidErr: errors.New("bad argument")}
	d := newTestDriver(companion)
	restarts := 0
	d.restart = func(context.Context) error { restarts++; return nil }
	if err := d.Tap(context.Background(), 10, 10); err == nil {
		t.Fatal("non-connection error should surface")
	}
	if restarts != 0 {
		t.Fatalf("restarts = %d, want 0 on a normal error", restarts)
	}
}

func TestSupervisionBudgetResetsBetweenIncidents(t *testing.T) {
	companion := &fakeCompanion{accessibilityJSON: "[]"}
	d := newTestDriver(companion)
	restarts := 0
	d.restart = func(context.Context) error { restarts++; return nil }

	companion.hidErr = status.Error(codes.Unavailable, "drop one")
	if err := d.Tap(context.Background(), 1, 1); err != nil {
		t.Fatalf("first incident: %v", err)
	}
	companion.hidErr = status.Error(codes.Unavailable, "drop two")
	if err := d.Tap(context.Background(), 2, 2); err != nil {
		t.Fatalf("second incident: %v", err)
	}
	if restarts != 2 {
		t.Fatalf("restarts = %d, want 2 (one per incident)", restarts)
	}
}

func TestPressKeyEnterSupportedOthersUnsupported(t *testing.T) {
	d := newTestDriver(&fakeCompanion{})
	if err := d.PressKey(context.Background(), "enter"); err != nil {
		t.Fatalf("PressKey enter: %v", err)
	}
	if err := d.PressKey(context.Background(), "return"); err != nil {
		t.Fatalf("PressKey return: %v", err)
	}
	for _, key := range []string{"back", "home", "tab"} {
		if err := d.PressKey(context.Background(), key); err == nil {
			t.Fatalf("PressKey %q should be unsupported", key)
		}
	}
}

func TestWaitForIdleSettlesOnStableTree(t *testing.T) {
	companion := &fakeCompanion{accessibilityJSON: "[]"}
	d := newTestDriver(companion)
	// A fake clock advances time only on Sleep, so the settle poll runs to its
	// cap instantly instead of taking the real two seconds.
	d.idleClock = &fakeClock{}
	if err := d.WaitForIdle(context.Background(), time.Second); err != nil {
		t.Fatalf("WaitForIdle: %v", err)
	}
	if indexOf(companion.calls, "accessibility") < 0 {
		t.Fatalf("WaitForIdle must poll the hierarchy; got %v", companion.calls)
	}
}

func TestForegroundAppPrefersAppUnderTest(t *testing.T) {
	companion := &fakeCompanion{apps: []transport.InstalledApp{
		{BundleID: "com.example.app", ProcessState: transport.ProcessStateRunning, InstallType: "user"},
		{BundleID: "com.other.app", ProcessState: transport.ProcessStateRunning, InstallType: "user"},
	}}
	d := newTestDriver(companion)
	got, err := d.ForegroundApp(context.Background())
	if err != nil {
		t.Fatalf("ForegroundApp: %v", err)
	}
	if got != "com.example.app" {
		t.Fatalf("ForegroundApp = %q, want com.example.app", got)
	}
}

func TestForegroundAppFallsBackToOtherRunningUserApp(t *testing.T) {
	companion := &fakeCompanion{apps: []transport.InstalledApp{
		{BundleID: "com.example.app", ProcessState: transport.ProcessStateNotRunning, InstallType: "user"},
		{BundleID: "com.other.app", ProcessState: transport.ProcessStateRunning, InstallType: "user"},
	}}
	d := newTestDriver(companion)
	got, err := d.ForegroundApp(context.Background())
	if err != nil {
		t.Fatalf("ForegroundApp: %v", err)
	}
	if got != "com.other.app" {
		t.Fatalf("ForegroundApp = %q, want com.other.app", got)
	}
}

func TestHealthReportsIOS(t *testing.T) {
	d := newTestDriver(&fakeCompanion{})
	health, err := d.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !health.Ready || health.Platform != "ios" {
		t.Fatalf("Health = %+v, want ready ios", health)
	}
}

// The driver must satisfy DeviceDriver and ForegroundChecker, but must NOT
// satisfy TextReplacer: its InputText appends, so the runner's pre-erase must
// stay in place.
func TestCapabilityAssertions(t *testing.T) {
	var instance any = newTestDriver(&fakeCompanion{})
	if _, ok := instance.(driver.DeviceDriver); !ok {
		t.Fatal("Driver must implement DeviceDriver")
	}
	if _, ok := instance.(driver.ForegroundChecker); !ok {
		t.Fatal("Driver must implement ForegroundChecker")
	}
	if _, ok := instance.(driver.TextReplacer); ok {
		t.Fatal("Driver must NOT implement TextReplacer")
	}
	if _, ok := instance.(driver.FocusedWindowChecker); ok {
		t.Fatal("Driver must NOT implement FocusedWindowChecker")
	}
}

func indexOf(slice []string, value string) int {
	for i, item := range slice {
		if item == value {
			return i
		}
	}
	return -1
}

// TestNewChildOutlivesStartup proves the companion child is spawned under the
// driver-lifetime context, not the startup-scoped one: a startup context that
// is canceled when New returns would SIGTERM the child mid-run.
func TestNewChildOutlivesStartup(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			_ = connection.Close()
		}
	}()

	var spawnContext context.Context
	options := Options{
		UniqueDeviceIdentifier: "FAKE-UDID",
		pickAddress:            func() (string, error) { return listener.Addr().String(), nil },
		spawnChild: func(ctx context.Context, _ string) (*exec.Cmd, error) {
			spawnContext = ctx
			return &exec.Cmd{}, nil
		},
		dialCompanion: func(string) (transport.Companion, error) {
			return &fakeCompanion{accessibilityJSON: "[]"}, nil
		},
	}

	d, err := New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-spawnContext.Done():
		t.Fatal("spawn context canceled after New returned; child would receive SIGTERM mid-run")
	default:
	}

	d.Close()
	select {
	case <-spawnContext.Done():
	default:
		t.Fatal("spawn context still alive after Close; child lifetime leaks")
	}
}
