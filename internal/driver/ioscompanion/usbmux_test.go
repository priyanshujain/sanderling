package ioscompanion

import (
	"context"
	"io"
	"net"
	"testing"
)

func TestHtonsSwapsBytes(t *testing.T) {
	// 49200 = 0xC030; network order swaps to 0x30C0 = 12480.
	if got := htons(49200); got != 12480 {
		t.Fatalf("htons(49200) = %d, want 12480", got)
	}
	if got := htons(0x1234); got != 0x3412 {
		t.Fatalf("htons(0x1234) = %#x, want 0x3412", got)
	}
}

func TestEncodePlistDictRoundTrips(t *testing.T) {
	encoded, err := encodePlistDict(map[string]any{
		"MessageType": "Connect",
		"DeviceID":    7,
		"PortNumber":  12480,
	})
	if err != nil {
		t.Fatal(err)
	}
	dict, err := parsePlistDict(encoded)
	if err != nil {
		t.Fatalf("parse round-trip: %v", err)
	}
	if dict["MessageType"] != "Connect" {
		t.Fatalf("MessageType = %v, want Connect", dict["MessageType"])
	}
	if dict["DeviceID"] != 7 {
		t.Fatalf("DeviceID = %v, want 7", dict["DeviceID"])
	}
	if dict["PortNumber"] != 12480 {
		t.Fatalf("PortNumber = %v, want 12480", dict["PortNumber"])
	}
}

func TestEncodePlistDictEscapesStrings(t *testing.T) {
	encoded, err := encodePlistDict(map[string]any{"Name": "a & b <c>"})
	if err != nil {
		t.Fatal(err)
	}
	dict, err := parsePlistDict(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if dict["Name"] != "a & b <c>" {
		t.Fatalf("Name = %q, want the unescaped original", dict["Name"])
	}
}

// listDevicesReply is a representative usbmux ListDevices response with one USB
// device, matching the shape macOS usbmuxd returns.
const listDevicesReply = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>DeviceList</key>
  <array>
    <dict>
      <key>DeviceID</key>
      <integer>7</integer>
      <key>MessageType</key>
      <string>Attached</string>
      <key>Properties</key>
      <dict>
        <key>ConnectionType</key>
        <string>USB</string>
        <key>SerialNumber</key>
        <string>00008140-00022C4A3E13001C</string>
      </dict>
    </dict>
  </array>
</dict>
</plist>`

func TestSelectDeviceIDMatchesDashedSerial(t *testing.T) {
	list, err := parsePlistDict([]byte(listDevicesReply))
	if err != nil {
		t.Fatal(err)
	}
	id, err := selectDeviceID(list, "00008140-00022C4A3E13001C")
	if err != nil {
		t.Fatalf("select by dashed serial: %v", err)
	}
	if id != 7 {
		t.Fatalf("DeviceID = %d, want 7", id)
	}
}

func TestSelectDeviceIDMatchesUndashedSerial(t *testing.T) {
	list, err := parsePlistDict([]byte(listDevicesReply))
	if err != nil {
		t.Fatal(err)
	}
	// The raw USB serial carries no dash; it must still resolve.
	id, err := selectDeviceID(list, "0000814000022C4A3E13001C")
	if err != nil {
		t.Fatalf("select by undashed serial: %v", err)
	}
	if id != 7 {
		t.Fatalf("DeviceID = %d, want 7", id)
	}
}

func TestSelectDeviceIDNotAttached(t *testing.T) {
	list, err := parsePlistDict([]byte(listDevicesReply))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := selectDeviceID(list, "DEADBEEF-NOTHERE"); err == nil {
		t.Fatal("a serial not in the list must error")
	}
}

func TestParsePlistDictRejectsNonDictRoot(t *testing.T) {
	arrayRoot := `<?xml version="1.0"?><plist version="1.0"><array></array></plist>`
	if _, err := parsePlistDict([]byte(arrayRoot)); err == nil {
		t.Fatal("a non-dict plist root must error")
	}
}

// TestForwarderBridgesToDevice drives the forwarder end to end against an
// in-process echo "device": a host connection through the loopback listener must
// reach the injected device dialer and round-trip bytes.
func TestForwarderBridgesToDevice(t *testing.T) {
	deviceListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer deviceListener.Close()
	go func() {
		for {
			conn, acceptErr := deviceListener.Accept()
			if acceptErr != nil {
				return
			}
			go io.Copy(conn, conn)
		}
	}()

	localAddress, err := pickLoopbackAddress()
	if err != nil {
		t.Fatal(err)
	}
	forwarder, err := startForwarder(context.Background(), localAddress, func(ctx context.Context) (net.Conn, error) {
		return net.Dial("tcp", deviceListener.Addr().String())
	})
	if err != nil {
		t.Fatal(err)
	}
	defer forwarder.Close()

	hostConn, err := net.Dial("tcp", localAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer hostConn.Close()

	message := []byte("ping\n")
	if _, err := hostConn.Write(message); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, len(message))
	if _, err := io.ReadFull(hostConn, buffer); err != nil {
		t.Fatal(err)
	}
	if string(buffer) != string(message) {
		t.Fatalf("round-trip = %q, want %q", buffer, message)
	}
}

// TestForwarderClosesHostOnDeviceDialFailure asserts the failure path the runner
// transport relies on: when the device dial fails (the runner not yet
// listening), the forwarder closes the host side so the caller sees a dropped
// connection and retries.
func TestForwarderClosesHostOnDeviceDialFailure(t *testing.T) {
	localAddress, err := pickLoopbackAddress()
	if err != nil {
		t.Fatal(err)
	}
	forwarder, err := startForwarder(context.Background(), localAddress, func(ctx context.Context) (net.Conn, error) {
		return nil, io.ErrUnexpectedEOF
	})
	if err != nil {
		t.Fatal(err)
	}
	defer forwarder.Close()

	hostConn, err := net.Dial("tcp", localAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer hostConn.Close()

	// The forwarder closes its side after the failed device dial; the read
	// returns EOF rather than blocking forever.
	if _, err := io.ReadFull(hostConn, make([]byte, 1)); err == nil {
		t.Fatal("read must fail once the forwarder closes the host side")
	}
}

func TestForwarderCloseStopsAcceptLoop(t *testing.T) {
	localAddress, err := pickLoopbackAddress()
	if err != nil {
		t.Fatal(err)
	}
	forwarder, err := startForwarder(context.Background(), localAddress, func(ctx context.Context) (net.Conn, error) {
		return nil, io.EOF
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := forwarder.Close(); err != nil {
		t.Fatal(err)
	}
	// After Close the listener is gone, so a dial must fail.
	if conn, dialErr := net.Dial("tcp", localAddress); dialErr == nil {
		conn.Close()
		t.Fatal("dial must fail after the forwarder is closed")
	}
}
