package ioscompanion

import (
	"bytes"
	"context"
	"io"
	"net"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/priyanshujain/sanderling/internal/driver/ioscompanion/transport"
)

// startLoopbackListener accepts and immediately closes connections so
// waitForListener succeeds against a real address without a real runner.
func startLoopbackListener(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	return listener.Addr().String()
}

func testDeviceOptions(address string, companion transport.Companion) DeviceOptions {
	return DeviceOptions{
		HardwareUDID: "00008140-HW",
		CoreDeviceID: "CORE-1",
		BundleID:     "com.example.app",
		Output:       &bytes.Buffer{},
		spawnRunner:  func(context.Context, string) (*exec.Cmd, error) { return &exec.Cmd{}, nil },
		startTunnel:  func(context.Context, string, string, string) (io.Closer, error) { return io.NopCloser(nil), nil },
		dialRunner:   func(string) (transport.Companion, error) { return companion, nil },
		pickAddress:  func() (string, error) { return address, nil },
	}
}

func newDeviceCompanion() *fakeTextEditingCompanion {
	companion := &fakeTextEditingCompanion{}
	companion.accessibilityJSON = "[]"
	companion.describe = transport.ScreenDescription{WidthPoints: 393, HeightPoints: 852}
	return companion
}

func TestNewDeviceWiresRunnerOnlyMode(t *testing.T) {
	address := startLoopbackListener(t)
	companion := newDeviceCompanion()
	d, err := NewDevice(context.Background(), testDeviceOptions(address, companion))
	if err != nil {
		t.Fatalf("NewDevice: %v", err)
	}
	defer d.Close()

	if !d.deviceMode {
		t.Fatal("deviceMode must be true")
	}
	if d.hybrid {
		t.Fatal("device mode must not be hybrid")
	}
	if d.runnerClient != nil {
		t.Fatal("device mode must leave runnerClient nil so InputText avoids the hybrid HID chord")
	}
	if d.companion != companion {
		t.Fatal("d.companion must be the runner dialed over the tunnel")
	}
	if d.coreDeviceID != "CORE-1" {
		t.Fatalf("coreDeviceID = %q, want CORE-1", d.coreDeviceID)
	}
	if d.screenWidth != 393 || d.screenHeight != 852 {
		t.Fatalf("screen = %dx%d, want 393x852", d.screenWidth, d.screenHeight)
	}
}

// TestNewDeviceWiresEveryBringUpsAddressPicker keeps the device driver whole.
// bringUpRunner reads its picker from a field rather than calling the package
// function, and NewDevice left that field nil, so the only thing standing
// between a device run and a nil call was which restart path happened to run.
func TestNewDeviceWiresEveryBringUpsAddressPicker(t *testing.T) {
	address := startLoopbackListener(t)
	options := testDeviceOptions(address, newDeviceCompanion())
	options.HardwareUDID = "00008140-PICKER"
	d, err := NewDevice(context.Background(), options)
	if err != nil {
		t.Fatalf("NewDevice: %v", err)
	}
	defer d.Close()

	if err := d.bringUpRunner(context.Background()); err != nil {
		t.Fatalf("bringUpRunner: %v", err)
	}
}

func TestNewDeviceRequiresIdentifiers(t *testing.T) {
	if _, err := NewDevice(context.Background(), DeviceOptions{CoreDeviceID: "x"}); err == nil {
		t.Fatal("missing HardwareUDID must error")
	}
	if _, err := NewDevice(context.Background(), DeviceOptions{HardwareUDID: "x"}); err == nil {
		t.Fatal("missing CoreDeviceID must error")
	}
}

func TestDeviceInputTextUsesNativeEditorNotKeyboardHID(t *testing.T) {
	address := startLoopbackListener(t)
	companion := newDeviceCompanion()
	d, err := NewDevice(context.Background(), testDeviceOptions(address, companion))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := d.InputText(context.Background(), "héllo 🌟"); err != nil {
		t.Fatal(err)
	}
	if len(companion.inputTexts) != 1 || companion.inputTexts[0] != "héllo 🌟" {
		t.Fatalf("inputTexts = %v, want native replace of the text", companion.inputTexts)
	}
	for _, call := range companion.recorded() {
		if call == "hid" {
			t.Fatal("device text must go through the native editor, never a keyboard HID chord")
		}
	}
}

func TestDeviceGesturesRouteTouchHIDToRunner(t *testing.T) {
	address := startLoopbackListener(t)
	companion := newDeviceCompanion()
	d, err := NewDevice(context.Background(), testDeviceOptions(address, companion))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := d.DoubleTap(context.Background(), 10, 20); err != nil {
		t.Fatal(err)
	}
	if indexOf(companion.recorded(), "hid") < 0 {
		t.Fatalf("gesture must send touch HID to the runner; got %v", companion.recorded())
	}
}

func TestDeviceEraseAndPressKeyRouteThroughEditor(t *testing.T) {
	address := startLoopbackListener(t)
	companion := newDeviceCompanion()
	d, err := NewDevice(context.Background(), testDeviceOptions(address, companion))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := d.EraseText(context.Background(), 4); err != nil {
		t.Fatal(err)
	}
	if err := d.PressKey(context.Background(), "enter"); err != nil {
		t.Fatal(err)
	}
	if len(companion.eraseCounts) != 1 || companion.eraseCounts[0] != 4 {
		t.Fatalf("eraseCounts = %v, want [4]", companion.eraseCounts)
	}
	if len(companion.pressedKeys) != 1 || companion.pressedKeys[0] != "enter" {
		t.Fatalf("pressedKeys = %v, want [enter]", companion.pressedKeys)
	}
}

func TestNewDeviceReinstallsOnceBeforeTheRunnerSession(t *testing.T) {
	address := startLoopbackListener(t)
	probe := &clearStateProbe{}
	options := testDeviceOptions(address, newDeviceCompanion())
	options.HardwareUDID = "00008140-CLEAR"
	options.AppPath = "/tmp/Sample.app"
	options.ClearState = true
	options.reinstallApp = func(context.Context) error { probe.record("reinstall"); return nil }
	spawn := options.spawnRunner
	options.spawnRunner = func(ctx context.Context, runnerAddress string) (*exec.Cmd, error) {
		probe.record("runner session")
		return spawn(ctx, runnerAddress)
	}

	d, err := NewDevice(context.Background(), options)
	if err != nil {
		t.Fatalf("NewDevice: %v", err)
	}
	defer d.Close()
	if err := d.Launch(context.Background(), "", true, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	want := []string{"reinstall", "runner session"}
	if got := probe.recorded(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v: devicectl must reinstall once, before the runner's test session attaches", got, want)
	}
}

func TestDevicectlReinstallStopsWhenTheUninstallFails(t *testing.T) {
	log := scriptedXcrun(t, `"devicectl device uninstall "*) echo "ERROR: Internal logic error: Connection was invalidated"; exit 1;;
"devicectl device install "*) :;;`)
	d := &Driver{coreDeviceID: "CORE-DEVICE", bundleID: "app.example", appPath: "/tmp/Sample.app"}

	err := d.devicectlReinstall(context.Background())

	if err == nil {
		t.Fatal("devicectlReinstall reported success while app.example kept the data clear-state was asked to remove")
	}
	for _, want := range []string{"app.example", "Connection was invalidated"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not quote %q", err, want)
		}
	}
	calls := xcrunCalls(t, log)
	if slices.ContainsFunc(calls, func(call string) bool { return strings.HasPrefix(call, "devicectl device install") }) {
		t.Errorf("xcrun calls = %v: installing over the app carries its data into the run", calls)
	}
}

func TestDevicectlReinstallProceedsWhenNothingIsInstalled(t *testing.T) {
	log := scriptedXcrun(t, `"devicectl device uninstall "*) echo "App uninstalled.";;
"devicectl device install "*) :;;`)
	d := &Driver{coreDeviceID: "CORE-DEVICE", bundleID: "app.example", appPath: "/tmp/Sample.app"}

	if err := d.devicectlReinstall(context.Background()); err != nil {
		t.Fatalf("devicectlReinstall: %v", err)
	}

	want := []string{
		"devicectl device uninstall app --device CORE-DEVICE app.example",
		"devicectl device install app --device CORE-DEVICE /tmp/Sample.app",
	}
	if got := xcrunCalls(t, log); !slices.Equal(got, want) {
		t.Fatalf("xcrun calls = %v, want %v", got, want)
	}
}

// TestNewDeviceRefusesClearStateWithoutAnAppPath keeps the device from starting
// a run whose clear-state cannot happen. There is no data-container wipe on a
// physical device, so without an app path to reinstall from, carrying on hands
// the run every previous run's data under a flag that says otherwise.
func TestNewDeviceRefusesClearStateWithoutAnAppPath(t *testing.T) {
	address := startLoopbackListener(t)
	options := testDeviceOptions(address, newDeviceCompanion())
	options.HardwareUDID = "00008140-NO-APP-PATH"
	options.ClearState = true
	spawned := false
	spawn := options.spawnRunner
	options.spawnRunner = func(ctx context.Context, runnerAddress string) (*exec.Cmd, error) {
		spawned = true
		return spawn(ctx, runnerAddress)
	}

	d, err := NewDevice(context.Background(), options)
	if err == nil {
		d.Close()
		t.Fatal("NewDevice returned a driver whose clear-state never happened")
	}
	if !strings.Contains(err.Error(), "--ios-app-path") {
		t.Fatalf("err = %v, want it to name the flag that makes the clear possible", err)
	}
	if spawned {
		t.Fatal("the run started anyway; a clear-state that cannot happen must end the run, not open it")
	}
}

type recordingCloser struct{ closed bool }

func (c *recordingCloser) Close() error {
	c.closed = true
	return nil
}

func TestDeviceCloseStopsRunnerAndTunnel(t *testing.T) {
	d := &Driver{output: &bytes.Buffer{}}
	runner := exec.Command("sleep", "30")
	if err := runner.Start(); err != nil {
		t.Fatal(err)
	}
	tunnel := &recordingCloser{}
	d.runnerChild = runner
	d.tunnel = tunnel

	d.Close()

	if d.runnerChild != nil || d.tunnel != nil {
		t.Fatal("Close must clear the runner child and the tunnel")
	}
	if runner.ProcessState == nil {
		t.Fatal("Close must reap the runner session child")
	}
	if !tunnel.closed {
		t.Fatal("Close must close the usbmux tunnel")
	}
}
