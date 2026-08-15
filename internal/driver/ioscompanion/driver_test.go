package ioscompanion

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/priyanshujain/sanderling/internal/driver"
	"github.com/priyanshujain/sanderling/internal/driver/ioscompanion/companionpb"
	"github.com/priyanshujain/sanderling/internal/driver/ioscompanion/transport"
)

// fakeCompanion is an in-package stand-in for transport.Companion. It records
// the order of calls and returns scripted results, so the driver's decision
// logic is testable without a live simulator.
type fakeCompanion struct {
	// callsMutex guards calls: Snapshot captures the hierarchy and the
	// screenshot concurrently.
	callsMutex sync.Mutex
	calls      []string

	accessibilityJSON string
	accessibilityErr  error
	screenshotData    []byte
	describe          transport.ScreenDescription
	apps              []transport.InstalledApp

	// hidErr fires on the next SendHID then clears, letting a test inject one
	// transient failure.
	hidErr error
}

func (f *fakeCompanion) record(name string) {
	f.callsMutex.Lock()
	defer f.callsMutex.Unlock()
	f.calls = append(f.calls, name)
}

func (f *fakeCompanion) recorded() []string {
	f.callsMutex.Lock()
	defer f.callsMutex.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

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
	d.reinstallApp = func(context.Context) error { return nil }
	d.grantPaste = func(context.Context) error { return nil }
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

func TestLaunchGrantsPasteboardBeforeLaunch(t *testing.T) {
	companion := &fakeCompanion{accessibilityJSON: "[]"}
	d := newTestDriver(companion)
	granted := false
	d.grantPaste = func(context.Context) error { granted = true; return nil }
	if err := d.Launch(context.Background(), "", false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if !granted {
		t.Fatal("Launch must grant pasteboard access so unicode input skips the permission prompt")
	}
}

func TestLaunchContinuesWhenGrantFails(t *testing.T) {
	companion := &fakeCompanion{accessibilityJSON: "[]"}
	d := newTestDriver(companion)
	d.grantPaste = func(context.Context) error { return errors.New("no privacy db") }
	if err := d.Launch(context.Background(), "", false, nil); err != nil {
		t.Fatalf("Launch must continue when the grant fails: %v", err)
	}
	if indexOf(companion.calls, "launch") < 0 {
		t.Fatalf("app must still launch; got %v", companion.calls)
	}
}

// clearStateProbe records, in order, the calls a run makes to reset the app and
// to bring the runner's automation session up. A reinstall recorded after the
// session is the ordering that races FrontBoard.
type clearStateProbe struct {
	mutex  sync.Mutex
	events []string
}

func (p *clearStateProbe) record(event string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.events = append(p.events, event)
}

func (p *clearStateProbe) recorded() []string {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	out := make([]string, len(p.events))
	copy(out, p.events)
	return out
}

// clearStateOptions wires every seam a hybrid bring-up needs, so New runs its
// real sequence against fakes: no simulator, no simctl, no XCTest session.
func clearStateOptions(t *testing.T, probe *clearStateProbe, udid string, clearState bool) Options {
	t.Helper()
	t.Setenv("SANDERLING_SIMULATOR_COMPANION", "")
	address := startLoopbackListener(t)
	return Options{
		UniqueDeviceIdentifier: udid,
		BundleID:               "com.example.app",
		ClearState:             clearState,
		Output:                 &bytes.Buffer{},
		pickAddress:            func() (string, error) { return address, nil },
		spawnChild:             func(context.Context, string) (*exec.Cmd, error) { return &exec.Cmd{}, nil },
		dialCompanion: func(string) (transport.Companion, error) {
			return &fakeCompanion{accessibilityJSON: "[]"}, nil
		},
		spawnRunner: func(context.Context, string) (*exec.Cmd, error) {
			probe.record("runner session")
			return &exec.Cmd{}, nil
		},
		dialRunner: func(string) (transport.Companion, error) {
			return &fakeCompanion{accessibilityJSON: "[]"}, nil
		},
		reinstallApp:   func(context.Context) error { probe.record("reinstall"); return nil },
		resetContainer: func(context.Context) error { probe.record("reset container"); return nil },
		terminateApp:   func(context.Context) error { probe.record("stop app"); return nil },
	}
}

func TestClearStateReinstallsOnceBeforeTheRunnerSession(t *testing.T) {
	probe := &clearStateProbe{}
	options := clearStateOptions(t, probe, "CLEAR-REINSTALL-UDID", true)
	options.AppPath = "/tmp/Sample.app"

	d, err := New(context.Background(), options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()
	if err := d.Launch(context.Background(), "", true, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	want := []string{"stop app", "reinstall", "runner session"}
	if got := probe.recorded(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v: the reinstall must run once, on a stopped app, before the automation session attaches", got, want)
	}
}

func TestClearStateWithoutAppPathWipesContainerBeforeTheRunnerSession(t *testing.T) {
	probe := &clearStateProbe{}
	output := &bytes.Buffer{}
	options := clearStateOptions(t, probe, "CLEAR-CONTAINER-UDID", true)
	options.Output = output

	d, err := New(context.Background(), options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()
	if err := d.Launch(context.Background(), "", true, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	want := []string{"stop app", "reset container", "runner session"}
	if got := probe.recorded(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v: the fallback must wipe a stopped app's container once, before the session, and never reinstall", got, want)
	}
	if warnings := strings.Count(output.String(), "resetting the data container only"); warnings != 1 {
		t.Fatalf("warning emitted %d times, want once", warnings)
	}
}

func TestWithoutClearStateTheAppIsLeftAlone(t *testing.T) {
	probe := &clearStateProbe{}
	options := clearStateOptions(t, probe, "NO-CLEAR-UDID", false)
	options.AppPath = "/tmp/Sample.app"

	d, err := New(context.Background(), options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()
	if err := d.Launch(context.Background(), "", false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	want := []string{"runner session"}
	if got := probe.recorded(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v: a run that did not ask for clear state must not touch the install", got, want)
	}
}

func TestLaunchRefusesClearStateTheDriverWasNotBuiltFor(t *testing.T) {
	companion := &fakeCompanion{accessibilityJSON: "[]"}
	d := newTestDriver(companion)
	d.appPath = "/tmp/Sample.app"
	reinstalls := 0
	d.reinstallApp = func(context.Context) error { reinstalls++; return nil }

	err := d.Launch(context.Background(), "", true, nil)
	if err == nil || !strings.Contains(err.Error(), "clear-state") {
		t.Fatalf("Launch err = %v, want a refusal naming clear-state", err)
	}
	if reinstalls != 0 {
		t.Fatalf("reinstalls = %d, want 0: a live session must never have the app reinstalled under it", reinstalls)
	}
	if indexOf(companion.recorded(), "launch") >= 0 {
		t.Fatalf("a refused launch must not reach the companion; got %v", companion.recorded())
	}
}

func TestNewRejectsClearStateWithoutBundleID(t *testing.T) {
	probe := &clearStateProbe{}
	options := clearStateOptions(t, probe, "NO-BUNDLE-UDID", true)
	options.BundleID = ""

	if _, err := New(context.Background(), options); err == nil || !strings.Contains(err.Error(), "BundleID") {
		t.Fatalf("New err = %v, want a refusal naming BundleID", err)
	}
	if got := probe.recorded(); len(got) != 0 {
		t.Fatalf("calls = %v, want none: clearing an unnamed bundle would reinstall without resetting anything", got)
	}
}

// scriptedXcrun puts an xcrun on PATH that logs each invocation's arguments and
// answers from replies, a `case "$*" in` body, so a reinstall runs its real
// command sequence and the log holds what reached the tool.
func scriptedXcrun(t *testing.T, replies string) string {
	t.Helper()
	directory := t.TempDir()
	log := filepath.Join(directory, "xcrun.log")
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\ncase \"$*\" in\n" + replies + "\nesac\n"
	if err := os.WriteFile(filepath.Join(directory, "xcrun"), []byte(script), 0o755); err != nil {
		t.Fatalf("write xcrun: %v", err)
	}
	t.Setenv("PATH", directory)
	return log
}

func xcrunCalls(t *testing.T, log string) []string {
	t.Helper()
	contents, err := os.ReadFile(log)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read %s: %v", log, err)
	}
	return strings.Split(strings.TrimSpace(string(contents)), "\n")
}

func TestSimctlReinstallStopsWhenTheUninstallFails(t *testing.T) {
	log := scriptedXcrun(t, `"simctl uninstall "*) echo "Simulator device failed to uninstall app.example."; echo "Uninstall prohibited."; exit 22;;
"simctl install "*) :;;`)
	d := &Driver{udid: "SIM-UDID", bundleID: "app.example", appPath: "/tmp/Sample.app"}

	err := d.simctlReinstall(context.Background())

	if err == nil {
		t.Fatal("simctlReinstall reported success while app.example kept the data clear-state was asked to remove")
	}
	for _, want := range []string{"app.example", "Uninstall prohibited."} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not quote %q", err, want)
		}
	}
	if calls := xcrunCalls(t, log); slices.Contains(calls, "simctl install SIM-UDID /tmp/Sample.app") {
		t.Errorf("xcrun calls = %v: installing over the app carries its data into the run", calls)
	}
}

func TestSimctlReinstallProceedsWhenNothingIsInstalled(t *testing.T) {
	log := scriptedXcrun(t, `"simctl uninstall "*) :;;
"simctl install "*) :;;`)
	d := &Driver{udid: "SIM-UDID", bundleID: "app.example", appPath: "/tmp/Sample.app"}

	if err := d.simctlReinstall(context.Background()); err != nil {
		t.Fatalf("simctlReinstall: %v", err)
	}

	want := []string{"simctl uninstall SIM-UDID app.example", "simctl install SIM-UDID /tmp/Sample.app"}
	if got := xcrunCalls(t, log); !slices.Equal(got, want) {
		t.Fatalf("xcrun calls = %v, want %v", got, want)
	}
}

// TestClearStateStopsTheAppBeforeWipingItsContainer covers the ordering Launch
// used to hold. simctl uninstall copes with a running app; deleting the data
// container out from under one does not, and the CI iOS leg passes no app path
// so it is the wipe that runs. A run whose previous run was interrupted finds
// the app still up.
func TestClearStateStopsTheAppBeforeWipingItsContainer(t *testing.T) {
	container := t.TempDir()
	stale := filepath.Join(container, "Documents")
	if err := os.Mkdir(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	log := scriptedXcrun(t, `"simctl terminate "*) :;;
"simctl get_app_container "*) echo `+container+`;;`)
	d := &Driver{udid: "SIM-UDID", bundleID: "app.example", output: &bytes.Buffer{}}
	d.terminateApp = d.simctlTerminate
	d.resetContainer = d.resetDataContainer

	if err := d.clearAppState(context.Background()); err != nil {
		t.Fatalf("clearAppState: %v", err)
	}

	want := []string{
		"simctl terminate SIM-UDID app.example",
		"simctl get_app_container SIM-UDID app.example data",
	}
	if got := xcrunCalls(t, log); !slices.Equal(got, want) {
		t.Fatalf("xcrun calls = %v, want %v: the app was still writing to the container being deleted", got, want)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stat %s = %v, want the previous run's state gone", stale, err)
	}
}

// TestClearStateSurvivesAnAppThatIsNotRunning holds the terminate to best
// effort. simctl exits non-zero when there is nothing to stop, and a first run
// on a fresh simulator must not fail on it.
func TestClearStateSurvivesAnAppThatIsNotRunning(t *testing.T) {
	container := t.TempDir()
	scriptedXcrun(t, `"simctl terminate "*) echo "No matching processes belonging to bundle identifier app.example"; exit 3;;
"simctl get_app_container "*) echo `+container+`;;`)
	d := &Driver{udid: "SIM-UDID", bundleID: "app.example", output: &bytes.Buffer{}}
	d.terminateApp = d.simctlTerminate
	d.resetContainer = d.resetDataContainer

	if err := d.clearAppState(context.Background()); err != nil {
		t.Fatalf("clearAppState: %v: an app that is not running is not a failure to clear", err)
	}
}

func TestLaunchRejectsEnvironment(t *testing.T) {
	d := newTestDriver(&fakeCompanion{accessibilityJSON: "[]"})
	err := d.Launch(context.Background(), "", false, map[string]string{"K": "V"})
	if err == nil || !strings.Contains(err.Error(), "environment") {
		t.Fatalf("Launch with env: err = %v, want unsupported-environment error", err)
	}
}

func TestSnapshotPairsHierarchyAndScreenshot(t *testing.T) {
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
	// The two captures run concurrently (they ride different transports on
	// the hybrid path), so both must happen but in no particular order.
	calls := companion.recorded()
	if indexOf(calls, "accessibility") < 0 || indexOf(calls, "screenshot") < 0 {
		t.Fatalf("snapshot must capture hierarchy and screenshot; got %v", calls)
	}
}

// blockingScreenshotCompanion holds its Screenshot until proceed closes, so a
// test can keep the screenshot leg in flight while the hierarchy leg recovers.
type blockingScreenshotCompanion struct {
	fakeCompanion
	proceed chan struct{}
}

func (b *blockingScreenshotCompanion) Screenshot(ctx context.Context) ([]byte, string, error) {
	<-b.proceed
	return b.fakeCompanion.Screenshot(ctx)
}

func TestSnapshotRestartDuringScreenshotDoesNotRace(t *testing.T) {
	// The hierarchy leg drops its connection, forcing withRecovery to restart
	// while the screenshot goroutine is still in flight. The restart reassigns
	// d.companion the way respawnAndRedial does; the goroutine must keep
	// working through the transport it captured rather than racing the field.
	first := &blockingScreenshotCompanion{
		fakeCompanion: fakeCompanion{
			accessibilityErr: status.Error(codes.Unavailable, "companion gone"),
			screenshotData:   samplePNG(t, 390, 844),
		},
		proceed: make(chan struct{}),
	}
	replacement := &fakeCompanion{
		accessibilityJSON: "[]",
		screenshotData:    samplePNG(t, 390, 844),
	}
	d := newTestDriver(first)
	d.restart = func(context.Context) error {
		d.companion = replacement
		close(first.proceed)
		return nil
	}
	_, image, err := d.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot should recover: %v", err)
	}
	if image.Width != 390 || image.Height != 844 {
		t.Fatalf("image dims = %dx%d, want 390x844", image.Width, image.Height)
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
	// "abc" is fully mappable and under the paste threshold, so the fast
	// keyboard path runs and never reads the accessibility dump for a field
	// target.
	if err := d.InputText(context.Background(), "abc"); err != nil {
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

// The driver must satisfy DeviceDriver, ForegroundChecker, and TextReplacer
// (its InputText clears the field then types, so the runner skips its
// blackout-fragile pre-erase), but must NOT satisfy FocusedWindowChecker.
func TestCapabilityAssertions(t *testing.T) {
	var instance any = newTestDriver(&fakeCompanion{})
	if _, ok := instance.(driver.DeviceDriver); !ok {
		t.Fatal("Driver must implement DeviceDriver")
	}
	if _, ok := instance.(driver.ForegroundChecker); !ok {
		t.Fatal("Driver must implement ForegroundChecker")
	}
	replacer, ok := instance.(driver.TextReplacer)
	if !ok {
		t.Fatal("Driver must implement TextReplacer")
	}
	if !replacer.ReplacesTextOnInput() {
		t.Fatal("ReplacesTextOnInput must report true")
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
	// The legacy-only path keeps this test focused on the companion child;
	// the hybrid default would also demand runner assets.
	t.Setenv("SANDERLING_SIMULATOR_COMPANION", "legacy")
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

// fakeTextEditingCompanion extends fakeCompanion with the optional TextEditor
// capability so routing through the native text path is testable.
type fakeTextEditingCompanion struct {
	fakeCompanion

	inputTexts  []string
	eraseCounts []int
	pressedKeys []string
}

func (f *fakeTextEditingCompanion) InputText(_ context.Context, text string) error {
	f.record("inputtext")
	f.inputTexts = append(f.inputTexts, text)
	return nil
}

func (f *fakeTextEditingCompanion) EraseText(_ context.Context, characterCount int) error {
	f.record("erasetext")
	f.eraseCounts = append(f.eraseCounts, characterCount)
	return nil
}

func (f *fakeTextEditingCompanion) PressKey(_ context.Context, key string) error {
	f.record("presskey")
	f.pressedKeys = append(f.pressedKeys, key)
	return nil
}

var _ transport.TextEditor = (*fakeTextEditingCompanion)(nil)

func TestInputTextRoutesThroughTextEditor(t *testing.T) {
	companion := &fakeTextEditingCompanion{}
	d := newTestDriver(companion)
	if err := d.InputText(context.Background(), "héllo 🌟"); err != nil {
		t.Fatal(err)
	}
	if len(companion.inputTexts) != 1 || companion.inputTexts[0] != "héllo 🌟" {
		t.Fatalf("inputTexts = %v, want the typed text once", companion.inputTexts)
	}
	for _, call := range companion.calls {
		if call == "hid" {
			t.Fatal("text editor path must not compose HID streams")
		}
	}
}

func TestEraseTextRoutesThroughTextEditor(t *testing.T) {
	companion := &fakeTextEditingCompanion{}
	d := newTestDriver(companion)
	if err := d.EraseText(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if len(companion.eraseCounts) != 1 || companion.eraseCounts[0] != 7 {
		t.Fatalf("eraseCounts = %v, want [7]", companion.eraseCounts)
	}
}

func TestPressKeyRoutesThroughTextEditor(t *testing.T) {
	companion := &fakeTextEditingCompanion{}
	d := newTestDriver(companion)
	if err := d.PressKey(context.Background(), "enter"); err != nil {
		t.Fatal(err)
	}
	if len(companion.pressedKeys) != 1 || companion.pressedKeys[0] != "enter" {
		t.Fatalf("pressedKeys = %v, want [enter]", companion.pressedKeys)
	}
	for _, call := range companion.calls {
		if call == "hid" {
			t.Fatal("text editor path must not compose HID streams")
		}
	}
}

func TestIsConnectionErrorRecognizesUnavailableSentinel(t *testing.T) {
	wrapped := errors.Join(errors.New("dial tcp: connection refused"), transport.ErrCompanionUnavailable)
	if !isConnectionError(wrapped) {
		t.Fatal("wrapped ErrCompanionUnavailable must count as a connection error")
	}
	if isConnectionError(errors.New("ordinary failure")) {
		t.Fatal("ordinary errors must not count as connection errors")
	}
}

// fakeRunnerCompanion stands in for the in-simulator runner half of the hybrid:
// it serves snapshots and native typing.
type fakeRunnerCompanion struct {
	fakeCompanion

	typed []struct {
		text    string
		replace bool
	}
}

func (f *fakeRunnerCompanion) TypeText(_ context.Context, text string, replace bool) error {
	f.record("typetext")
	f.typed = append(f.typed, struct {
		text    string
		replace bool
	}{text, replace})
	return nil
}

var _ transport.TextTyper = (*fakeRunnerCompanion)(nil)

func newHybridTestDriver(legacy *fakeCompanion, runner *fakeRunnerCompanion) *Driver {
	d := newTestDriver(legacy)
	d.hybrid = true
	d.runnerClient = runner
	return d
}

func TestHybridInputTextClearsViaHIDThenTypes(t *testing.T) {
	legacy := &fakeCompanion{accessibilityJSON: "[]"}
	runner := &fakeRunnerCompanion{}
	d := newHybridTestDriver(legacy, runner)

	if err := d.InputText(context.Background(), "héllo 🌟"); err != nil {
		t.Fatal(err)
	}
	if len(legacy.calls) != 1 || legacy.calls[0] != "hid" {
		t.Fatalf("legacy calls = %v, want exactly the clear chord", legacy.calls)
	}
	if len(runner.typed) != 1 || runner.typed[0].text != "héllo 🌟" || runner.typed[0].replace {
		t.Fatalf("typed = %+v, want the text appended without replace", runner.typed)
	}
}

func TestHybridSnapshotsComeFromRunner(t *testing.T) {
	legacy := &fakeCompanion{accessibilityJSON: `[{"type":"Application","frame":{"x":0,"y":0,"width":1,"height":1},"enabled":true}]`}
	runner := &fakeRunnerCompanion{}
	runner.accessibilityJSON = `[{"type":"Button","AXUniqueId":"FromRunner","frame":{"x":0,"y":0,"width":10,"height":10},"enabled":true}]`
	d := newHybridTestDriver(legacy, runner)

	hierarchy, err := d.Hierarchy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hierarchy, "FromRunner") {
		t.Fatalf("hierarchy = %s, want runner content", hierarchy)
	}
	for _, call := range legacy.calls {
		if call == "accessibility" {
			t.Fatal("hybrid must not read snapshots from the legacy companion")
		}
	}
}

func TestHybridLaunchSkipsPasteGrant(t *testing.T) {
	legacy := &fakeCompanion{accessibilityJSON: "[]"}
	runner := &fakeRunnerCompanion{}
	d := newHybridTestDriver(legacy, runner)
	granted := 0
	d.grantPaste = func(context.Context) error { granted++; return nil }

	if err := d.Launch(context.Background(), "", false, nil); err != nil {
		t.Fatal(err)
	}
	if granted != 0 {
		t.Fatalf("grantPaste called %d times, want 0 on the hybrid path", granted)
	}
}

func TestHybridGesturesStayOnLegacyHID(t *testing.T) {
	legacy := &fakeCompanion{accessibilityJSON: "[]"}
	runner := &fakeRunnerCompanion{}
	d := newHybridTestDriver(legacy, runner)

	if err := d.DoubleTap(context.Background(), 10, 20); err != nil {
		t.Fatal(err)
	}
	if len(legacy.calls) != 1 || legacy.calls[0] != "hid" {
		t.Fatalf("legacy calls = %v, want the double-tap HID stream", legacy.calls)
	}
	for _, call := range runner.calls {
		if call == "hid" {
			t.Fatal("runner must not receive gesture streams")
		}
	}
}

func TestHybridRunnerErrorTriggersRestart(t *testing.T) {
	legacy := &fakeCompanion{accessibilityJSON: "[]"}
	runner := &fakeRunnerCompanion{}
	runner.accessibilityErr = fmt.Errorf("read snapshot: %w", transport.ErrCompanionUnavailable)
	d := newHybridTestDriver(legacy, runner)
	restarts := 0
	d.restart = func(context.Context) error {
		restarts++
		runner.accessibilityErr = nil
		return nil
	}

	if _, err := d.Hierarchy(context.Background()); err != nil {
		t.Fatal(err)
	}
	if restarts != 1 {
		t.Fatalf("restarts = %d, want 1", restarts)
	}
}

func TestBindTestRunPortSubstitutesPlaceholder(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "runner.xctestrun")
	if err := os.WriteFile(source, []byte("<string>__COMPANION_PORT__</string>"), 0o644); err != nil {
		t.Fatal(err)
	}
	bound, err := bindTestRunPort(source, "27999")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(bound) != directory {
		t.Fatalf("bound copy %s must sit next to the original", bound)
	}
	content, err := os.ReadFile(bound)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "<string>27999</string>" {
		t.Fatalf("bound content = %s", content)
	}
}

func TestBindTestRunPortRejectsMissingPlaceholder(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "runner.xctestrun")
	if err := os.WriteFile(source, []byte("<string>fixed</string>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := bindTestRunPort(source, "27999"); err == nil {
		t.Fatal("expected an error for a configuration without the placeholder")
	}
}

func TestHybridMappableTextRidesOneHIDStream(t *testing.T) {
	legacy := &fakeCompanion{accessibilityJSON: "[]"}
	runner := &fakeRunnerCompanion{}
	d := newHybridTestDriver(legacy, runner)

	if err := d.InputText(context.Background(), "Travel 42"); err != nil {
		t.Fatal(err)
	}
	if len(legacy.recorded()) != 1 || legacy.recorded()[0] != "hid" {
		t.Fatalf("legacy calls = %v, want one combined chord-and-keystrokes stream", legacy.recorded())
	}
	if len(runner.typed) != 0 {
		t.Fatalf("typed = %+v, want no native typing for mappable text", runner.typed)
	}
}

func TestHybridUnicodeWaitsForClearedFieldBeforeTyping(t *testing.T) {
	legacy := &fakeCompanion{accessibilityJSON: "[]"}
	runner := &fakeRunnerCompanion{}
	// The focused field still shows old content on the first read and is
	// empty on the second; typing must come after the cleared read.
	first := `[{"type":"TextField","AXUniqueId":"F","AXValue":"old","frame":{"x":0,"y":0,"width":100,"height":40},"enabled":true}]`
	second := `[{"type":"TextField","AXUniqueId":"F","AXValue":"","frame":{"x":0,"y":0,"width":100,"height":40},"enabled":true}]`
	reads := 0
	d := newHybridTestDriver(legacy, runner)
	d.mu.Lock()
	d.lastTap.x, d.lastTap.y, d.lastTap.set = 50, 20, true
	d.mu.Unlock()
	// Swap the dump after the first read through a wrapper companion.
	wrapped := &sequencedDumpCompanion{fakeRunnerCompanion: runner, dumps: []string{first, second}, reads: &reads}
	d.runnerClient = wrapped

	if err := d.InputText(context.Background(), "héllo 🌟"); err != nil {
		t.Fatal(err)
	}
	if reads < 2 {
		t.Fatalf("reads = %d, want at least 2 (poll until cleared)", reads)
	}
	if len(wrapped.typed) != 1 || wrapped.typed[0].text != "héllo 🌟" || wrapped.typed[0].replace {
		t.Fatalf("typed = %+v", wrapped.typed)
	}
}

// sequencedDumpCompanion serves scripted dumps in order, repeating the last.
type sequencedDumpCompanion struct {
	*fakeRunnerCompanion
	dumps []string
	reads *int
}

func (s *sequencedDumpCompanion) AccessibilityInfo(context.Context) (string, error) {
	index := *s.reads
	if index >= len(s.dumps) {
		index = len(s.dumps) - 1
	}
	*s.reads++
	return s.dumps[index], nil
}

// wedgedLifecycleCompanion never answers a lifecycle RPC, standing in for a
// runner whose XCTest session is stuck inside a rejected launch.
type wedgedLifecycleCompanion struct {
	fakeCompanion
	release chan struct{}
}

func (w *wedgedLifecycleCompanion) block(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.release:
		return nil
	}
}

func (w *wedgedLifecycleCompanion) Launch(ctx context.Context, _ string, _ bool) error {
	return w.block(ctx)
}

func (w *wedgedLifecycleCompanion) Terminate(ctx context.Context, _ string) error {
	return w.block(ctx)
}

// TestLaunchBoundsWedgedLifecycleRPC proves the launch path carries its own
// deadline. Callers hand Launch an undeadlined context, so without one a runner
// that never answers hangs the run forever with nothing printed.
func TestLaunchBoundsWedgedLifecycleRPC(t *testing.T) {
	previous := launchTimeout
	launchTimeout = 100 * time.Millisecond
	defer func() { launchTimeout = previous }()

	companion := &wedgedLifecycleCompanion{release: make(chan struct{})}
	defer close(companion.release)
	d := newTestDriver(companion)

	done := make(chan error, 1)
	go func() { done <- d.Launch(context.Background(), "com.example.app", false, nil) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("wedged launch returned nil; a stuck runner must surface an error")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want a deadline-exceeded error", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Launch never returned: the lifecycle RPC is unbounded, so a stuck runner hangs the run forever")
	}
}

// TestTerminateBoundsWedgedLifecycleRPC covers the same bound on the standalone
// terminate, which the runner calls mid-run on an equally stuck session.
func TestTerminateBoundsWedgedLifecycleRPC(t *testing.T) {
	previous := launchTimeout
	launchTimeout = 100 * time.Millisecond
	defer func() { launchTimeout = previous }()

	companion := &wedgedLifecycleCompanion{release: make(chan struct{})}
	defer close(companion.release)
	d := newTestDriver(companion)

	done := make(chan error, 1)
	go func() { done <- d.Terminate(context.Background()) }()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want a deadline-exceeded error", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Terminate never returned: the lifecycle RPC is unbounded")
	}
}

// TestLaunchLeavesATighterCallerDeadlineAlone confirms the bound narrows the
// caller's context and never widens it, so a caller that wants to give up
// sooner still does.
func TestLaunchLeavesATighterCallerDeadlineAlone(t *testing.T) {
	previous := launchTimeout
	launchTimeout = 30 * time.Second
	defer func() { launchTimeout = previous }()

	companion := &wedgedLifecycleCompanion{release: make(chan struct{})}
	defer close(companion.release)
	d := newTestDriver(companion)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := d.Launch(ctx, "com.example.app", false, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want a deadline-exceeded error", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Launch took %v; the driver's bound overrode the caller's tighter deadline", elapsed)
	}
}

// blownBudgetShape is one way a transport the driver launches through reports
// a lifecycle call outliving its budget.
type blownBudgetShape struct {
	name string
	err  error
}

// blownBudgetShapes drives every such transport against a server that never
// answers and returns the error each one really produces. The runner transport
// has two: wrapTransport wraps the context's own error once cancellation has
// landed, and the connection's i/o timeout when the deadline it armed from
// that context fires first. The legacy transport reports the same expiry as a
// gRPC status. Only the first satisfies errors.Is(err, context.DeadlineExceeded),
// so a fake that returns ctx.Err() raw shows the driver a recovery that two
// thirds of production can never reach.
func blownBudgetShapes(t *testing.T) []blownBudgetShape {
	t.Helper()
	return []blownBudgetShape{
		{"runner context deadline", runnerContextDeadlineError(t)},
		{"runner connection deadline", silentRunnerLaunchError(t, connectionDeadlineOnly{time.Now().Add(100 * time.Millisecond)})},
		{"legacy grpc deadline", silentGRPCLaunchError(t)},
	}
}

// runnerContextDeadlineError is the shape the runner transport produces once
// the context's own cancellation has landed.
func runnerContextDeadlineError(t *testing.T) error {
	t.Helper()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	return silentRunnerLaunchError(t, ctx)
}

// connectionDeadlineOnly carries a deadline the runner transport arms the
// connection with, while its own cancellation never lands. That is the race
// wrapTransport's second branch exists for: the connection's deadline fires
// while ctx.Err() is still nil.
type connectionDeadlineOnly struct{ deadline time.Time }

func (c connectionDeadlineOnly) Deadline() (time.Time, bool) { return c.deadline, true }
func (c connectionDeadlineOnly) Done() <-chan struct{}       { return nil }
func (c connectionDeadlineOnly) Err() error                  { return nil }
func (c connectionDeadlineOnly) Value(any) any               { return nil }

// silentRunnerLaunchError returns what the real runner transport produces for a
// launch nobody ever answers.
func silentRunnerLaunchError(t *testing.T, ctx context.Context) error {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		<-closed
	}()
	companion, err := transport.DialRunner(listener.Addr().String(), "SIM-UDID", "com.example.app")
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		close(closed)
		_ = companion.Close()
		listener.Close()
	})

	launchErr := companion.Launch(ctx, "com.example.app", true)
	if launchErr == nil {
		t.Fatal("the runner transport reported a launch no server ever answered")
	}
	return launchErr
}

// silentCompanionServer is the legacy companion with a launch that never
// answers, so the caller's own deadline is what ends the call.
type silentCompanionServer struct {
	companionpb.UnimplementedCompanionServiceServer
}

func (silentCompanionServer) Launch(stream grpc.BidiStreamingServer[companionpb.LaunchRequest, companionpb.LaunchResponse]) error {
	<-stream.Context().Done()
	return stream.Context().Err()
}

// silentGRPCLaunchError returns what the legacy transport produces for the same
// launch, which SANDERLING_SIMULATOR_COMPANION=legacy still runs on.
func silentGRPCLaunchError(t *testing.T) error {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	companionpb.RegisterCompanionServiceServer(server, silentCompanionServer{})
	go server.Serve(listener)
	t.Cleanup(server.Stop)

	companion, err := transport.Dial(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { companion.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	launchErr := companion.Launch(ctx, "com.example.app", true)
	if launchErr == nil {
		t.Fatal("the legacy transport reported a launch the companion never answered")
	}
	return launchErr
}

// wedgedUntilRestartCompanion models the session a refused launch leaves
// behind: the refusal is never reported, no later launch is answered until the
// session itself is replaced, and the expired bound reaches the driver in
// whatever shape its transport gives it.
type wedgedUntilRestartCompanion struct {
	fakeCompanion
	blownBudget error
	mutex       sync.Mutex
	replaced    bool
	attempted   int
}

func (w *wedgedUntilRestartCompanion) replaceSession() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.replaced = true
}

func (w *wedgedUntilRestartCompanion) launchAttempts() int {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.attempted
}

func (w *wedgedUntilRestartCompanion) Launch(ctx context.Context, _ string, _ bool) error {
	w.mutex.Lock()
	w.attempted++
	replaced := w.replaced
	w.mutex.Unlock()
	if replaced {
		return nil
	}
	<-ctx.Done()
	return w.blownBudget
}

// TestLaunchReplacesTheSessionAfterALaunchBlowsItsBound covers the FrontBoard
// race: a clear-state reinstall the simulator has not finished registering
// makes the session refuse the launch, and XCTest answers that refusal with
// minutes of diagnostics instead of an error, so the bound expires and every
// later call queues behind the same wedge. Calling launch again on that session
// cannot work; the run only recovers if the session is replaced first. The
// recovery has to fire on every shape the driver's transports report that
// expiry in, because which one arrives is a race the driver does not control.
func TestLaunchReplacesTheSessionAfterALaunchBlowsItsBound(t *testing.T) {
	for _, shape := range blownBudgetShapes(t) {
		t.Run(shape.name, func(t *testing.T) {
			previous := launchTimeout
			launchTimeout = 100 * time.Millisecond
			defer func() { launchTimeout = previous }()

			companion := &wedgedUntilRestartCompanion{blownBudget: shape.err}
			output := &bytes.Buffer{}
			d := newTestDriver(companion)
			d.output = output
			restarts := 0
			d.restart = func(context.Context) error {
				restarts++
				companion.replaceSession()
				return nil
			}

			if err := d.Launch(context.Background(), "", false, nil); err != nil {
				t.Fatalf("Launch: %v", err)
			}
			if restarts != 1 {
				t.Fatalf("session restarts = %d, want exactly 1 (the session reported %v)", restarts, shape.err)
			}
			if attempts := companion.launchAttempts(); attempts != 2 {
				t.Fatalf("launch attempts = %d, want 2: one that wedged and one on the replaced session", attempts)
			}
			if !strings.Contains(output.String(), "restarting the session") {
				t.Fatalf("the recovery was silent, so a run that needed it never says so; output was %q", output.String())
			}
		})
	}
}

// TestLaunchBoundsTheSessionRestartItTriggers keeps the recovery inside a
// budget of its own. The restart deliberately runs on the driver's lifetime
// context rather than the caller's, so without a deadline a session that never
// comes back would hang the launch path exactly the way #73 stopped it hanging.
func TestLaunchBoundsTheSessionRestartItTriggers(t *testing.T) {
	previousLaunch, previousRecovery := launchTimeout, launchRecoveryTimeout
	launchTimeout = 100 * time.Millisecond
	launchRecoveryTimeout = 200 * time.Millisecond
	defer func() { launchTimeout, launchRecoveryTimeout = previousLaunch, previousRecovery }()

	d := newTestDriver(&wedgedUntilRestartCompanion{blownBudget: runnerContextDeadlineError(t)})
	d.restart = func(restartCtx context.Context) error {
		<-restartCtx.Done()
		return restartCtx.Err()
	}

	done := make(chan error, 1)
	go func() { done <- d.Launch(context.Background(), "", false, nil) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "session restart failed") {
			t.Fatalf("err = %v, want the failed restart named", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Launch never returned: a session that never comes back hangs the launch path")
	}
}

// TestLaunchKeepsTheSessionWhenTheCallersOwnDeadlineExpires holds the recovery
// to the driver's own bound. Spending a session restart on a caller that has
// already run out of budget cannot produce a launch, only a later failure.
func TestLaunchKeepsTheSessionWhenTheCallersOwnDeadlineExpires(t *testing.T) {
	previous := launchTimeout
	launchTimeout = 30 * time.Second
	defer func() { launchTimeout = previous }()

	companion := &wedgedUntilRestartCompanion{blownBudget: runnerContextDeadlineError(t)}
	d := newTestDriver(companion)
	restarts := 0
	d.restart = func(context.Context) error {
		restarts++
		companion.replaceSession()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := d.Launch(ctx, "", false, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want a deadline-exceeded error", err)
	}
	if restarts != 0 {
		t.Fatalf("session restarts = %d, want 0", restarts)
	}
}

// refusedLaunchCompanion answers a launch the way the runner does once it
// checks the app's state after activating it: promptly, naming the app and the
// state it reached, over a session that is still serving.
type refusedLaunchCompanion struct {
	fakeCompanion
	attempts int
}

func (r *refusedLaunchCompanion) Launch(context.Context, string, bool) error {
	r.attempts++
	return errors.New(`runner launch: failed("com.example.app is not running after launch")`)
}

// TestLaunchKeepsTheSessionWhenTheRunnerNamesTheRefusal separates a launch that
// answers from a launch that never does. The session restart is the only
// recovery from a wedged session, and it costs a cold start; a runner that
// reports the app's state has already said what a fresh session would say, so
// restarting to hear it again only delays the error and hides the app under it.
func TestLaunchKeepsTheSessionWhenTheRunnerNamesTheRefusal(t *testing.T) {
	companion := &refusedLaunchCompanion{}
	output := &bytes.Buffer{}
	d := newTestDriver(companion)
	d.output = output
	restarts := 0
	d.restart = func(context.Context) error {
		restarts++
		return nil
	}

	err := d.Launch(context.Background(), "", false, nil)
	if err == nil || !strings.Contains(err.Error(), "com.example.app is not running after launch") {
		t.Fatalf("err = %v, want the runner's refusal reaching the caller intact", err)
	}
	if restarts != 0 {
		t.Fatalf("session restarts = %d, want 0: a refusal the runner reported is not a wedged session", restarts)
	}
	if companion.attempts != 1 {
		t.Fatalf("launch attempts = %d, want 1: relaunching an app the runner just refused cannot launch it", companion.attempts)
	}
	if strings.Contains(output.String(), "restarting the session") {
		t.Fatalf("the driver announced a recovery it must not spend here; output was %q", output.String())
	}
}

// newLockTestOptions builds New options that dial a seamed companion, so the
// device-lock tests exercise New without spawning anything.
func newLockTestOptions(t *testing.T, udid string) Options {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			_ = connection.Close()
		}
	}()
	return Options{
		UniqueDeviceIdentifier: udid,
		pickAddress:            func() (string, error) { return listener.Addr().String(), nil },
		spawnChild:             func(context.Context, string) (*exec.Cmd, error) { return &exec.Cmd{}, nil },
		dialCompanion: func(string) (transport.Companion, error) {
			return &fakeCompanion{accessibilityJSON: "[]"}, nil
		},
	}
}

// TestNewRejectsConcurrentRunOnSameDevice proves a second run cannot claim a
// device the first is driving. Two runs interleave app lifecycle on one
// simulator: the second's reinstall lands under the first's automation session
// and wedges it. Failing fast names the contended device; the claim is released
// on Close so the next run is not locked out.
func TestNewRejectsConcurrentRunOnSameDevice(t *testing.T) {
	t.Setenv("SANDERLING_SIMULATOR_COMPANION", "legacy")
	udid := "LOCK-TEST-" + t.Name()

	first, err := New(context.Background(), newLockTestOptions(t, udid))
	if err != nil {
		t.Fatalf("first New: %v", err)
	}

	_, err = New(context.Background(), newLockTestOptions(t, udid))
	if err == nil {
		t.Fatal("second run claimed a device the first still drives; concurrent runs corrupt each other's session")
	}
	if !strings.Contains(err.Error(), udid) {
		t.Fatalf("err = %v, want it to name the contended device %s", err, udid)
	}

	first.Close()
	third, err := New(context.Background(), newLockTestOptions(t, udid))
	if err != nil {
		t.Fatalf("device stayed locked after Close: %v", err)
	}
	third.Close()
}

// TestAcquireDeviceLockKeepsDistinctDevicesIndependent guards against a lock
// path that ignores the udid and serializes unrelated runs.
func TestAcquireDeviceLockKeepsDistinctDevicesIndependent(t *testing.T) {
	first, err := acquireDeviceLock("LOCK-TEST-DEVICE-A")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := acquireDeviceLock("LOCK-TEST-DEVICE-B")
	if err != nil {
		t.Fatalf("a second device was refused while another was locked: %v", err)
	}
	defer second.Close()
}
