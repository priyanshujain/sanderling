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

func swapResolveSeams(t *testing.T) {
	t.Helper()
	origBootedAll, origAvailable := listBootedAll, listAvailable
	t.Cleanup(func() { listBootedAll, listAvailable = origBootedAll, origAvailable })
}

func TestResolveTarget_ExplicitMatchesBootedSimulator(t *testing.T) {
	swapResolveSeams(t)
	listBootedAll = func(context.Context) ([]simDevice, error) {
		return []simDevice{{UDID: "booted-udid", Name: "iPhone 15", State: "Booted", IsAvailable: true}}, nil
	}
	listAvailable = func(context.Context) ([]simDevice, error) {
		t.Fatal("should not list available when a booted simulator matches")
		return nil, nil
	}
	udid, isSimulator, err := ResolveTarget(context.Background(), "iPhone 15")
	if err != nil {
		t.Fatal(err)
	}
	if udid != "booted-udid" || !isSimulator {
		t.Errorf("got (%q, %v), want (booted-udid, true)", udid, isSimulator)
	}
}

func TestResolveTarget_ExplicitMatchesAvailableByUDID(t *testing.T) {
	swapResolveSeams(t)
	listBootedAll = func(context.Context) ([]simDevice, error) { return nil, nil }
	listAvailable = func(context.Context) ([]simDevice, error) {
		return []simDevice{{UDID: "shutdown-udid", Name: "iPhone 16", IsAvailable: true}}, nil
	}
	udid, isSimulator, err := ResolveTarget(context.Background(), "shutdown-udid")
	if err != nil {
		t.Fatal(err)
	}
	if udid != "shutdown-udid" || !isSimulator {
		t.Errorf("got (%q, %v), want (shutdown-udid, true)", udid, isSimulator)
	}
}

func TestResolveTarget_UnknownQueryResolvesAsPhysicalDevice(t *testing.T) {
	swapResolveSeams(t)
	listBootedAll = func(context.Context) ([]simDevice, error) { return nil, nil }
	listAvailable = func(context.Context) ([]simDevice, error) {
		return []simDevice{{UDID: "sim-udid", Name: "iPhone 16", IsAvailable: true}}, nil
	}
	udid, isSimulator, err := ResolveTarget(context.Background(), "00008110-physical-device-udid")
	if err != nil {
		t.Fatal(err)
	}
	if udid != "00008110-physical-device-udid" || isSimulator {
		t.Errorf("got (%q, %v), want (00008110-physical-device-udid, false)", udid, isSimulator)
	}
}

func TestResolveTarget_NoQuerySingleBooted(t *testing.T) {
	swapResolveSeams(t)
	listBootedAll = func(context.Context) ([]simDevice, error) {
		return []simDevice{{UDID: "only-booted", Name: "iPhone 15", State: "Booted", IsAvailable: true}}, nil
	}
	udid, isSimulator, err := ResolveTarget(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if udid != "only-booted" || !isSimulator {
		t.Errorf("got (%q, %v), want (only-booted, true)", udid, isSimulator)
	}
}

func TestResolveTarget_NoQueryZeroBootedErrors(t *testing.T) {
	swapResolveSeams(t)
	listBootedAll = func(context.Context) ([]simDevice, error) { return nil, nil }
	_, _, err := ResolveTarget(context.Background(), "")
	if err == nil {
		t.Fatal("expected error when no simulator is booted")
	}
}

func TestResolveTarget_NoQueryMultipleBootedErrors(t *testing.T) {
	swapResolveSeams(t)
	listBootedAll = func(context.Context) ([]simDevice, error) {
		return []simDevice{
			{UDID: "udid-a", Name: "iPhone 15", State: "Booted", IsAvailable: true},
			{UDID: "udid-b", Name: "iPad Pro", State: "Booted", IsAvailable: true},
		}, nil
	}
	_, _, err := ResolveTarget(context.Background(), "")
	if err == nil {
		t.Fatal("expected ambiguity error for multiple booted simulators")
	}
	message := err.Error()
	for _, want := range []string{"--ios-device", "iPhone 15", "udid-a", "iPad Pro", "udid-b"} {
		if !strings.Contains(message, want) {
			t.Errorf("ambiguity error missing %q: %v", want, message)
		}
	}
}

func TestResolveTarget_BootedListError(t *testing.T) {
	swapResolveSeams(t)
	listBootedAll = func(context.Context) ([]simDevice, error) { return nil, errors.New("xcrun blew up") }
	if _, _, err := ResolveTarget(context.Background(), ""); err == nil {
		t.Fatal("expected booted-list error to propagate")
	}
}

func TestParseBootedDevices_CollectsAllBooted(t *testing.T) {
	got, err := parseBootedDevices([]byte(`{"devices":{"r":[
		{"udid":"a","state":"Booted","name":"iPhone 15","isAvailable":true},
		{"udid":"b","state":"Shutdown","name":"iPad","isAvailable":true},
		{"udid":"c","state":"Booted","name":"iPhone 16","isAvailable":true}
	]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d booted, want 2", len(got))
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
