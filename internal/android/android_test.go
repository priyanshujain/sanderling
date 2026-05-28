package android

import (
	"reflect"
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
