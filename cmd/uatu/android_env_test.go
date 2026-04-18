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
