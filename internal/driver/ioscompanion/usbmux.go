// This file implements the host-to-device TCP forward the device runner needs,
// natively, against macOS's own usbmuxd. The runner exposes a JSON-RPC server on
// the device loopback; the host dials it through this forward. usbmuxd is the
// macOS daemon at /var/run/usbmuxd that already multiplexes every USB device
// connection; a third-party client (iproxy and the like) is only a thin speaker
// of the same protocol, so talking to the socket directly keeps the device path
// dependent on nothing beyond the OS.
package ioscompanion

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
)

// usbmuxdSocket is the macOS usbmuxd unix socket. It is part of the base OS, not
// an installed dependency.
const usbmuxdSocket = "/var/run/usbmuxd"

// VerifyUsbmuxdSocket reports whether the macOS usbmuxd socket is present. The
// doctor calls it so the device preflight confirms the tunnel's transport
// before a run reaches the build step.
func VerifyUsbmuxdSocket() error {
	if _, err := os.Stat(usbmuxdSocket); err != nil {
		return fmt.Errorf("usbmuxd socket not found at %s: %w", usbmuxdSocket, err)
	}
	return nil
}

// usbmux message framing: a 16-byte little-endian header (length including the
// header, protocol version, payload type, request tag) precedes an XML plist.
const (
	usbmuxHeaderLength = 16
	usbmuxVersion      = 1
	usbmuxPayloadPlist = 8
	usbmuxTag          = 1
)

// usbmuxDial opens a live byte pipe to devicePort on the device identified by
// hardwareUDID: it resolves the device's usbmux id, then issues a Connect whose
// success turns the usbmuxd socket into a raw conduit to that device port. The
// returned net.Conn is the device side of the runner's TCP server.
func usbmuxDial(ctx context.Context, hardwareUDID string, devicePort int) (net.Conn, error) {
	deviceID, err := usbmuxDeviceID(ctx, hardwareUDID)
	if err != nil {
		return nil, err
	}
	conn, err := dialUsbmuxd(ctx)
	if err != nil {
		return nil, err
	}
	// usbmux carries the port in network byte order.
	request, err := encodePlistDict(map[string]any{
		"MessageType": "Connect",
		"DeviceID":    deviceID,
		"PortNumber":  int(htons(uint16(devicePort))),
	})
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := writeUsbmuxMessage(conn, request); err != nil {
		conn.Close()
		return nil, err
	}
	reply, err := readUsbmuxMessage(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	result, err := parsePlistDict(reply)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if number, _ := result["Number"].(int); number != 0 {
		conn.Close()
		return nil, fmt.Errorf("usbmux: connect to device port %d failed (result %d)", devicePort, number)
	}
	return conn, nil
}

// usbmuxDeviceID lists attached devices and returns the usbmux id of the one
// whose serial matches hardwareUDID. usbmux ids are assigned per attachment and
// can change across reconnects, so it is resolved fresh on every dial.
func usbmuxDeviceID(ctx context.Context, hardwareUDID string) (int, error) {
	conn, err := dialUsbmuxd(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	request, err := encodePlistDict(map[string]any{"MessageType": "ListDevices"})
	if err != nil {
		return 0, err
	}
	if err := writeUsbmuxMessage(conn, request); err != nil {
		return 0, err
	}
	reply, err := readUsbmuxMessage(conn)
	if err != nil {
		return 0, err
	}
	list, err := parsePlistDict(reply)
	if err != nil {
		return 0, err
	}
	return selectDeviceID(list, hardwareUDID)
}

// selectDeviceID picks the usbmux DeviceID for hardwareUDID from a parsed
// ListDevices reply, matching the serial with dashes and case ignored so the
// devicectl HardwareUDID (dashed) and the raw USB serial (undashed) both resolve.
func selectDeviceID(list map[string]any, hardwareUDID string) (int, error) {
	devices, _ := list["DeviceList"].([]any)
	target := normalizeSerial(hardwareUDID)
	for _, entry := range devices {
		device, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		properties, _ := device["Properties"].(map[string]any)
		serial, _ := properties["SerialNumber"].(string)
		if normalizeSerial(serial) != target {
			continue
		}
		if id, ok := device["DeviceID"].(int); ok {
			return id, nil
		}
		if id, ok := properties["DeviceID"].(int); ok {
			return id, nil
		}
	}
	return 0, fmt.Errorf("usbmux: device %s not attached", hardwareUDID)
}

func normalizeSerial(serial string) string {
	return strings.ToLower(strings.ReplaceAll(serial, "-", ""))
}

// htons swaps a port to network byte order, as the usbmux Connect PortNumber
// requires.
func htons(port uint16) uint16 {
	return port<<8 | port>>8
}

func dialUsbmuxd(ctx context.Context) (net.Conn, error) {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", usbmuxdSocket)
	if err != nil {
		return nil, fmt.Errorf("usbmux: dial %s: %w", usbmuxdSocket, err)
	}
	return conn, nil
}

func writeUsbmuxMessage(conn net.Conn, payload []byte) error {
	header := make([]byte, usbmuxHeaderLength)
	binary.LittleEndian.PutUint32(header[0:4], uint32(usbmuxHeaderLength+len(payload)))
	binary.LittleEndian.PutUint32(header[4:8], usbmuxVersion)
	binary.LittleEndian.PutUint32(header[8:12], usbmuxPayloadPlist)
	binary.LittleEndian.PutUint32(header[12:16], usbmuxTag)
	if _, err := conn.Write(header); err != nil {
		return fmt.Errorf("usbmux: write header: %w", err)
	}
	if _, err := conn.Write(payload); err != nil {
		return fmt.Errorf("usbmux: write payload: %w", err)
	}
	return nil
}

func readUsbmuxMessage(conn net.Conn) ([]byte, error) {
	header := make([]byte, usbmuxHeaderLength)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, fmt.Errorf("usbmux: read header: %w", err)
	}
	length := binary.LittleEndian.Uint32(header[0:4])
	if length < usbmuxHeaderLength {
		return nil, fmt.Errorf("usbmux: reply length %d shorter than header", length)
	}
	payload := make([]byte, length-usbmuxHeaderLength)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, fmt.Errorf("usbmux: read payload: %w", err)
	}
	return payload, nil
}

// encodePlistDict renders a flat dict (string or int values) as the XML plist
// usbmux requests use. Keys are sorted so the output is deterministic.
func encodePlistDict(fields map[string]any) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	buffer.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	buffer.WriteString(`<plist version="1.0">` + "\n<dict>\n")
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&buffer, "<key>%s</key>", key)
		switch value := fields[key].(type) {
		case string:
			buffer.WriteString("<string>")
			xml.EscapeText(&buffer, []byte(value))
			buffer.WriteString("</string>\n")
		case int:
			fmt.Fprintf(&buffer, "<integer>%d</integer>\n", value)
		default:
			return nil, fmt.Errorf("usbmux: unsupported plist value type %T for key %s", value, key)
		}
	}
	buffer.WriteString("</dict>\n</plist>\n")
	return buffer.Bytes(), nil
}

// parsePlistDict parses an XML plist whose root is a dict into a generic map.
func parsePlistDict(data []byte) (map[string]any, error) {
	value, err := parsePlist(data)
	if err != nil {
		return nil, err
	}
	dict, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("usbmux: plist root is %T, want dict", value)
	}
	return dict, nil
}

// parsePlist decodes an XML plist into nested map[string]any / []any / string /
// int / bool values. It covers the element set usbmux replies use.
func parsePlist(data []byte) (any, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("usbmux: parse plist: %w", err)
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "plist" {
			return parsePlistChild(decoder)
		}
	}
}

// parsePlistChild reads forward to the next start element and parses it as a
// value. It is used for the lone child of <plist> and of each <key>.
func parsePlistChild(decoder *xml.Decoder) (any, error) {
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch element := token.(type) {
		case xml.StartElement:
			return parsePlistElement(decoder, element)
		case xml.EndElement:
			return nil, nil
		}
	}
}

func parsePlistElement(decoder *xml.Decoder, start xml.StartElement) (any, error) {
	switch start.Name.Local {
	case "dict":
		return parsePlistDictBody(decoder)
	case "array":
		return parsePlistArray(decoder)
	case "string":
		return parsePlistText(decoder)
	case "integer":
		text, err := parsePlistText(decoder)
		if err != nil {
			return nil, err
		}
		number, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil {
			return nil, fmt.Errorf("usbmux: parse integer %q: %w", text, err)
		}
		return number, nil
	case "true":
		return true, decoder.Skip()
	case "false":
		return false, decoder.Skip()
	default:
		// Unhandled scalar (real, data, date): consume it and report nil so an
		// unexpected field never aborts parsing the fields that matter.
		return nil, decoder.Skip()
	}
}

func parsePlistDictBody(decoder *xml.Decoder) (map[string]any, error) {
	result := map[string]any{}
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch element := token.(type) {
		case xml.StartElement:
			if element.Name.Local != "key" {
				return nil, fmt.Errorf("usbmux: expected <key>, got <%s>", element.Name.Local)
			}
			key, err := parsePlistText(decoder)
			if err != nil {
				return nil, err
			}
			value, err := parsePlistChild(decoder)
			if err != nil {
				return nil, err
			}
			result[key] = value
		case xml.EndElement:
			return result, nil
		}
	}
}

func parsePlistArray(decoder *xml.Decoder) ([]any, error) {
	var result []any
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch element := token.(type) {
		case xml.StartElement:
			value, err := parsePlistElement(decoder, element)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		case xml.EndElement:
			return result, nil
		}
	}
}

func parsePlistText(decoder *xml.Decoder) (string, error) {
	var text strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			return "", err
		}
		switch element := token.(type) {
		case xml.CharData:
			text.Write(element)
		case xml.EndElement:
			return text.String(), nil
		}
	}
}

// usbmuxForwarder is the in-process replacement for an iproxy child: it accepts
// host loopback connections and bridges each to a fresh device-port conduit over
// usbmux. It satisfies io.Closer so the driver tears it down like any other
// tunnel handle; closing the listener ends the accept loop and the bridges drain
// as their copies finish.
type usbmuxForwarder struct {
	listener   net.Listener
	dialDevice func(ctx context.Context) (net.Conn, error)
	ctx        context.Context
}

// startUsbmuxForwarder listens on localAddress and forwards every accepted
// connection to devicePort on the device, over usbmux.
func startUsbmuxForwarder(ctx context.Context, hardwareUDID, localAddress string, devicePort int) (*usbmuxForwarder, error) {
	return startForwarder(ctx, localAddress, func(dialCtx context.Context) (net.Conn, error) {
		return usbmuxDial(dialCtx, hardwareUDID, devicePort)
	})
}

// startForwarder is the seam-friendly core: the device dialer is injected so a
// test can bridge to an in-process echo server without a real device.
func startForwarder(ctx context.Context, localAddress string, dialDevice func(context.Context) (net.Conn, error)) (*usbmuxForwarder, error) {
	listener, err := net.Listen("tcp", localAddress)
	if err != nil {
		return nil, fmt.Errorf("usbmux forwarder: listen %s: %w", localAddress, err)
	}
	forwarder := &usbmuxForwarder{listener: listener, dialDevice: dialDevice, ctx: ctx}
	go forwarder.serve()
	return forwarder, nil
}

func (f *usbmuxForwarder) serve() {
	for {
		hostConn, err := f.listener.Accept()
		if err != nil {
			return
		}
		go f.bridge(hostConn)
	}
}

// bridge connects to the device side, then copies bytes in both directions
// until either end closes. A failed device dial (the runner not yet listening)
// closes the host side, which the runner transport reads as a dropped
// connection and recovers from on its next call.
func (f *usbmuxForwarder) bridge(hostConn net.Conn) {
	defer hostConn.Close()
	deviceConn, err := f.dialDevice(f.ctx)
	if err != nil {
		return
	}
	defer deviceConn.Close()
	done := make(chan struct{}, 2)
	go func() { io.Copy(deviceConn, hostConn); done <- struct{}{} }()
	go func() { io.Copy(hostConn, deviceConn); done <- struct{}{} }()
	// One direction ending closes both conns via the defers, which unblocks the
	// other copy; waiting for one is enough to know the bridge is finished.
	<-done
}

func (f *usbmuxForwarder) Close() error {
	return f.listener.Close()
}

// startUsbmuxTunnel adapts the forwarder to the driver's startTunnel seam,
// returning it as an io.Closer.
func startUsbmuxTunnel(ctx context.Context, hardwareUDID, localAddress, devicePort string) (io.Closer, error) {
	port, err := strconv.Atoi(devicePort)
	if err != nil {
		return nil, fmt.Errorf("usbmux tunnel: device port %q: %w", devicePort, err)
	}
	return startUsbmuxForwarder(ctx, hardwareUDID, localAddress, port)
}
