package android

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestWakeCommands(t *testing.T) {
	want := [][]string{
		{"svc", "power", "stayon", "true"},
		{"input", "keyevent", "KEYCODE_WAKEUP"},
		{"wm", "dismiss-keyguard"},
	}
	if got := wakeCommands(); !reflect.DeepEqual(got, want) {
		t.Errorf("wakeCommands() = %v, want %v", got, want)
	}
}

func TestParseAdbDevices_OnlineOnly(t *testing.T) {
	output := `List of devices attached
emulator-5554	device
emulator-5556	offline
physical-abc	device
`

	got := parseAdbDevices(output)
	want := []string{"emulator-5554", "physical-abc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseAdbDevices_Empty(t *testing.T) {
	output := "List of devices attached\n\n"

	got := parseAdbDevices(output)
	if len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}

func TestPickDevice_RequestedOnline(t *testing.T) {
	serial, found, err := pickDevice("physical-abc", []string{"emulator-5554", "physical-abc"})
	if err != nil || !found || serial != "physical-abc" {
		t.Fatalf("got (%q, %v, %v), want (physical-abc, true, nil)", serial, found, err)
	}
}

func TestPickDevice_RequestedNotConnected(t *testing.T) {
	_, found, err := pickDevice("physical-abc", []string{"emulator-5554"})
	if err == nil {
		t.Fatal("expected error for a serial that is not connected")
	}
	if found {
		t.Fatal("found must be false when the requested device is absent")
	}
}

func TestPickDevice_NoRequestSingleDeviceUsesIt(t *testing.T) {
	serial, found, err := pickDevice("", []string{"emulator-5554"})
	if err != nil || !found || serial != "emulator-5554" {
		t.Fatalf("got (%q, %v, %v), want (emulator-5554, true, nil)", serial, found, err)
	}
}

func TestPickDevice_NoRequestMultipleDevicesErrors(t *testing.T) {
	serial, found, err := pickDevice("", []string{"emulator-5554", "physical-abc"})
	if err == nil {
		t.Fatal("expected an error asking for --device when several devices are connected")
	}
	if found || serial != "" {
		t.Fatalf("ambiguous selection must not pick a device, got (%q, %v)", serial, found)
	}
}

func TestPickDevice_NoneConnectedFallsBackToAVD(t *testing.T) {
	serial, found, err := pickDevice("", nil)
	if err != nil || found || serial != "" {
		t.Fatalf("got (%q, %v, %v), want (\"\", false, nil)", serial, found, err)
	}
}

func TestParseAVDList_DropsInfoLines(t *testing.T) {
	output := `INFO    | Storing crashdata in: /tmp/x
Medium_Phone_API_36.0
sanderling_test
`

	got := parseAVDList(output)
	want := []string{"Medium_Phone_API_36.0", "sanderling_test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPickAVD_ExplicitName(t *testing.T) {
	got, err := pickAVD("Pixel_7", []string{"Pixel_7", "sanderling_test"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "Pixel_7" {
		t.Fatalf("got %q, want Pixel_7", got)
	}
}

func TestPickAVD_ExplicitMissing(t *testing.T) {
	_, err := pickAVD("Nope", []string{"Pixel_7"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPickAVD_SingleAvailable(t *testing.T) {
	got, err := pickAVD("", []string{"sanderling_test"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "sanderling_test" {
		t.Fatalf("got %q, want sanderling_test", got)
	}
}

func TestPickAVD_AmbiguousWithoutHint(t *testing.T) {
	_, err := pickAVD("", []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error when multiple AVDs and no --avd")
	}
}

func TestPickAVD_NoneAvailable(t *testing.T) {
	_, err := pickAVD("", nil)
	if err == nil {
		t.Fatal("expected error when no AVDs exist")
	}
}

// fakeSDK writes an SDK layout holding only the named tools ("emulator/emulator"),
// so a lookup test never resolves against the host's own SDK.
func fakeSDK(t *testing.T, tools ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, tool := range tools {
		path := filepath.Join(root, filepath.FromSlash(tool))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, nil, 0o755); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return root
}

// isolateSDKLookup closes every route to the host's own SDK: an empty PATH, no
// root variables, a home with nothing under it, and the standard install
// locations replaced by the given roots. It returns the fake home directory.
func isolateSDKLookup(t *testing.T, roots ...string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("ANDROID_HOME", "")
	t.Setenv("ANDROID_SDK_ROOT", "")
	t.Setenv("HOME", home)
	original := standardSDKRoots
	t.Cleanup(func() { standardSDKRoots = original })
	standardSDKRoots = roots
	return home
}

func TestAdbBinary_ResolvesUnderStandardSDKRootWithAndroidHomeUnset(t *testing.T) {
	sdk := fakeSDK(t, "platform-tools/adb")
	isolateSDKLookup(t, sdk)

	adb, err := AdbBinary()
	if err != nil {
		t.Fatalf("AdbBinary: %v", err)
	}
	if want := filepath.Join(sdk, "platform-tools", "adb"); adb != want {
		t.Errorf("AdbBinary() = %q, want %q", adb, want)
	}
}

func TestEmulatorBinary_ResolvesUnderStandardSDKRootWithAndroidHomeUnset(t *testing.T) {
	sdk := fakeSDK(t, "emulator/emulator")
	isolateSDKLookup(t, sdk)

	emulator, err := EmulatorBinary()
	if err != nil {
		t.Fatalf("EmulatorBinary: %v", err)
	}
	if want := filepath.Join(sdk, "emulator", "emulator"); emulator != want {
		t.Errorf("EmulatorBinary() = %q, want %q", emulator, want)
	}
}

func TestAdbBinary_UsesAndroidHome(t *testing.T) {
	sdk := fakeSDK(t, "platform-tools/adb")
	isolateSDKLookup(t)
	t.Setenv("ANDROID_HOME", sdk)

	adb, err := AdbBinary()
	if err != nil {
		t.Fatalf("AdbBinary: %v", err)
	}
	if want := filepath.Join(sdk, "platform-tools", "adb"); adb != want {
		t.Errorf("AdbBinary() = %q, want %q", adb, want)
	}
}

func TestAdbBinary_NoSDKAnywhereReportsWhereItLooked(t *testing.T) {
	empty := t.TempDir()
	home := isolateSDKLookup(t, empty)

	_, err := AdbBinary()
	if err == nil {
		t.Fatal("expected an error with no SDK anywhere")
	}
	for _, want := range []string{
		"adb not found: not on PATH",
		filepath.Join(home, "Library", "Android", "sdk", "platform-tools", "adb"),
		filepath.Join(empty, "platform-tools", "adb"),
		"$ANDROID_HOME and $ANDROID_SDK_ROOT are unset",
		"put adb on PATH, or point $ANDROID_HOME at an Android SDK that has platform-tools/adb",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestAdbBinary_AndroidHomePointingNowhereIsNamed(t *testing.T) {
	isolateSDKLookup(t, t.TempDir())
	missing := filepath.Join(t.TempDir(), "no-such-sdk")
	t.Setenv("ANDROID_HOME", missing)

	_, err := AdbBinary()
	if err == nil {
		t.Fatal("expected an error when ANDROID_HOME points nowhere")
	}
	if want := "$ANDROID_HOME=" + missing + " does not exist"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q must say %q rather than leaving it in the tried list", err, want)
	}
}

func TestPathContains(t *testing.T) {
	path := "/usr/bin:/opt/tools:/usr/local/bin"
	if !pathContains(path, "/opt/tools") {
		t.Error("expected /opt/tools in PATH")
	}
	if pathContains(path, "/nope") {
		t.Error("did not expect /nope in PATH")
	}
}

func TestParseForegroundPackage(t *testing.T) {
	cases := []struct {
		name    string
		dumpsys string
		want    string
	}{
		{
			name:    "mResumedActivity folio",
			dumpsys: "  Stack #0:\n    mResumedActivity: ActivityRecord{a1b2c3 u0 app.folio/.MainActivity t42}\n",
			want:    "app.folio",
		},
		{
			name:    "topResumedActivity chrome",
			dumpsys: "ResumedActivity: ActivityRecord{ff u0 com.android.chrome/com.google.android.apps.chrome.Main t9}\n  topResumedActivity=ActivityRecord{ff u0 com.android.chrome/com.google.android.apps.chrome.Main}",
			want:    "com.android.chrome",
		},
		{
			name:    "launcher",
			dumpsys: "    mResumedActivity: ActivityRecord{x u0 com.google.android.apps.nexuslauncher/.NexusLauncherActivity t1}",
			want:    "com.google.android.apps.nexuslauncher",
		},
		{
			name:    "no resumed activity",
			dumpsys: "  some unrelated dumpsys output\n  with no resumed line\n",
			want:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseForegroundPackage(tc.dumpsys); got != tc.want {
				t.Errorf("parseForegroundPackage = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseFocusedWindowPackage(t *testing.T) {
	cases := []struct {
		name    string
		dumpsys string
		want    string
	}{
		{
			name:    "folio focused",
			dumpsys: "  mCurrentFocus=Window{e00f63a u0 app.folio/app.folio.MainActivity}\n  mFocusedApp=ActivityRecord{c0 u0 app.folio/.MainActivity t202}",
			want:    "app.folio",
		},
		{
			name:    "settings focused",
			dumpsys: "  mCurrentFocus=Window{709 u0 com.android.settings/com.android.settings.SubSettings}",
			want:    "com.android.settings",
		},
		{
			name:    "no focused window mid-launch",
			dumpsys: "  mCurrentFocus=null\n  mFocusedApp=null",
			want:    "",
		},
		{
			name:    "no focus line",
			dumpsys: "  some unrelated dumpsys window output\n",
			want:    "",
		},
		{
			name:    "notification shade focused",
			dumpsys: "  mCurrentFocus=Window{885e289 u0 NotificationShade}",
			want:    "com.android.systemui",
		},
		{
			name:    "quick settings focused",
			dumpsys: "  mCurrentFocus=Window{abc u0 QuickSettings}",
			want:    "com.android.systemui",
		},
		{
			name:    "volume dialog focused",
			dumpsys: "  mCurrentFocus=Window{abc u0 VolumeUiDialog}",
			want:    "com.android.systemui",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseFocusedWindowPackage(tc.dumpsys); got != tc.want {
				t.Errorf("parseFocusedWindowPackage = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseEnabledNavOverlay(t *testing.T) {
	cases := []struct {
		name    string
		listing string
		want    string
	}{
		{
			name:    "gesture nav active",
			listing: "[ ] com.android.internal.systemui.navbar.threebutton\n[x] com.android.internal.systemui.navbar.gestural\n[ ] com.android.internal.systemui.navbar.transparent",
			want:    "com.android.internal.systemui.navbar.gestural",
		},
		{
			name:    "three-button active",
			listing: "[x] com.android.internal.systemui.navbar.threebutton\n[ ] com.android.internal.systemui.navbar.gestural",
			want:    "com.android.internal.systemui.navbar.threebutton",
		},
		{
			name:    "two-button active",
			listing: "[x] com.android.internal.systemui.navbar.twobutton\n[ ] com.android.internal.systemui.navbar.gestural",
			want:    "com.android.internal.systemui.navbar.twobutton",
		},
		{
			name:    "ignores enabled non-nav overlays",
			listing: "[x] com.some.other.overlay\n[ ] com.android.internal.systemui.navbar.gestural",
			want:    "",
		},
		{
			name:    "no overlay enabled",
			listing: "[ ] com.android.internal.systemui.navbar.gestural\n[ ] com.android.internal.systemui.navbar.threebutton",
			want:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseEnabledNavOverlay(tc.listing); got != tc.want {
				t.Errorf("parseEnabledNavOverlay = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAntiFreezeCommands_DisablesFreezersAndExemptsDriver(t *testing.T) {
	commands := antiFreezeCommands()
	has := func(want ...string) bool {
		return slices.ContainsFunc(commands, func(c []string) bool { return slices.Equal(c, want) })
	}
	for _, want := range [][]string{
		{"device_config", "set_sync_disabled_for_tests", "persistent"},
		{"device_config", "put", "activity_manager_native_boot", "use_freezer", "false"},
		{"settings", "put", "global", "settings_enable_monitor_phantom_procs", "false"},
		{"dumpsys", "deviceidle", "disable"},
	} {
		if !has(want...) {
			t.Errorf("anti-freeze commands missing exact command %v", want)
		}
	}

	// The freezer exemption must be one device_config command whose final
	// argument lists every driver package, not just the package string
	// appearing somewhere among the commands.
	exemption := findCommand(commands, "device_config", "put", "activity_manager_native_boot", "freeze_exempt_inst_pkg")
	if exemption == nil {
		t.Fatalf("no freeze_exempt_inst_pkg command found in %v", commands)
	}
	value := exemption[len(exemption)-1]
	for _, pkg := range driverPackages {
		if !strings.Contains(value, pkg) {
			t.Errorf("freeze_exempt_inst_pkg value %q missing driver package %q", value, pkg)
		}
		if !has("dumpsys", "deviceidle", "whitelist", "+"+pkg) {
			t.Errorf("driver package %q not whitelisted from doze", pkg)
		}
	}
}

// findCommand returns the first command whose leading tokens equal prefix.
func findCommand(commands [][]string, prefix ...string) []string {
	for _, c := range commands {
		if len(c) >= len(prefix) && slices.Equal(c[:len(prefix)], prefix) {
			return c
		}
	}
	return nil
}

func TestNavModeToRestore(t *testing.T) {
	cases := map[string]string{
		"com.android.internal.systemui.navbar.gestural":    "com.android.internal.systemui.navbar.gestural",
		"com.android.internal.systemui.navbar.twobutton":   "com.android.internal.systemui.navbar.twobutton",
		"com.android.internal.systemui.navbar.threebutton": "", // already 3-button: nothing to change
		"": "", // unknown current mode: must not switch what cannot be restored
	}
	for original, want := range cases {
		if got := navModeToRestore(original); got != want {
			t.Errorf("navModeToRestore(%q) = %q, want %q", original, got, want)
		}
	}
}

// scriptedAdb puts an adb under a fake SDK root that logs each invocation's
// arguments and answers from replies, a `case "$*" in` body. SDK lookup is
// isolated onto that root, so ReinstallApp runs its real command sequence
// against the script and the log holds what reached adb.
func scriptedAdb(t *testing.T, replies string) string {
	t.Helper()
	root := t.TempDir()
	log := filepath.Join(root, "adb.log")
	adb := filepath.Join(root, "platform-tools", "adb")
	if err := os.MkdirAll(filepath.Dir(adb), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(adb), err)
	}
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\ncase \"$*\" in\n" + replies + "\nesac\n"
	if err := os.WriteFile(adb, []byte(script), 0o755); err != nil {
		t.Fatalf("write %s: %v", adb, err)
	}
	isolateSDKLookup(t, root)
	return log
}

func adbCalls(t *testing.T, log string) []string {
	t.Helper()
	contents, err := os.ReadFile(log)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read %s: %v", log, err)
	}
	return strings.Split(strings.TrimSpace(string(contents)), "\n")
}

const (
	uninstallFails  = `"uninstall "*) echo "Failure [DELETE_FAILED_INTERNAL_ERROR]"; exit 1;;`
	stillInstalled  = `"shell pm path "*) echo "package:/data/app/app.example-1/base.apk";;`
	notInstalled    = `"shell pm path "*) exit 1;;`
	installSucceeds = `"install "*) echo "Success";;`
)

func TestReinstallApp_RefusedUninstallClearsTheDataItLeftBehind(t *testing.T) {
	log := scriptedAdb(t, strings.Join([]string{
		uninstallFails,
		stillInstalled,
		`"shell pm clear "*) echo "Success";;`,
		installSucceeds,
	}, "\n"))
	output := &strings.Builder{}

	if err := ReinstallApp(t.Context(), "", "app.example", "/tmp/app.apk", output); err != nil {
		t.Fatalf("ReinstallApp: %v", err)
	}

	want := []string{
		"uninstall app.example",
		"shell pm path app.example",
		"shell pm clear app.example",
		"install -r /tmp/app.apk",
	}
	if got := adbCalls(t, log); !slices.Equal(got, want) {
		t.Fatalf("adb calls = %v, want %v", got, want)
	}
	if !strings.Contains(output.String(), "pm clear") {
		t.Errorf("output %q does not say the data was cleared some other way", output.String())
	}
}

func TestReinstallApp_RefusedUninstallThatCannotBeClearedIsFatal(t *testing.T) {
	log := scriptedAdb(t, strings.Join([]string{
		uninstallFails,
		stillInstalled,
		`"shell pm clear "*) echo "Failed"; exit 1;;`,
		installSucceeds,
	}, "\n"))

	err := ReinstallApp(t.Context(), "", "app.example", "/tmp/app.apk", &strings.Builder{})

	if err == nil {
		t.Fatal("ReinstallApp reported success while app.example kept the data clear-state was asked to remove")
	}
	for _, want := range []string{"app.example", "DELETE_FAILED_INTERNAL_ERROR", "Failed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not quote %q", err, want)
		}
	}
	if slices.Contains(adbCalls(t, log), "install -r /tmp/app.apk") {
		t.Error("installed over an app whose data survived, which is the reinstall reporting a clear it did not perform")
	}
}

func TestReinstallApp_UninstallFailureWithNothingInstalledIsQuiet(t *testing.T) {
	log := scriptedAdb(t, strings.Join([]string{
		uninstallFails,
		notInstalled,
		installSucceeds,
	}, "\n"))
	output := &strings.Builder{}

	if err := ReinstallApp(t.Context(), "", "app.example", "/tmp/app.apk", output); err != nil {
		t.Fatalf("ReinstallApp: %v", err)
	}

	calls := adbCalls(t, log)
	if !slices.Contains(calls, "install -r /tmp/app.apk") {
		t.Fatalf("adb calls = %v, want the install to go ahead: a first run has no app to uninstall", calls)
	}
	if slices.Contains(calls, "shell pm clear app.example") {
		t.Errorf("adb calls = %v, want no data clear: there was no app holding data", calls)
	}
	if output.String() != "" {
		t.Errorf("output = %q, want nothing: a first run has no app to uninstall", output.String())
	}
}

func TestReinstallApp_SuccessfulUninstallNeedsNoFallback(t *testing.T) {
	log := scriptedAdb(t, strings.Join([]string{
		`"uninstall "*) echo "Success";;`,
		stillInstalled,
		`"shell pm clear "*) echo "Success";;`,
		installSucceeds,
	}, "\n"))

	if err := ReinstallApp(t.Context(), "", "app.example", "/tmp/app.apk", &strings.Builder{}); err != nil {
		t.Fatalf("ReinstallApp: %v", err)
	}

	want := []string{"uninstall app.example", "install -r /tmp/app.apk"}
	if got := adbCalls(t, log); !slices.Equal(got, want) {
		t.Fatalf("adb calls = %v, want %v", got, want)
	}
}
