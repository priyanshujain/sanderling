package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// ensureDevice returns the serial of an Android device ready for adb
// commands. Resolution order:
//   - if an adb device is already online, use it (and if avdName is non-empty
//     and doesn't match any running device, that mismatch is ignored);
//   - else if avdName is set, boot that AVD and wait for it;
//   - else fail.
func ensureDevice(ctx context.Context, avdName string, stdout io.Writer) error {
	devices, err := listAdbDevices(ctx)
	if err != nil {
		return fmt.Errorf("list adb devices: %w", err)
	}
	if len(devices) > 0 {
		fmt.Fprintf(stdout, "using connected device: %s\n", devices[0])
		return nil
	}
	if avdName == "" {
		return fmt.Errorf("no android device connected and --avd not provided")
	}
	avds, err := listAVDs(ctx)
	if err != nil {
		return fmt.Errorf("list AVDs: %w", err)
	}
	if !slices.Contains(avds, avdName) {
		return fmt.Errorf("AVD %q does not exist (available: %s)", avdName, strings.Join(avds, ", "))
	}
	fmt.Fprintf(stdout, "booting AVD %q...\n", avdName)
	if err := bootAVD(ctx, avdName); err != nil {
		return fmt.Errorf("boot AVD %q: %w", avdName, err)
	}
	if err := waitForBoot(ctx, 180*time.Second); err != nil {
		return fmt.Errorf("wait for AVD boot: %w", err)
	}
	fmt.Fprintf(stdout, "AVD %q ready\n", avdName)
	return nil
}

func listAdbDevices(ctx context.Context) ([]string, error) {
	output, err := exec.CommandContext(ctx, "adb", "devices").Output()
	if err != nil {
		return nil, err
	}
	return parseAdbDevices(string(output)), nil
}

func parseAdbDevices(output string) []string {
	var serials []string
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "List of devices") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "device" {
			serials = append(serials, fields[0])
		}
	}
	return serials
}

func listAVDs(ctx context.Context) ([]string, error) {
	emulator, err := emulatorBinary()
	if err != nil {
		return nil, err
	}
	output, err := exec.CommandContext(ctx, emulator, "-list-avds").Output()
	if err != nil {
		return nil, err
	}
	return parseAVDList(string(output)), nil
}

// emulatorBinary returns the path to the Android `emulator` binary, preferring
// PATH and falling back to $ANDROID_HOME/emulator/emulator.
func emulatorBinary() (string, error) {
	if path, err := exec.LookPath("emulator"); err == nil {
		return path, nil
	}
	if androidHome := os.Getenv("ANDROID_HOME"); androidHome != "" {
		candidate := filepath.Join(androidHome, "emulator", "emulator")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("`emulator` binary not found on PATH or under $ANDROID_HOME/emulator/")
}

func parseAVDList(output string) []string {
	var avds []string
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "INFO") {
			continue
		}
		avds = append(avds, line)
	}
	return avds
}

func bootAVD(_ context.Context, name string) error {
	emulator, err := emulatorBinary()
	if err != nil {
		return err
	}
	command := exec.Command(emulator, "-avd", name, "-no-snapshot-save", "-no-audio", "-no-boot-anim")
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}

func waitForBoot(ctx context.Context, timeout time.Duration) error {
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if completed, _ := bootCompleted(deadline); completed {
			return nil
		}
		select {
		case <-deadline.Done():
			return deadline.Err()
		case <-ticker.C:
		}
	}
}

func bootCompleted(ctx context.Context) (bool, error) {
	output, err := exec.CommandContext(ctx, "adb", "shell", "getprop", "sys.boot_completed").Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(output)) == "1", nil
}

