package verifier

import (
	"strings"
	"testing"
)

func TestPushSnapshot_CrossExtractorReadIsGuarded(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
const a = __sanderling__.extract(state => 1);
const b = __sanderling__.extract(() => a.current);
globalThis.a = a;
globalThis.b = b;
`)
	err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}})
	if err == nil {
		t.Fatal("expected guard error, got nil")
	}
	if !strings.Contains(err.Error(), "inside another extractor is not allowed") {
		t.Errorf("guard error wrong: %v", err)
	}
}

func TestExtract_NamedSetsExtractorName(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
const route = __sanderling__.extract(state => "home").named("route");
globalThis.route = route;
`)
	if got := verifier.extractors[0].name; got != "route" {
		t.Errorf("named() did not set name: got %q, want %q", got, "route")
	}
}
