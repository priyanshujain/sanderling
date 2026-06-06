//go:build !withcompanion

package companionassets

import (
	"strings"
	"testing"
)

func TestStubBuild_IsPlaceholder(t *testing.T) {
	if !IsPlaceholder() {
		t.Error("default build (no -tags withcompanion) must report a placeholder")
	}
	if EmbeddedSize() != 0 {
		t.Errorf("placeholder build must embed no archive, got %d bytes", EmbeddedSize())
	}
}

func TestStubBuild_ExtractErrors(t *testing.T) {
	_, err := Extract(t.TempDir())
	if err == nil {
		t.Fatal("Extract must fail when no archive is embedded")
	}
	if !strings.Contains(err.Error(), "withcompanion") {
		t.Errorf("error should tell the user to rebuild with -tags withcompanion, got %v", err)
	}
}
