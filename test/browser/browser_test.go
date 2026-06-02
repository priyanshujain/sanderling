//go:build browser

// Package browser_test drives small self-contained web fixtures through the real
// bundle -> run -> verify pipeline against headless Chrome, proving the web path
// surfaces a known violation by property name and leaves a correct page clean.
//
// The suite is gated behind the `browser` build tag so the default
// `go test ./...` (which has no Chrome dependency) never runs it. Invoke it with
// `go test -tags browser ./test/browser/...` or `make test-browser`.
package browser_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/bundler"
	"github.com/priyanshujain/sanderling/internal/driver"
	"github.com/priyanshujain/sanderling/internal/driver/chrome"
	chromerunner "github.com/priyanshujain/sanderling/internal/runner"
	"github.com/priyanshujain/sanderling/internal/trace"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

const (
	fixtureSeed     = 1
	fixtureMaxSteps = 12
)

// TestBrowserUncaughtExceptionSurfaces drives a page whose button throws an
// uncaught Error on the third click and asserts the default noUncaughtExceptions
// property fires within the step budget.
func TestBrowserUncaughtExceptionSurfaces(t *testing.T) {
	violations := runFixture(t, "throwing")
	if !slices.Contains(violations, "noUncaughtExceptions") {
		t.Fatalf("noUncaughtExceptions did not fire; violations=%v", violations)
	}
}

// TestBrowserCounterInvariantHolds drives a correct counter page whose buttons
// move the value by exactly one and asserts the changesByAtMostOne property
// never fires.
func TestBrowserCounterInvariantHolds(t *testing.T) {
	violations := runFixture(t, "counter")
	if slices.Contains(violations, "changesByAtMostOne") {
		t.Fatalf("changesByAtMostOne fired on a correct page; violations=%v", violations)
	}
}

// runFixture serves the named testdata case over an in-process file server,
// drives it through headless Chrome with a fixed seed and a bounded step count,
// and returns every property name that was ever reported violated.
func runFixture(t *testing.T, name string) []string {
	t.Helper()

	server := httptest.NewServer(http.FileServer(http.Dir(testdataDir(t))))
	t.Cleanup(server.Close)
	url := server.URL + "/" + name + "/"

	specPath := filepath.Join(testdataDir(t), name, "spec.ts")
	gojaBundle, webBundle := bundleSpec(t, specPath)

	driverInstance := chrome.New()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = driverInstance.Terminate(ctx)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if err := driverInstance.Launch(ctx, url, false, nil); err != nil {
		t.Fatalf("launch %s: %v", url, err)
	}
	web, ok := any(driverInstance).(driver.WebDriver)
	if !ok {
		t.Fatal("chrome driver does not satisfy WebDriver")
	}
	if err := web.InstallBundle(ctx, webBundle); err != nil {
		t.Fatalf("install web bundle: %v", err)
	}

	verifierInstance, err := verifier.New(
		verifier.WithSeed(fixtureSeed),
		verifier.WithPlatform("web"),
	)
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	if err := verifierInstance.Load(string(gojaBundle)); err != nil {
		t.Fatalf("load spec: %v", err)
	}

	traceWriter, err := trace.NewWriter(t.TempDir())
	if err != nil {
		t.Fatalf("trace writer: %v", err)
	}
	t.Cleanup(func() { _ = traceWriter.Close() })

	summary, err := chromerunner.Run(ctx, chromerunner.Options{
		Duration:    90 * time.Second,
		IdleTimeout: time.Second,
		MaxSteps:    fixtureMaxSteps,
		Driver:      driverInstance,
		Verifier:    verifierInstance,
		TraceWriter: traceWriter,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	var violated []string
	for _, record := range summary.Violations {
		violated = append(violated, record.Properties...)
	}
	return violated
}

// bundleSpec compiles the fixture spec for both runtimes: the goja bundle the
// host verifier loads and the web bundle injected into the page. Aliases and
// runtime-entry paths are resolved relative to the repo's pkg/spec checkout.
func bundleSpec(t *testing.T, specPath string) (goja, web []byte) {
	t.Helper()
	specSrc := specSrcDir(t)
	aliases := map[string]string{
		"@sanderling/spec":                    filepath.Join(specSrc, "index.ts"),
		"@sanderling/spec/defaults":           filepath.Join(specSrc, "defaults/index.ts"),
		"@sanderling/spec/defaults/properties": filepath.Join(specSrc, "defaults/properties.ts"),
	}
	defines := map[string]string{"SANDERLING_SEED": strconv.Itoa(fixtureSeed)}

	gojaResult, err := bundler.Bundle(bundler.Options{
		EntryFile:   specPath,
		RuntimeFile: filepath.Join(specSrc, "goja-runtime.ts"),
		Defines:     defines,
		Aliases:     aliases,
	})
	if err != nil {
		t.Fatalf("bundle goja spec: %v", err)
	}
	webResult, err := bundler.BundleWeb(bundler.WebOptions{
		EntryFile:      specPath,
		WebRuntimeFile: filepath.Join(specSrc, "web-runtime.ts"),
		Defines:        defines,
		Aliases:        aliases,
	})
	if err != nil {
		t.Fatalf("bundle web spec: %v", err)
	}
	return gojaResult.JavaScript, webResult.JavaScript
}

// repoRoot walks up from this test file to the module root (the directory
// holding go.mod), so paths resolve regardless of the working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}

func testdataDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller path")
	}
	return filepath.Join(filepath.Dir(thisFile), "testdata")
}

func specSrcDir(t *testing.T) string {
	return filepath.Join(repoRoot(t), "pkg", "spec", "src")
}
