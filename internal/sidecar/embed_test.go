//go:build !withsidecar

package sidecar

import "testing"

func TestStub_IsPlaceholder(t *testing.T) {
	if !IsPlaceholder() {
		t.Errorf("default build should be a placeholder")
	}
	if EmbeddedSize() != 0 {
		t.Errorf("stub should have empty embedded JAR, got size %d", EmbeddedSize())
	}
}

func TestStub_ExtractFails(t *testing.T) {
	if _, err := Extract(t.TempDir()); err == nil {
		t.Fatal("expected Extract to fail in stub mode")
	}
}
