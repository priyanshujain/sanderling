package ios

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// devicectlJSON mirrors the shape of `xcrun devicectl list devices
// --json-output` for two connected devices, trimmed to the fields the parser
// reads.
const devicectlJSON = `{
  "info": {"outcome": "success"},
  "result": {
    "devices": [
      {
        "identifier": "1FB35C36-A358-56F9-AD9F-931DC1C867FF",
        "hardwareProperties": {"udid": "00008140-00022C4A3E13001C"},
        "deviceProperties": {"name": "iPhone"}
      },
      {
        "identifier": "2AC46D47-B469-67A0-BE0A-042ED2D978AA",
        "hardwareProperties": {"udid": "00008110-000A1B2C3D4E5F60"},
        "deviceProperties": {"name": "Test iPad"}
      }
    ]
  }
}`

func swapListDevices(t *testing.T, devices []Device, err error) {
	t.Helper()
	original := listDevices
	t.Cleanup(func() { listDevices = original })
	listDevices = func(context.Context) ([]Device, error) { return devices, err }
}

func TestParseDevices(t *testing.T) {
	got, err := parseDevices([]byte(devicectlJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("parsed %d devices, want 2", len(got))
	}
	want := Device{Name: "iPhone", HardwareUDID: "00008140-00022C4A3E13001C", CoreDeviceID: "1FB35C36-A358-56F9-AD9F-931DC1C867FF"}
	if got[0] != want {
		t.Fatalf("device[0] = %+v, want %+v", got[0], want)
	}
}

func TestParseDevices_InvalidJSON(t *testing.T) {
	if _, err := parseDevices([]byte("not json")); err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}

func TestResolveDevice_MatchByName(t *testing.T) {
	devices, _ := parseDevices([]byte(devicectlJSON))
	swapListDevices(t, devices, nil)
	got, err := ResolveDevice(context.Background(), "iPhone")
	if err != nil {
		t.Fatal(err)
	}
	if got.HardwareUDID != "00008140-00022C4A3E13001C" {
		t.Fatalf("resolved %+v, want the iPhone", got)
	}
}

func TestResolveDevice_MatchByHardwareUDID(t *testing.T) {
	devices, _ := parseDevices([]byte(devicectlJSON))
	swapListDevices(t, devices, nil)
	got, err := ResolveDevice(context.Background(), "00008110-000A1B2C3D4E5F60")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Test iPad" {
		t.Fatalf("resolved %+v, want Test iPad", got)
	}
}

func TestResolveDevice_MatchByCoreDeviceID(t *testing.T) {
	devices, _ := parseDevices([]byte(devicectlJSON))
	swapListDevices(t, devices, nil)
	got, err := ResolveDevice(context.Background(), "1FB35C36-A358-56F9-AD9F-931DC1C867FF")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "iPhone" {
		t.Fatalf("resolved %+v, want iPhone", got)
	}
}

func TestResolveDevice_EmptyQuerySingleDevice(t *testing.T) {
	swapListDevices(t, []Device{{Name: "iPhone", HardwareUDID: "udid-1", CoreDeviceID: "id-1"}}, nil)
	got, err := ResolveDevice(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got.HardwareUDID != "udid-1" {
		t.Fatalf("resolved %+v, want the only device", got)
	}
}

func TestResolveDevice_EmptyQueryMultipleErrors(t *testing.T) {
	devices, _ := parseDevices([]byte(devicectlJSON))
	swapListDevices(t, devices, nil)
	_, err := ResolveDevice(context.Background(), "")
	if err == nil {
		t.Fatal("expected ambiguity error for multiple devices with no query")
	}
	for _, want := range []string{"--ios-device", "iPhone", "Test iPad"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ambiguity error missing %q: %v", want, err)
		}
	}
}

func TestResolveDevice_NotFound(t *testing.T) {
	devices, _ := parseDevices([]byte(devicectlJSON))
	swapListDevices(t, devices, nil)
	_, err := ResolveDevice(context.Background(), "Pixel")
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if !strings.Contains(err.Error(), "iPhone") {
		t.Errorf("not-found error should list candidates: %v", err)
	}
}

func TestResolveDevice_NoneConnected(t *testing.T) {
	swapListDevices(t, nil, nil)
	_, err := ResolveDevice(context.Background(), "iPhone")
	if err == nil {
		t.Fatal("expected error when no device is connected")
	}
}

func TestResolveDevice_ListError(t *testing.T) {
	swapListDevices(t, nil, errors.New("devicectl blew up"))
	if _, err := ResolveDevice(context.Background(), ""); err == nil {
		t.Fatal("expected list error to propagate")
	}
}

func TestResolveDevice_AmbiguousMatch(t *testing.T) {
	swapListDevices(t, []Device{
		{Name: "iPhone", HardwareUDID: "udid-a", CoreDeviceID: "id-a"},
		{Name: "iPhone", HardwareUDID: "udid-b", CoreDeviceID: "id-b"},
	}, nil)
	_, err := ResolveDevice(context.Background(), "iPhone")
	if err == nil {
		t.Fatal("expected ambiguous-match error for duplicate names")
	}
	if !strings.Contains(err.Error(), "udid-a") || !strings.Contains(err.Error(), "udid-b") {
		t.Errorf("ambiguous error should list both candidates: %v", err)
	}
}
