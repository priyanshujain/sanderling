package testrun

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
)

func TestBuildDriverRejectsPhysicalIOS(t *testing.T) {
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("xcrun not on PATH; physical-iOS rejection is reached only after preflight")
	}
	options := Options{Platform: "ios"}
	options.iosIsSimulator = false

	_, cleanup, err := buildDriver(context.Background(), options, io.Discard)
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Fatal("expected physical-device iOS to be rejected")
	}
	if !strings.Contains(err.Error(), "not yet supported") {
		t.Fatalf("expected an unsupported-physical-iOS error, got %v", err)
	}
}
