package ios

import (
	"context"
	"testing"
)

func TestPickSimulator_ByName(t *testing.T) {
	available := []simDevice{
		{UDID: "aaa", Name: "iPad Pro", IsAvailable: true},
		{UDID: "bbb", Name: "iPhone 15", IsAvailable: true},
	}
	got, err := pickSimulator("iPhone 15", available)
	if err != nil {
		t.Fatal(err)
	}
	if got.UDID != "bbb" {
		t.Errorf("got %q, want bbb", got.UDID)
	}
}

func TestPickSimulator_ByUDID(t *testing.T) {
	available := []simDevice{
		{UDID: "aaa", Name: "iPad Pro", IsAvailable: true},
		{UDID: "bbb", Name: "iPhone 14", IsAvailable: true},
	}
	got, err := pickSimulator("aaa", available)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "iPad Pro" {
		t.Errorf("got %q, want iPad Pro", got.Name)
	}
}

func TestPickSimulator_UnknownName(t *testing.T) {
	available := []simDevice{
		{UDID: "aaa", Name: "iPad Pro", IsAvailable: true},
	}
	_, err := pickSimulator("Pixel 7", available)
	if err == nil {
		t.Fatal("expected error for unknown simulator name")
	}
}

func TestPickSimulator_EmptyName_PrefersIPhone(t *testing.T) {
	available := []simDevice{
		{UDID: "aaa", Name: "iPad mini", IsAvailable: true},
		{UDID: "bbb", Name: "iPhone 16", IsAvailable: true},
		{UDID: "ccc", Name: "Apple Watch", IsAvailable: true},
	}
	got, err := pickSimulator("", available)
	if err != nil {
		t.Fatal(err)
	}
	if got.UDID != "bbb" {
		t.Errorf("got %q, want bbb (iPhone)", got.UDID)
	}
}

func TestPickSimulator_EmptyName_FallsBackToFirst(t *testing.T) {
	available := []simDevice{
		{UDID: "aaa", Name: "iPad Air", IsAvailable: true},
		{UDID: "bbb", Name: "Apple TV", IsAvailable: true},
	}
	got, err := pickSimulator("", available)
	if err != nil {
		t.Fatal(err)
	}
	if got.UDID != "aaa" {
		t.Errorf("got %q, want aaa (first available)", got.UDID)
	}
}

func TestPickSimulator_EmptyList(t *testing.T) {
	_, err := pickSimulator("", nil)
	if err == nil {
		t.Fatal("expected error for empty simulator list")
	}
}

func TestBootedUDID_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	udid := BootedUDID(ctx)
	if udid != "" {
		t.Errorf("expected empty UDID on canceled context, got %q", udid)
	}
}
