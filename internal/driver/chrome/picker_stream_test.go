//go:build browser

package chrome

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/priyanshujain/sanderling/internal/bundler"
)

const streamSeed = 1

// A seed describes one stream of actions, and a web page can end it.
//
// The picker's draw position lived in the page, and the bundle is registered to
// run at every freshly-navigated document, so any navigation built a new picker
// at the seed's first draw. A page that submits a form, follows a link or
// reloads therefore replayed draw one forever: on the angular-dart TodoMVC
// implementation, whose form GET-submits on Enter, seed 1 chose PressKey enter
// on 200 of 200 steps and created no todo at all. The run reported clean, so
// nothing but this distinguishes it from a seed that chose badly.
func TestNextActionFromV8_ReloadDoesNotRestartTheSeedStream(t *testing.T) {
	url := testdataServer(t).URL + "/picker-stream.html"

	const calls = 8
	uninterrupted := pickerStream(t, url, calls, calls)
	if distinctActions(uninterrupted) < 2 {
		t.Fatalf("the uninterrupted stream never varies (%v); a restart would be invisible", uninterrupted)
	}
	reloaded := pickerStream(t, url, calls, calls/2)

	for index := range uninterrupted {
		if reloaded[index] != uninterrupted[index] {
			t.Fatalf("action %d after a reload is %s, uninterrupted the seed chose %s\nreloaded:      %v\nuninterrupted: %v",
				index, reloaded[index], uninterrupted[index], reloaded, uninterrupted)
		}
	}
}

// A navigation the trace cannot see is a run nobody can read: an analysis has
// no way to tell an app that reloaded from a generator that repeated itself.
func TestNavigations_ReportTheDocumentThatReplacedThePage(t *testing.T) {
	url := testdataServer(t).URL + "/picker-stream.html"

	d, ctx := launchChrome(t, url)

	opening, err := d.Navigations(ctx)
	if err != nil {
		t.Fatalf("Navigations: %v", err)
	}
	if len(opening) != 0 {
		t.Fatalf("the run's own opening navigation was reported as the app navigating: %v", opening)
	}

	if err := chromedp.Run(d.tabCtx, chromedp.Reload()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	reported, err := d.Navigations(ctx)
	if err != nil {
		t.Fatalf("Navigations: %v", err)
	}
	if len(reported) != 1 {
		t.Fatalf("a reload reported %d navigations, want 1: %v", len(reported), reported)
	}
	if reported[0].URL != url {
		t.Errorf("navigation URL = %q, want %q", reported[0].URL, url)
	}
	if reported[0].UnixMillis == 0 {
		t.Error("navigation carries no timestamp")
	}

	drained, err := d.Navigations(ctx)
	if err != nil {
		t.Fatalf("Navigations: %v", err)
	}
	if len(drained) != 0 {
		t.Errorf("navigations were reported twice: %v", drained)
	}
}

// pickerStream drives the real seeded picker over the page and returns the
// action it chose on each call, reloading the page once after reloadAfter
// calls. Reloading past the call count leaves the stream uninterrupted.
func pickerStream(t *testing.T, url string, calls, reloadAfter int) []string {
	t.Helper()
	d, ctx := launchChrome(t, url)
	installPickerStreamProbe(ctx, t, d)

	chosen := make([]string, 0, calls)
	for call := 0; call < calls; call++ {
		if call == reloadAfter {
			if err := chromedp.Run(d.tabCtx, chromedp.Reload()); err != nil {
				t.Fatalf("reload before call %d: %v", call, err)
			}
		}
		action, err := d.NextActionFromV8(ctx)
		if err != nil {
			t.Fatalf("NextActionFromV8 call %d: %v", call, err)
		}
		if len(action) == 0 {
			t.Fatalf("call %d chose nothing; the page has tappable targets", call)
		}
		chosen = append(chosen, string(action))
	}
	return chosen
}

func installPickerStreamProbe(ctx context.Context, t *testing.T, d *Driver) {
	t.Helper()
	specSource := filepath.Join(repoRootDir(t), "pkg", "spec")
	probe, err := bundler.BundleWeb(bundler.WebOptions{
		EntryFile:      filepath.Join(specSource, "test", "picker-stream-probe.ts"),
		WebRuntimeFile: filepath.Join(specSource, "src", "web-runtime.ts"),
		Defines:        map[string]string{"SANDERLING_SEED": strconv.Itoa(streamSeed)},
	})
	if err != nil {
		t.Fatalf("bundle picker stream probe: %v", err)
	}
	if err := d.InstallBundle(ctx, probe.JavaScript); err != nil {
		t.Fatalf("install picker stream probe: %v", err)
	}
}

func distinctActions(actions []string) int {
	seen := map[string]struct{}{}
	for _, action := range actions {
		seen[action] = struct{}{}
	}
	return len(seen)
}
