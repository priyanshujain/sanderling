package ios

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Device is a physical iOS device resolved from devicectl. A device carries two
// identifiers: HardwareUDID feeds xcodebuild -destination and iproxy -u, while
// CoreDeviceID feeds devicectl install/uninstall.
type Device struct {
	Name         string
	HardwareUDID string
	CoreDeviceID string
}

// coreDevice mirrors the fields of one entry in devicectl's JSON device list.
type coreDevice struct {
	Identifier         string `json:"identifier"`
	HardwareProperties struct {
		UDID string `json:"udid"`
	} `json:"hardwareProperties"`
	DeviceProperties struct {
		Name string `json:"name"`
	} `json:"deviceProperties"`
}

type coreDeviceList struct {
	Result struct {
		Devices []coreDevice `json:"devices"`
	} `json:"result"`
}

// listDevices is a seam: overridable in tests so ResolveDevice runs against
// canned devicectl output without invoking xcrun.
var listDevices = coreDevices

// ResolveDevice picks the physical iOS device a run drives. An empty query
// resolves the single connected device (an error names them all when several
// are connected). A non-empty query matches a device by name, hardware UDID, or
// CoreDevice id. No match and an ambiguous match are both errors that list the
// connected devices so the caller can refine --ios-device.
func ResolveDevice(ctx context.Context, query string) (Device, error) {
	devices, err := listDevices(ctx)
	if err != nil {
		return Device{}, fmt.Errorf("list devices: %w", err)
	}
	if len(devices) == 0 {
		return Device{}, fmt.Errorf("no connected iOS device found; connect and pair an iPhone, then check `xcrun devicectl list devices`")
	}

	if query == "" {
		if len(devices) == 1 {
			return devices[0], nil
		}
		return Device{}, fmt.Errorf("multiple connected iOS devices; pass --ios-device to select one:%s", deviceLines(devices))
	}

	var matches []Device
	for _, device := range devices {
		if device.Name == query || device.HardwareUDID == query || device.CoreDeviceID == query {
			matches = append(matches, device)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return Device{}, fmt.Errorf("no connected iOS device matches %q; connected devices:%s", query, deviceLines(devices))
	default:
		return Device{}, fmt.Errorf("--ios-device %q matches multiple devices; select by hardware UDID or CoreDevice id:%s", query, deviceLines(matches))
	}
}

func deviceLines(devices []Device) string {
	var lines strings.Builder
	for _, device := range devices {
		fmt.Fprintf(&lines, "\n  %s (udid %s, id %s)", device.Name, device.HardwareUDID, device.CoreDeviceID)
	}
	return lines.String()
}

// coreDevices invokes devicectl and parses its JSON device list. devicectl
// writes the JSON to a file path rather than stdout, so a temp file backs the
// --json-output flag and is read back after the command runs.
func coreDevices(ctx context.Context) ([]Device, error) {
	outputFile, err := os.CreateTemp("", "sanderling-devices-*.json")
	if err != nil {
		return nil, err
	}
	outputPath := outputFile.Name()
	outputFile.Close()
	defer os.Remove(outputPath)

	if err := exec.CommandContext(ctx, "xcrun", "devicectl", "list", "devices", "--json-output", outputPath).Run(); err != nil {
		return nil, fmt.Errorf("xcrun devicectl list devices: %w", err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, err
	}
	return parseDevices(data)
}

func parseDevices(data []byte) ([]Device, error) {
	var list coreDeviceList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	var devices []Device
	for _, entry := range list.Result.Devices {
		devices = append(devices, Device{
			Name:         entry.DeviceProperties.Name,
			HardwareUDID: entry.HardwareProperties.UDID,
			CoreDeviceID: entry.Identifier,
		})
	}
	return devices, nil
}
