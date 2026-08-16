//go:build browser

package browser_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/driver"
	"github.com/priyanshujain/sanderling/internal/driver/chrome"
	"github.com/priyanshujain/sanderling/internal/hierarchy"
	"github.com/priyanshujain/sanderling/internal/verifier"
)

// TestBrowserConsoleErrorReachesTheSpec drives a page that calls console.error
// and follows the entry the whole way a run does: the driver's log fetch at the
// runner's minimum level, then into the verifier state a property reads. The
// default noLogcatErrors counts entries whose level is "E", so a driver that
// spells the level any other way leaves the property permanently satisfied on
// web with nothing reporting that it never saw anything.
func TestBrowserConsoleErrorReachesTheSpec(t *testing.T) {
	ctx, driverInstance, since := launchConsoleFixture(t)

	entries, err := driverInstance.RecentLogs(ctx, since, "E")
	if err != nil {
		t.Fatalf("recent logs: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("the runner's error-level fetch returned %d entries, want the page's two console.error calls: %+v", len(entries), entries)
	}
	for _, entry := range entries {
		if entry.Level != "E" {
			t.Errorf("console.error %q arrived as level %q, want %q", entry.Message, entry.Level, "E")
		}
	}
	// console.error(err) is the ordinary way a page reports a failure, and the
	// argument is then an object rather than a string. An entry that arrives
	// with the right level and no message names nothing a reader can act on.
	if !messageSeen(entries, "boom from the page") {
		t.Errorf("the page's console.error string never arrived: %+v", entries)
	}
	if !messageSeen(entries, "object arg detail") {
		t.Errorf("console.error(new Error(...)) arrived with no message: %+v", entries)
	}

	dump, err := driverInstance.Hierarchy(ctx)
	if err != nil {
		t.Fatalf("hierarchy: %v", err)
	}
	tree, err := hierarchy.Parse(dump)
	if err != nil {
		t.Fatalf("parse hierarchy: %v", err)
	}

	gojaBundle, _ := bundleSpec(t, filepath.Join(testdataDir(t), "console-levels", "spec.ts"))
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
	if err := verifierInstance.PushSnapshot(verifier.SnapshotInput{
		Tree: tree,
		Logs: asVerifierLogs(entries),
	}); err != nil {
		t.Fatalf("push snapshot: %v", err)
	}
	verifierInstance.EvaluateProperties()
	if violated := verifierInstance.NewlyViolatedProperties(); !slices.Contains(violated, "noLogcatErrors") {
		t.Fatalf("a console.error on the page left noLogcatErrors satisfied; violations=%v", violated)
	}
}

// TestBrowserConsoleErrorFiresTheLogProperty drives the same page through the
// whole bundle -> run -> verify pipeline instead of hand-assembling a snapshot.
// The driver holding the entry is not enough on web: every extractor reading is
// replaced by the one the page computed, so state.logs is whatever the page
// says it is, and a page that answers "no logs" leaves noLogcatErrors green on
// a run whose console was full of errors.
func TestBrowserConsoleErrorFiresTheLogProperty(t *testing.T) {
	violations := runFixture(t, "console-levels")
	if !slices.Contains(violations, "noLogcatErrors") {
		t.Fatalf("a page calling console.error ran a whole run without noLogcatErrors firing; violations=%v", violations)
	}
}

// TestBrowserQuietPageKeepsTheLogPropertySatisfied is the other half: a page
// whose console never reaches the error level must leave noLogcatErrors alone,
// so the property is reporting what the page logged rather than being on
// whenever the run is web.
func TestBrowserQuietPageKeepsTheLogPropertySatisfied(t *testing.T) {
	violations := runFixture(t, "console-quiet")
	if slices.Contains(violations, "noLogcatErrors") {
		t.Errorf("noLogcatErrors fired on a page that logged nothing at error level; violations=%v", violations)
	}
	if !slices.Contains(violations, "counterNeverMoves") {
		t.Fatalf("nothing was ever pressed, so the run proves nothing about a property that can fire; violations=%v", violations)
	}
}

// TestBrowserConsoleLevelsMapToTheLogcatScale pins what each console verb
// becomes once it crosses the driver. driver.LogEntry.Level is the single-letter
// logcat scale on every platform, so a spec asking for warnings or debug lines
// by letter has to get the same answer on web as it does on Android, and a
// console verb the driver has no mapping for still has to arrive rather than be
// silently discarded.
func TestBrowserConsoleLevelsMapToTheLogcatScale(t *testing.T) {
	ctx, driverInstance, since := launchConsoleFixture(t)

	entries, err := driverInstance.RecentLogs(ctx, since, "V")
	if err != nil {
		t.Fatalf("recent logs: %v", err)
	}
	if len(entries) != 7 {
		t.Fatalf("the page made 7 console calls, the driver kept %d: %+v", len(entries), entries)
	}

	wantLevels := map[string]string{
		"boom from the page": "E",
		"a warning":          "W",
		"a plain log":        "I",
		"a debug line":       "D",
		"an info line":       "I",
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if !slices.Contains([]string{"V", "D", "I", "W", "E", "F"}, entry.Level) {
			t.Errorf("entry %q carries level %q, which is not on the logcat scale a spec compares against", entry.Message, entry.Level)
		}
		for message, level := range wantLevels {
			if !strings.Contains(entry.Message, message) {
				continue
			}
			seen[message] = true
			if entry.Level != level {
				t.Errorf("console message %q arrived as level %q, want %q", message, entry.Level, level)
			}
		}
	}
	for message := range wantLevels {
		if !seen[message] {
			t.Errorf("console message %q never reached the driver's log buffer", message)
		}
	}

	errorsOnly, err := driverInstance.RecentLogs(ctx, since, "E")
	if err != nil {
		t.Fatalf("recent logs: %v", err)
	}
	if len(errorsOnly) != 2 {
		t.Fatalf("the error-level fetch kept %d of the 7 entries, want only the console.error calls: %+v", len(errorsOnly), errorsOnly)
	}
	warningsUp, err := driverInstance.RecentLogs(ctx, since, "W")
	if err != nil {
		t.Fatalf("recent logs: %v", err)
	}
	if len(warningsUp) != 3 {
		t.Fatalf("the warning-level fetch kept %d entries, want the console.error calls and the console.warn: %+v", len(warningsUp), warningsUp)
	}
}

// launchConsoleFixture serves the console-levels page and drives headless Chrome
// to it, returning the driver plus the instant before the page ran so a log
// fetch can ask for everything the page emitted.
func launchConsoleFixture(t *testing.T) (context.Context, *chrome.Driver, time.Time) {
	t.Helper()

	server := httptest.NewServer(http.FileServer(http.Dir(testdataDir(t))))
	t.Cleanup(server.Close)

	driverInstance := chrome.New()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = driverInstance.Terminate(ctx)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	since := time.Now()
	if err := driverInstance.Launch(ctx, server.URL+"/console-levels/", false, nil); err != nil {
		t.Fatalf("launch: %v", err)
	}
	return ctx, driverInstance, since
}

func messageSeen(entries []driver.LogEntry, want string) bool {
	return slices.ContainsFunc(entries, func(entry driver.LogEntry) bool {
		return strings.Contains(entry.Message, want)
	})
}

func asVerifierLogs(entries []driver.LogEntry) []verifier.LogEntry {
	out := make([]verifier.LogEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, verifier.LogEntry{
			UnixMillis: entry.UnixMillis,
			Level:      entry.Level,
			Tag:        entry.Tag,
			Message:    entry.Message,
		})
	}
	return out
}
