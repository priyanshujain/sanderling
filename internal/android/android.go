package android

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

// EnsureDevice makes sure an Android device is ready for adb commands.
// Resolution order:
//   - if an adb device is already online, use it;
//   - else if avdName is set, validate and boot it;
//   - else if exactly one AVD exists locally, boot it;
//   - else fail with a helpful message listing the available AVDs.
func EnsureDevice(ctx context.Context, avdName string, stdout io.Writer) error {
	devices, err := listAdbDevices(ctx)
	if err != nil {
		return fmt.Errorf("list adb devices: %w", err)
	}
	if len(devices) > 0 {
		fmt.Fprintf(stdout, "using connected device: %s\n", devices[0])
		return nil
	}
	avds, err := listAVDs(ctx)
	if err != nil {
		return fmt.Errorf("list AVDs: %w", err)
	}
	target, err := pickAVD(avdName, avds)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "booting AVD %q...\n", target)
	if err := bootAVD(ctx, target); err != nil {
		return fmt.Errorf("boot AVD %q: %w", target, err)
	}
	if err := waitForBoot(ctx, 180*time.Second); err != nil {
		return fmt.Errorf("wait for AVD boot: %w", err)
	}
	fmt.Fprintf(stdout, "AVD %q ready\n", target)
	return nil
}

// AdbReverse sets up adb reverse forwarding for a local abstract socket.
func AdbReverse(socket string, port int) error {
	adb, err := AdbBinary()
	if err != nil {
		return err
	}
	command := exec.Command(adb, "reverse", "localabstract:"+socket, fmt.Sprintf("tcp:%d", port))
	return command.Run()
}

// AdbReverseRemove removes an adb reverse forwarding rule.
func AdbReverseRemove(socket string) error {
	adb, err := AdbBinary()
	if err != nil {
		return err
	}
	return exec.Command(adb, "reverse", "--remove", "localabstract:"+socket).Run()
}

// EnvWithAndroidPlatformTools returns env with the directory containing adb
// prepended to PATH, so child processes (the sidecar) can invoke adb even
// when the user hasn't set up their shell PATH.
func EnvWithAndroidPlatformTools(env []string) []string {
	adb, err := AdbBinary()
	if err != nil {
		return env
	}
	adbDir := filepath.Dir(adb)
	result := make([]string, 0, len(env))
	found := false
	for _, entry := range env {
		if current, ok := strings.CutPrefix(entry, "PATH="); ok {
			if !pathContains(current, adbDir) {
				entry = "PATH=" + adbDir + string(os.PathListSeparator) + current
			}
			found = true
		}
		result = append(result, entry)
	}
	if !found {
		result = append(result, "PATH="+adbDir)
	}
	return result
}

// AdbBinary locates the adb binary via PATH or known Android SDK locations.
func AdbBinary() (string, error) { return findAndroidTool("adb", "platform-tools") }

func emulatorBinary() (string, error) { return findAndroidTool("emulator", "emulator") }

// findAndroidTool locates a binary from the Android SDK. It checks PATH,
// then $ANDROID_HOME/<subdir>/<name> and $ANDROID_SDK_ROOT/<subdir>/<name>,
// then the canonical install locations used by Android Studio and Homebrew.
func findAndroidTool(name, subdir string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	var tried []string
	for _, root := range androidSDKCandidates() {
		candidate := filepath.Join(root, subdir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
		tried = append(tried, candidate)
	}
	return "", fmt.Errorf("could not locate %q: not on PATH and not under any known Android SDK root (set $ANDROID_HOME to point at your SDK; tried %v)", name, tried)
}

func androidSDKCandidates() []string {
	var roots []string
	seen := map[string]bool{}
	addRoot := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		roots = append(roots, path)
	}
	addRoot(os.Getenv("ANDROID_HOME"))
	addRoot(os.Getenv("ANDROID_SDK_ROOT"))
	if home, err := os.UserHomeDir(); err == nil {
		addRoot(filepath.Join(home, "Library", "Android", "sdk"))
		addRoot(filepath.Join(home, "Android", "Sdk"))
	}
	addRoot("/opt/homebrew/share/android-commandlinetools")
	addRoot("/usr/local/share/android-commandlinetools")
	return roots
}

func listAdbDevices(ctx context.Context) ([]string, error) {
	adb, err := AdbBinary()
	if err != nil {
		return nil, err
	}
	output, err := exec.CommandContext(ctx, adb, "devices").Output()
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

func pickAVD(requested string, available []string) (string, error) {
	if requested != "" {
		if !slices.Contains(available, requested) {
			return "", fmt.Errorf("AVD %q does not exist (available: %s)", requested, strings.Join(available, ", "))
		}
		return requested, nil
	}
	switch len(available) {
	case 0:
		return "", fmt.Errorf("no android device connected and no AVD found; create one in Android Studio or `avdmanager create avd`")
	case 1:
		return available[0], nil
	default:
		return "", fmt.Errorf("no android device connected and multiple AVDs available (%s); pick one with --avd", strings.Join(available, ", "))
	}
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
	adb, err := AdbBinary()
	if err != nil {
		return false, err
	}
	output, err := exec.CommandContext(ctx, adb, "shell", "getprop", "sys.boot_completed").Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(output)) == "1", nil
}

func pathContains(path, directory string) bool {
	return slices.Contains(strings.Split(path, string(os.PathListSeparator)), directory)
}
