package main

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
uatu_test
`

	got := parseAVDList(output)
	want := []string{"Medium_Phone_API_36.0", "uatu_test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPickAVD_ExplicitName(t *testing.T) {
	got, err := pickAVD("Pixel_7", []string{"Pixel_7", "uatu_test"})
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
	got, err := pickAVD("", []string{"uatu_test"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "uatu_test" {
		t.Fatalf("got %q, want uatu_test", got)
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
