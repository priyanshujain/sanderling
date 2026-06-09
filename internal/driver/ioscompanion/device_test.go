package ioscompanion

import (
	"bytes"
	"context"
	"io"
	"net"
	"os/exec"
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

func TestDeviceClearStateWithoutAppPathWarnsOnce(t *testing.T) {
	output := &bytes.Buffer{}
	d := &Driver{output: output, deviceMode: true}
	d.resetContainer = d.deviceResetContainerUnsupported
	for i := 0; i < 2; i++ {
		if err := d.deviceResetContainerUnsupported(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := bytes.Count(output.Bytes(), []byte("requires --ios-app-path")); got != 1 {
		t.Fatalf("warning emitted %d times, want once", got)
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

func equalArgs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
