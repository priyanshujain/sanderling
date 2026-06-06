// Package ios boots and prepares an iOS simulator for testing via simctl.
package ios

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

type simDevice struct {
	UDID        string `json:"udid"`
	State       string `json:"state"`
	Name        string `json:"name"`
	IsAvailable bool   `json:"isAvailable"`
}

type simctlDeviceList struct {
	Devices map[string][]simDevice `json:"devices"`
}

// Command-runner seams: overridable in tests so EnsureSimulator can be driven
// with canned device lists without invoking xcrun.
var (
	listBooted    = bootedSimulator
	listBootedAll = bootedSimulators
	listAvailable = availableSimulators
	boot          = bootSimulator
	waitForBoot   = waitForSimulatorBoot
)

func EnsureSimulator(ctx context.Context, deviceName string, stdout io.Writer) error {
	booted, err := listBooted(ctx)
	if err != nil {
		return fmt.Errorf("list booted simulators: %w", err)
	}
	if booted != nil {
		fmt.Fprintf(stdout, "using booted simulator: %s (%s)\n", booted.Name, booted.UDID)
		return nil
	}

	available, err := listAvailable(ctx)
	if err != nil {
		return fmt.Errorf("list available simulators: %w", err)
	}

	target, err := pickSimulator(deviceName, available)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "booting simulator %q (%s)...\n", target.Name, target.UDID)
	if err := boot(ctx, target.UDID); err != nil {
		return fmt.Errorf("boot simulator %q: %w", target.UDID, err)
	}

	if err := waitForBoot(ctx, target.UDID, 60*time.Second); err != nil {
		return fmt.Errorf("wait for simulator boot: %w", err)
	}

	fmt.Fprintf(stdout, "simulator %q ready\n", target.Name)
	return nil
}

// BootedUDID returns the UDID of the currently booted iOS simulator, or "" if none is booted.
func BootedUDID(ctx context.Context) string {
	d, _ := bootedSimulator(ctx)
	if d == nil {
		return ""
	}
	return d.UDID
}

// ResolveTarget decides which iOS target a run drives and whether it is a
// simulator. An explicit query matching a simulator name or UDID (booted or
// available) resolves to that simulator. With no query, exactly one booted
// simulator resolves to it; multiple booted simulators is an error that lists
// them and asks the caller to pass --ios-device. A query that matches no
// simulator resolves as a physical device (udid = query, simulator false) so
// the sidecar path drives it.
func ResolveTarget(ctx context.Context, query string) (udid string, isSimulator bool, err error) {
	if query != "" {
		booted, err := listBootedAll(ctx)
		if err != nil {
			return "", false, fmt.Errorf("list booted simulators: %w", err)
		}
		if match := matchSimulator(query, booted); match != nil {
			return match.UDID, true, nil
		}
		available, err := listAvailable(ctx)
		if err != nil {
			return "", false, fmt.Errorf("list available simulators: %w", err)
		}
		if match := matchSimulator(query, available); match != nil {
			return match.UDID, true, nil
		}
		return query, false, nil
	}

	booted, err := listBootedAll(ctx)
	if err != nil {
		return "", false, fmt.Errorf("list booted simulators: %w", err)
	}
	switch len(booted) {
	case 1:
		return booted[0].UDID, true, nil
	case 0:
		return "", false, fmt.Errorf("no booted iOS simulator found")
	default:
		var lines strings.Builder
		for _, device := range booted {
			fmt.Fprintf(&lines, "\n  %s (%s)", device.Name, device.UDID)
		}
		return "", false, fmt.Errorf("multiple booted iOS simulators; pass --ios-device to select one:%s", lines.String())
	}
}

func matchSimulator(query string, devices []simDevice) *simDevice {
	for i := range devices {
		if devices[i].Name == query || devices[i].UDID == query {
			return &devices[i]
		}
	}
	return nil
}

func bootedSimulator(ctx context.Context) (*simDevice, error) {
	out, err := exec.CommandContext(ctx, "xcrun", "simctl", "list", "devices", "booted", "--json").Output()
	if err != nil {
		return nil, err
	}
	return parseBootedDevice(out)
}

func bootedSimulators(ctx context.Context) ([]simDevice, error) {
	out, err := exec.CommandContext(ctx, "xcrun", "simctl", "list", "devices", "booted", "--json").Output()
	if err != nil {
		return nil, err
	}
	return parseBootedDevices(out)
}

func availableSimulators(ctx context.Context) ([]simDevice, error) {
	out, err := exec.CommandContext(ctx, "xcrun", "simctl", "list", "devices", "available", "--json").Output()
	if err != nil {
		return nil, err
	}
	return parseAvailableDevices(out)
}

func parseBootedDevice(out []byte) (*simDevice, error) {
	var list simctlDeviceList
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, err
	}
	for _, devices := range list.Devices {
		for i := range devices {
			if devices[i].State == "Booted" {
				return &devices[i], nil
			}
		}
	}
	return nil, nil
}

func parseBootedDevices(out []byte) ([]simDevice, error) {
	var list simctlDeviceList
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, err
	}
	var result []simDevice
	for _, devices := range list.Devices {
		for i := range devices {
			if devices[i].State == "Booted" {
				result = append(result, devices[i])
			}
		}
	}
	return result, nil
}

func parseAvailableDevices(out []byte) ([]simDevice, error) {
	var list simctlDeviceList
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, err
	}
	var result []simDevice
	for _, devices := range list.Devices {
		for _, d := range devices {
			if d.IsAvailable {
				result = append(result, d)
			}
		}
	}
	return result, nil
}

func pickSimulator(deviceName string, available []simDevice) (*simDevice, error) {
	if deviceName != "" {
		for i, d := range available {
			if d.Name == deviceName || d.UDID == deviceName {
				return &available[i], nil
			}
		}
		return nil, fmt.Errorf("simulator %q not found among available simulators; run `xcrun simctl list devices available` to see options", deviceName)
	}

	if len(available) == 0 {
		return nil, fmt.Errorf("no available iOS simulators found; create one in Xcode -> Window -> Devices and Simulators")
	}

	for i, d := range available {
		if strings.HasPrefix(d.Name, "iPhone") {
			return &available[i], nil
		}
	}
	return &available[0], nil
}

func bootSimulator(ctx context.Context, udid string) error {
	return exec.CommandContext(ctx, "xcrun", "simctl", "boot", udid).Run()
}

func waitForSimulatorBoot(ctx context.Context, udid string, timeout time.Duration) error {
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		booted, _ := bootedSimulator(deadline)
		if booted != nil && (booted.UDID == udid || udid == "") {
			return nil
		}
		select {
		case <-deadline.Done():
			return deadline.Err()
		case <-ticker.C:
		}
	}
}
