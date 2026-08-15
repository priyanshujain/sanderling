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
	"github.com/priyanshujain/sanderling/internal/hierarchy"
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

// One page, one selector, two hosts, two answers.
//
// The V8 host resolves state.ax.find against the live DOM; the goja host
// resolves the same selector against the hierarchy dump. The fixture puts a
// shadow-hosted #x above a light-DOM #x, the one shape where the two walks can
// disagree, and on web it is V8's answer that reaches the properties. Each host
// is driven through its production path (EvaluateExtractors in the page,
// PushSnapshot over the dump) and neither is asked what the other said, so the
// comparison is evidence rather than an assertion about one of them.
func TestBrowserAxFindAgreesAcrossHosts(t *testing.T) {
	server := httptest.NewServer(http.FileServer(http.Dir(testdataDir(t))))
	t.Cleanup(server.Close)

	gojaBundle, webBundle := bundleSpec(t, filepath.Join(testdataDir(t), "find-order", "spec.ts"))

	driverInstance := chrome.New()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = driverInstance.Terminate(ctx)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := driverInstance.Launch(ctx, server.URL+"/find-order/", false, nil); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if err := driverInstance.InstallBundle(ctx, webBundle); err != nil {
		t.Fatalf("install web bundle: %v", err)
	}
	readings, err := driverInstance.EvaluateExtractors(ctx)
	if err != nil {
		t.Fatalf("evaluate extractors in the page: %v", err)
	}
	fromV8 := string(readings[0])

	dump, err := driverInstance.Hierarchy(ctx)
	if err != nil {
		t.Fatalf("hierarchy: %v", err)
	}
	tree, err := hierarchy.Parse(dump)
	if err != nil {
		t.Fatalf("parse hierarchy: %v", err)
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
	if err := verifierInstance.PushSnapshot(verifier.SnapshotInput{Tree: tree}); err != nil {
		t.Fatalf("push snapshot: %v", err)
	}
	change, ok := verifierInstance.ChangedExtractors()["found"]
	if !ok {
		t.Fatal("the goja host recorded no reading for the found extractor")
	}
	fromGoja := string(change.Curr)

	// Pinned, not just compared: two hosts that both resolved nothing would
	// agree on undefined and prove nothing about the walk.
	if fromGoja != `"shadow"` {
		t.Errorf("the goja host read %s off the dump, want the shadow-hosted %q", fromGoja, "shadow")
	}
	if fromV8 != fromGoja {
		t.Fatalf(
			"one page, one selector, two answers: the V8 host read %s and the goja host read %s",
			fromV8,
			fromGoja,
		)
	}
}
