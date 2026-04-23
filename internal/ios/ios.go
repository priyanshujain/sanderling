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

func EnsureSimulator(ctx context.Context, deviceName string, stdout io.Writer) error {
	booted, err := bootedSimulator(ctx)
	if err != nil {
		return fmt.Errorf("list booted simulators: %w", err)
	}
	if booted != nil {
		fmt.Fprintf(stdout, "using booted simulator: %s (%s)\n", booted.Name, booted.UDID)
		return nil
	}

	available, err := availableSimulators(ctx)
	if err != nil {
		return fmt.Errorf("list available simulators: %w", err)
	}

	target, err := pickSimulator(deviceName, available)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "booting simulator %q (%s)...\n", target.Name, target.UDID)
	if err := bootSimulator(ctx, target.UDID); err != nil {
		return fmt.Errorf("boot simulator %q: %w", target.UDID, err)
	}

	if err := waitForSimulatorBoot(ctx, target.UDID, 60*time.Second); err != nil {
		return fmt.Errorf("wait for simulator boot: %w", err)
	}

	fmt.Fprintf(stdout, "simulator %q ready\n", target.Name)
	return nil
}

// BootedUDID returns the UDID of the currently booted iOS simulator, or "" if none is booted.
func BootedUDID(ctx context.Context) (string, error) {
	d, err := bootedSimulator(ctx)
	if err != nil {
		return "", err
	}
	if d == nil {
		return "", nil
	}
	return d.UDID, nil
}

func bootedSimulator(ctx context.Context) (*simDevice, error) {
	out, err := exec.CommandContext(ctx, "xcrun", "simctl", "list", "devices", "booted", "--json").Output()
	if err != nil {
		return nil, err
	}
	var list simctlDeviceList
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, err
	}
	for _, devices := range list.Devices {
		for _, d := range devices {
			if d.State == "Booted" {
				return &d, nil
			}
		}
	}
	return nil, nil
}

func availableSimulators(ctx context.Context) ([]simDevice, error) {
	out, err := exec.CommandContext(ctx, "xcrun", "simctl", "list", "devices", "available", "--json").Output()
	if err != nil {
		return nil, err
	}
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
