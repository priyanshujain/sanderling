package android

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

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
