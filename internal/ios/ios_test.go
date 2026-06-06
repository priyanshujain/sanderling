package ios

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const simctlJSON = `{
  "devices": {
    "com.apple.CoreSimulator.SimRuntime.iOS-17-0": [
      {"udid": "ipad-udid", "state": "Shutdown", "name": "iPad Pro", "isAvailable": true},
      {"udid": "iphone15-udid", "state": "Booted", "name": "iPhone 15", "isAvailable": true},
      {"udid": "broken-udid", "state": "Shutdown", "name": "iPhone 14", "isAvailable": false}
    ],
    "com.apple.CoreSimulator.SimRuntime.iOS-16-4": [
      {"udid": "watch-udid", "state": "Shutdown", "name": "Apple Watch", "isAvailable": true}
    ]
  }
}`

func TestParseBootedDevice(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantUDID string
		wantNil  bool
	}{
		{"picks booted entry", simctlJSON, "iphone15-udid", false},
		{"none booted", `{"devices":{"r":[{"udid":"a","state":"Shutdown","name":"x","isAvailable":true}]}}`, "", true},
		{"empty list", `{"devices":{}}`, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseBootedDevice([]byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantNil {
				if got != nil {
					t.Fatalf("want nil, got %+v", got)
				}
				return
			}
			if got == nil || got.UDID != tc.wantUDID {
				t.Fatalf("got %+v, want UDID %q", got, tc.wantUDID)
			}
		})
	}
}

func TestParseBootedDevice_InvalidJSON(t *testing.T) {
	if _, err := parseBootedDevice([]byte("not json")); err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}

func TestParseAvailableDevices_DropsUnavailable(t *testing.T) {
	got, err := parseAvailableDevices([]byte(simctlJSON))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range got {
		if !d.IsAvailable {
			t.Fatalf("unavailable device leaked through: %+v", d)
		}
		if d.UDID == "broken-udid" {
			t.Fatalf("isAvailable=false device must not be selectable")
		}
	}
	if len(got) != 3 {
		t.Fatalf("got %d available across runtimes, want 3", len(got))
	}
}

func TestPickSimulator(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		available []simDevice
		wantUDID  string // "" with wantErr means error expected
		wantErr   bool
	}{
		{
			name:      "by name",
			query:     "iPhone 15",
			available: []simDevice{{UDID: "aaa", Name: "iPad Pro", IsAvailable: true}, {UDID: "bbb", Name: "iPhone 15", IsAvailable: true}},
			wantUDID:  "bbb",
		},
		{
			name:      "by udid",
			query:     "aaa",
			available: []simDevice{{UDID: "aaa", Name: "iPad Pro", IsAvailable: true}, {UDID: "bbb", Name: "iPhone 14", IsAvailable: true}},
			wantUDID:  "aaa",
		},
		{
			name:      "unknown name errors",
			query:     "Pixel 7",
			available: []simDevice{{UDID: "aaa", Name: "iPad Pro", IsAvailable: true}},
			wantErr:   true,
		},
		{
			name:      "empty query prefers iPhone",
			query:     "",
			available: []simDevice{{UDID: "aaa", Name: "iPad mini", IsAvailable: true}, {UDID: "bbb", Name: "iPhone 16", IsAvailable: true}, {UDID: "ccc", Name: "Apple Watch", IsAvailable: true}},
			wantUDID:  "bbb",
		},
		{
			name:      "empty query falls back to first",
			query:     "",
			available: []simDevice{{UDID: "aaa", Name: "iPad Air", IsAvailable: true}, {UDID: "bbb", Name: "Apple TV", IsAvailable: true}},
			wantUDID:  "aaa",
		},
		{
			name:    "empty list errors",
			query:   "",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pickSimulator(tc.query, tc.available)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for query %q", tc.query)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.UDID != tc.wantUDID {
				t.Errorf("got %q, want %q", got.UDID, tc.wantUDID)
			}
		})
	}
}

func TestBootedUDID_CanceledContextReturnsEmpty(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	udid := BootedUDID(ctx)
	if udid != "" {
		t.Errorf("expected empty UDID on canceled context, got %q", udid)
	}
}

func swapSeams(t *testing.T) {
	t.Helper()
	origBoot, origWait := boot, waitForBoot
	t.Cleanup(func() { boot, waitForBoot = origBoot, origWait })
}

func TestEnsureSimulator_UsesBootedSimulator(t *testing.T) {
	swapSeams(t)
	origListBooted, origListAvailable := listBooted, listAvailable
	t.Cleanup(func() { listBooted, listAvailable = origListBooted, origListAvailable })

	listBooted = func(context.Context) (*simDevice, error) {
		return &simDevice{UDID: "iphone15-udid", Name: "iPhone 15"}, nil
	}
	availableCalled := false
	listAvailable = func(context.Context) ([]simDevice, error) {
		availableCalled = true
		return nil, nil
	}
	boot = func(context.Context, string) error {
		t.Fatal("must not boot when a simulator is already booted")
		return nil
	}

	var out bytes.Buffer
	if err := EnsureSimulator(context.Background(), "", &out); err != nil {
		t.Fatal(err)
	}
	if availableCalled {
		t.Error("should short-circuit without listing available simulators")
	}
	if !bytes.Contains(out.Bytes(), []byte("iphone15-udid")) {
		t.Errorf("expected booted UDID in output, got %q", out.String())
	}
}

func TestEnsureSimulator_BootsPickedSimulator(t *testing.T) {
	swapSeams(t)
	origListBooted, origListAvailable := listBooted, listAvailable
	t.Cleanup(func() { listBooted, listAvailable = origListBooted, origListAvailable })

	listBooted = func(context.Context) (*simDevice, error) { return nil, nil }
	listAvailable = func(context.Context) ([]simDevice, error) {
		return []simDevice{
			{UDID: "ipad-udid", Name: "iPad Pro", IsAvailable: true},
			{UDID: "iphone15-udid", Name: "iPhone 15", IsAvailable: true},
		}, nil
	}
	var bootedUDID string
	boot = func(_ context.Context, udid string) error { bootedUDID = udid; return nil }
	waitForBoot = func(context.Context, string, time.Duration) error { return nil }

	if err := EnsureSimulator(context.Background(), "", new(bytes.Buffer)); err != nil {
		t.Fatal(err)
	}
	if bootedUDID != "iphone15-udid" {
		t.Errorf("booted %q, want iphone15-udid (iPhone preference)", bootedUDID)
	}
}

func TestEnsureSimulator_BootedListError(t *testing.T) {
	origListBooted := listBooted
	t.Cleanup(func() { listBooted = origListBooted })
	listBooted = func(context.Context) (*simDevice, error) { return nil, errors.New("xcrun blew up") }

	err := EnsureSimulator(context.Background(), "", new(bytes.Buffer))
	if err == nil {
		t.Fatal("expected error to propagate")
	}
	if !strings.Contains(err.Error(), "xcrun blew up") {
		t.Errorf("error should wrap cause, got %v", err)
	}
}
