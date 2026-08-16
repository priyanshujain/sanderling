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
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

// TestBrowserShadowDOMIsReachable drives a page whose entire UI (canvas, the
// button over it, and the counter) lives inside a shadow root, the shape
// Compose for Web produces. Both the enumeration and the selector lookup have
// to cross the boundary for the counter to move at all, so the property firing
// is the end-to-end evidence.
func TestBrowserShadowDOMIsReachable(t *testing.T) {
	violations := runFixture(t, "shadow")
	if !slices.Contains(violations, "counterNeverMoves") {
		t.Fatalf("nothing inside the shadow root was ever tapped; violations=%v", violations)
	}
}

// TestBrowserAriaRoleIsTappable drives a page whose only controls are
// <li role="option"> rows, the shape the replay UI gives its step list. The
// tappable set covered role="button" and nothing else, so a spec had to
// hand-write an action to reach a row; with the standard interactive roles
// covered, the default tap enumeration finds them and the counter moves.
func TestBrowserAriaRoleIsTappable(t *testing.T) {
	violations := runFixture(t, "aria-roles")
	if !slices.Contains(violations, "noRowWasEverSelected") {
		t.Fatalf("no role=\"option\" row was ever tapped; violations=%v", violations)
	}
}

// runFixture serves the named testdata case over an in-process file server,
// drives it through headless Chrome with a fixed seed and a bounded step count,
// and returns every property name that was ever reported violated.
func runFixture(t *testing.T, name string) []string {
	t.Helper()
	violations, _ := runFixtureRecording(t, name)
	return violations
}

// runFixtureRecording is runFixture plus the run directory, for a test that
// asserts what the run left on disk rather than what it reported.
func runFixtureRecording(t *testing.T, name string) ([]string, string) {
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
	if err := traceWriter.Close(); err != nil {
		t.Fatalf("close trace: %v", err)
	}

	var violated []string
	for _, record := range summary.Violations {
		violated = append(violated, record.Properties...)
	}
	return violated, traceWriter.Directory()
}

// bundleSpec compiles the fixture spec for both runtimes: the goja bundle the
// host verifier loads and the web bundle injected into the page. Aliases and
// runtime-entry paths are resolved relative to the repo's pkg/spec checkout.
func bundleSpec(t *testing.T, specPath string) (goja, web []byte) {
	t.Helper()
	specSrc := specSrcDir(t)
	aliases := map[string]string{
		"@sanderling/spec":                     filepath.Join(specSrc, "index.ts"),
		"@sanderling/spec/defaults":            filepath.Join(specSrc, "defaults/index.ts"),
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

// TestBrowserUndefinedExtractorStaysUndefined drives the four layers of the
// undefined reading through one run: the page wraps each reading in a {value}
// envelope, the driver unwraps an absent value to an empty payload, the runner
// checks the page reported one reading per extractor, and the verifier decodes
// the empty payload as undefined. Each layer has its own unit test; only a run
// proves they compose. Written straight into the map instead, an undefined
// reading lost its whole index to JSON.stringify and that extractor silently
// kept goja's dump-derived value while its neighbours held the page's.
func TestBrowserUndefinedExtractorStaysUndefined(t *testing.T) {
	violations := runFixture(t, "undefined-extractor")
	if slices.Contains(violations, "undefinedStaysUndefined") {
		t.Error("a reading the page could not take did not reach the spec as undefined")
	}
	if !slices.Contains(violations, "counterNeverMoves") {
		t.Fatalf("nothing was ever tapped, so the property above held vacuously; violations=%v", violations)
	}
}

// TestBrowserUncaughtExceptionReachesTheTrace covers the error surface a web
// run has no other source for. The page buffers its uncaught errors in V8;
// nothing used to carry them out, so state.exceptions was empty on the host
// and no trace ever held one, leaving an offline crash oracle nothing to read.
func TestBrowserUncaughtExceptionReachesTheTrace(t *testing.T) {
	violations, directory := runFixtureRecording(t, "throwing")
	if !slices.Contains(violations, "noUncaughtExceptions") {
		t.Fatalf("the page never threw; violations=%v", violations)
	}

	file, err := os.Open(filepath.Join(directory, "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var recorded []trace.Exception
	for scanner.Scan() {
		var step trace.Step
		if err := json.Unmarshal(scanner.Bytes(), &step); err != nil {
			t.Fatalf("decode step: %v", err)
		}
		if step.TraceVersion != trace.TraceVersion {
			t.Errorf("step %d: trace_version = %d, want %d", step.Index, step.TraceVersion, trace.TraceVersion)
		}
		recorded = append(recorded, step.Exceptions...)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(recorded) == 0 {
		t.Fatal("the page threw and the verdict saw it, but no trace step carries an exception")
	}
	if recorded[0].Class == "" || recorded[0].Message == "" {
		t.Errorf("exception recorded without class or message: %+v", recorded[0])
	}
}
