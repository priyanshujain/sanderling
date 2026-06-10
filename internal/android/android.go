// Package android boots and prepares an Android device or emulator for testing via adb.
package android

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// EnsureDevice makes sure an Android device is ready for adb commands.
// Resolution order:
//   - if serial is set, require that exact device to be online;
//   - else if an adb device is already online, use it;
//   - else if avdName is set, validate and boot it;
//   - else if exactly one AVD exists locally, boot it;
//   - else fail with a helpful message listing the available AVDs.
func EnsureDevice(ctx context.Context, serial, avdName string, stdout io.Writer) error {
	devices, err := listAdbDevices(ctx)
	if err != nil {
		return fmt.Errorf("list adb devices: %w", err)
	}
	chosen, found, err := pickDevice(serial, devices)
	if err != nil {
		return err
	}
	if found {
		fmt.Fprintf(stdout, "using connected device: %s\n", chosen)
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

// PrepareDevice keeps the device awake, wakes the screen, and dismisses a
// non-secure keyguard so the launched app stays in the foreground for the whole
// run. The fuzzer drives whatever is on screen, so a sleeping or locked device
// would have it explore system UI instead of the app. A secure lock
// (PIN/password) cannot be dismissed here and must be unlocked out of band.
//
// Every step is best effort: some OEM builds restrict or kill these commands
// (e.g. HyperOS SIGKILLs `svc power stayon`), and none is required for a run to
// proceed, so a failure is logged and skipped rather than aborting the run.
func PrepareDevice(ctx context.Context, serial string, stdout io.Writer) error {
	adb, err := AdbBinary()
	if err != nil {
		return err
	}
	for _, shellCommand := range append([][]string{
		{"svc", "power", "stayon", "true"},
		{"input", "keyevent", "KEYCODE_WAKEUP"},
		{"wm", "dismiss-keyguard"},
	}, antiFreezeCommands()...) {
		args := []string{}
		if serial != "" {
			args = append(args, "-s", serial)
		}
		args = append(append(args, "shell"), shellCommand...)
		if err := exec.CommandContext(ctx, adb, args...).Run(); err != nil {
			fmt.Fprintf(stdout, "device prep: skipping `adb %s` (%v)\n", strings.Join(shellCommand, " "), err)
		}
	}
	return nil
}

// driverPackages are the on-device native-driver instrumentation packages that
// the platform and OEM background freezers must not suspend mid-run.
var driverPackages = []string{"dev.mobile.maestro", "dev.mobile.maestro.test"}

// antiFreezeCommands turns off the background-process freezers that suspend the
// driver between actions. Android 12+ adds a cached-app freezer and a
// phantom-process killer; OEM builds (e.g. OnePlus/Oppo ColorOS OSense) add
// their own. Left on, they freeze the driver instrumentation while the app is
// foreground and the run stalls. set_sync_disabled_for_tests keeps the
// device_config writes from being reverted by server-side sync. All best effort:
// the caller skips and logs any command an OEM build rejects.
func antiFreezeCommands() [][]string {
	commands := [][]string{
		{"device_config", "set_sync_disabled_for_tests", "persistent"},
		{"device_config", "put", "activity_manager_native_boot", "use_freezer", "false"},
		{"device_config", "put", "activity_manager_native_boot", "freeze_exempt_inst_pkg", strings.Join(driverPackages, ",")},
		{"settings", "put", "global", "settings_enable_monitor_phantom_procs", "false"},
		{"device_config", "put", "activity_manager", "max_phantom_processes", "2147483647"},
		{"dumpsys", "deviceidle", "disable"},
	}
	for _, pkg := range driverPackages {
		commands = append(commands, []string{"dumpsys", "deviceidle", "whitelist", "+" + pkg})
	}
	return commands
}

// ReinstallApp resets an app to first-launch state by uninstalling and
// reinstalling it. This replaces `pm clear` for clear-state: ColorOS and other
// hardened OEM builds deny CLEAR_APP_USER_DATA even to the adb shell user, so a
// clear aborts the launch, whereas uninstall+install is always permitted.
// The uninstall is best effort so a not-installed app is not an error.
func ReinstallApp(ctx context.Context, serial, bundleID, apkPath string, stdout io.Writer) error {
	adb, err := AdbBinary()
	if err != nil {
		return err
	}
	withSerial := func(args ...string) []string {
		if serial != "" {
			return append([]string{"-s", serial}, args...)
		}
		return args
	}
	if output, err := exec.CommandContext(ctx, adb, withSerial("uninstall", bundleID)...).CombinedOutput(); err != nil {
		fmt.Fprintf(stdout, "clear-state: uninstall %s skipped (%v: %s)\n", bundleID, err, strings.TrimSpace(string(output)))
	}
	if output, err := exec.CommandContext(ctx, adb, withSerial("install", "-r", apkPath)...).CombinedOutput(); err != nil {
		return fmt.Errorf("install %s: %w: %s", apkPath, err, strings.TrimSpace(string(output)))
	}
	return nil
}

const threeButtonNavOverlay = "com.android.internal.systemui.navbar.threebutton"

// navModeOverlays are the system navigation-mode overlays. Only one is active at
// a time; the active one is restored after the run.
var navModeOverlays = []string{
	"com.android.internal.systemui.navbar.gestural",
	threeButtonNavOverlay,
	"com.android.internal.systemui.navbar.twobutton",
}

// ForceThreeButtonNav switches the device to 3-button navigation for the run, so
// the fuzzer's swipes cannot trigger the gesture-nav home/back actions and fling
// the app off screen (the nav bar's own buttons are systemui-owned and already
// dropped from action candidates). It returns a function that restores the
// original navigation mode. Best effort: on any failure it leaves navigation
// untouched and returns a no-op restore.
func ForceThreeButtonNav(ctx context.Context, serial string, stdout io.Writer) func() {
	adb, err := AdbBinary()
	if err != nil {
		return func() {}
	}
	original := enabledNavOverlay(ctx, adb, serial)
	if err := navOverlayCommand(ctx, adb, serial, threeButtonNavOverlay).Run(); err != nil {
		fmt.Fprintf(stdout, "device prep: skipping 3-button nav (%v)\n", err)
		return func() {}
	}
	if original == "" || original == threeButtonNavOverlay {
		return func() {}
	}
	return func() {
		if err := navOverlayCommand(context.Background(), adb, serial, original).Run(); err != nil {
			fmt.Fprintf(stdout, "device prep: could not restore nav mode %s (%v)\n", original, err)
		}
	}
}

// enabledNavOverlay returns the currently active navigation-mode overlay, or ""
// when it cannot be determined.
func enabledNavOverlay(ctx context.Context, adb, serial string) string {
	args := []string{}
	if serial != "" {
		args = append(args, "-s", serial)
	}
	args = append(args, "shell", "cmd", "overlay", "list")
	output, err := exec.CommandContext(ctx, adb, args...).Output()
	if err != nil {
		return ""
	}
	return parseEnabledNavOverlay(string(output))
}

// parseEnabledNavOverlay reads `cmd overlay list` output and returns the
// enabled ("[x]") navigation-mode overlay package.
func parseEnabledNavOverlay(overlayList string) string {
	for line := range strings.SplitSeq(overlayList, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "[x]") {
			continue
		}
		package_ := strings.TrimSpace(strings.TrimPrefix(trimmed, "[x]"))
		if slices.Contains(navModeOverlays, package_) {
			return package_
		}
	}
	return ""
}

func navOverlayCommand(ctx context.Context, adb, serial, overlay string) *exec.Cmd {
	args := []string{}
	if serial != "" {
		args = append(args, "-s", serial)
	}
	args = append(args, "shell", "cmd", "overlay", "enable-exclusive", overlay)
	return exec.CommandContext(ctx, adb, args...)
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

// pickDevice resolves which connected device to drive. A requested serial must
// be online; with no request the first connected device is used, else found is
// false so the caller falls back to booting an AVD.
func pickDevice(requested string, connected []string) (serial string, found bool, err error) {
	if requested != "" {
		if !slices.Contains(connected, requested) {
			return "", false, fmt.Errorf("device %q is not connected (online devices: %s)", requested, strings.Join(connected, ", "))
		}
		return requested, true, nil
	}
	if len(connected) > 0 {
		return connected[0], true, nil
	}
	return "", false, nil
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

// ForegroundPackage returns the package of the currently resumed activity on
// the connected device, or "" when it cannot be determined.
func ForegroundPackage(ctx context.Context) (string, error) {
	adb, err := AdbBinary()
	if err != nil {
		return "", err
	}
	output, err := exec.CommandContext(ctx, adb, "shell", "dumpsys", "activity", "activities").Output()
	if err != nil {
		return "", err
	}
	return parseForegroundPackage(string(output)), nil
}

// FocusedWindowPackage returns the package owning the currently focused window,
// or "" when no window is focused (e.g. mid-launch, before the app has drawn).
// Unlike ForegroundPackage, this reflects what is actually on screen:
// ResumedActivity flips to a newly launched app before its first frame renders,
// while mCurrentFocus only names the app once its window is up.
func FocusedWindowPackage(ctx context.Context) (string, error) {
	adb, err := AdbBinary()
	if err != nil {
		return "", err
	}
	// Grep the focus line on-device: the full dumpsys window output is large and
	// this runs on the per-step scope guard, so transferring it whole would add
	// latency to every step.
	output, err := exec.CommandContext(ctx, adb, "shell", "dumpsys window | grep mCurrentFocus").Output()
	if err != nil {
		return "", err
	}
	return parseFocusedWindowPackage(string(output)), nil
}

// resumedActivityPackage matches the "<package>/<activity>" component name that
// dumpsys prints on ResumedActivity and mCurrentFocus lines, capturing the
// package.
var resumedActivityPackage = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9_.]*)/[a-zA-Z0-9_.$]+`)

// parseForegroundPackage extracts the foreground package from `dumpsys activity
// activities` output by reading the first ResumedActivity component name.
func parseForegroundPackage(dumpsys string) string {
	for line := range strings.SplitSeq(dumpsys, "\n") {
		if !strings.Contains(line, "ResumedActivity") {
			continue
		}
		if match := resumedActivityPackage.FindStringSubmatch(line); match != nil {
			return match[1]
		}
	}
	return ""
}

// systemUIPackage is the owner reported for system overlays (notification
// shade, quick settings) that take window focus without a package/activity
// component name. The scope guard treats it as "not the app" and dismisses it.
const systemUIPackage = "com.android.systemui"

// systemOverlayWindowNames are the mCurrentFocus window names for the system
// panels a fuzzer gesture can pull over the app (a swipe from the status bar
// opens NotificationShade). They own focus while the app stays the resumed
// activity, so the resumed-activity signal alone misses them.
var systemOverlayWindowNames = []string{"NotificationShade", "ShadePanel", "QuickSettings", "VolumeUiDialog"}

// parseFocusedWindowPackage extracts the focused-window package from
// `dumpsys window` output by reading the mCurrentFocus component name. A
// "mCurrentFocus=null" line (no focused window) yields "". A system overlay
// (e.g. the notification shade) yields systemUIPackage so callers can tell it
// apart from the app and from "no focus".
func parseFocusedWindowPackage(dumpsys string) string {
	for line := range strings.SplitSeq(dumpsys, "\n") {
		if !strings.Contains(line, "mCurrentFocus") {
			continue
		}
		if match := resumedActivityPackage.FindStringSubmatch(line); match != nil {
			return match[1]
		}
		for _, overlay := range systemOverlayWindowNames {
			if strings.Contains(line, overlay) {
				return systemUIPackage
			}
		}
	}
	return ""
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
