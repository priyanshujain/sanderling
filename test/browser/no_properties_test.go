//go:build browser

package browser_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/testrun"
)

// TestBrowserZeroPropertySpecFailsTheRun drives the whole pipeline against a
// spec that bundles and loads cleanly and registers nothing to judge with. Such
// a run reaches the verifier on every step, evaluates an empty property set and
// reports no violations: a confident green from an instrument measuring nothing,
// which is the one outcome a fuzzer must never hand back.
func TestBrowserZeroPropertySpecFailsTheRun(t *testing.T) {
	err := executeFixture(t, "no-properties", false)

	var noProperties testrun.NoPropertiesError
	if !errors.As(err, &noProperties) {
		t.Fatalf("a spec registering no properties came back %v, want a NoPropertiesError", err)
	}
	for _, phrase := range []string{"loaded into the verifier cleanly", "registers no properties", "--allow-no-properties"} {
		if !strings.Contains(noProperties.Error(), phrase) {
			t.Errorf("the error never says %q, so it reads as a broken spec: %v", phrase, noProperties)
		}
	}
}

// TestBrowserZeroPropertySpecRunsUnderTheOptOut covers the extraction and
// portability sweeps, which run a property-free spec on purpose to measure what
// it can read rather than to judge an app. They ask for it by name and the run
// proceeds.
func TestBrowserZeroPropertySpecRunsUnderTheOptOut(t *testing.T) {
	if err := executeFixture(t, "no-properties", true); err != nil {
		t.Fatalf("the opt-out did not carry a property-free run through: %v", err)
	}
}

// TestBrowserSpecWithPropertiesRunsUnaffected pins the guard to the empty case
// alone: a spec that registers one property runs to its budget as before.
func TestBrowserSpecWithPropertiesRunsUnaffected(t *testing.T) {
	if err := executeFixture(t, "counter", false); err != nil {
		t.Fatalf("a spec with a property no longer runs: %v", err)
	}
}

// executeFixture serves the named testdata case and drives it through
// testrun.Execute, the pipeline `sanderling test` itself runs.
func executeFixture(t *testing.T, name string, allowNoProperties bool) error {
	t.Helper()

	server := httptest.NewServer(http.FileServer(http.Dir(testdataDir(t))))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)

	return testrun.Execute(ctx, testrun.Options{
		Spec:              filepath.Join(testdataDir(t), name, "spec.ts"),
		BundleID:          server.URL + "/" + name + "/",
		Platform:          "web",
		Seed:              fixtureSeed,
		MaxSteps:          fixtureMaxSteps,
		Duration:          90 * time.Second,
		Output:            t.TempDir(),
		AllowNoProperties: allowNoProperties,
	}, io.Discard)
}
