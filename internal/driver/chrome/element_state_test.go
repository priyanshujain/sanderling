//go:build browser

package chrome

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/priyanshujain/sanderling/internal/bundler"
	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// One page, one checkbox, two readers of its state.
//
// docs/manual/spec-language.md lists `checked` on every element find returns.
// The goja host reads it off the hierarchy dump this driver builds; the V8 host
// reads it off the live DOM through elementHandle in
// pkg/spec/src/web-runtime.ts. A field one host does not expose is silent: the
// property reading it compares undefined and holds on every screen.
//
// The state is read before and after a real click, because HTML keeps checkbox
// state in the DOM property and not in the markup attribute: an implementation
// reading element.getAttribute("checked") reports the starting value forever and
// passes any test that only reads a freshly loaded page.
func TestElementState_ChecksTrackTheLiveDOM(t *testing.T) {
	server := httptest.NewServer(http.FileServer(http.Dir("testdata")))
	defer server.Close()

	d := New()
	defer d.Terminate(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := d.Launch(ctx, server.URL+"/element-state.html", false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	installStateProbe(ctx, t, d)

	requireChecked(ctx, t, d, "toggle-all", false)
	requireChecked(ctx, t, d, "toggle-done", true)

	clickElement(ctx, t, d, "id:toggle-all")
	clickElement(ctx, t, d, "id:toggle-done")

	requireChecked(ctx, t, d, "toggle-all", true)
	requireChecked(ctx, t, d, "toggle-done", false)
}

// requireChecked holds both hosts to one answer. The dump is re-read per call so
// the goja side is compared at the same page state as the handle.
func requireChecked(
	ctx context.Context,
	t *testing.T,
	d *Driver,
	id string,
	want bool,
) {
	t.Helper()
	state := elementStateFromWebRuntime(ctx, t, d, id)
	if state.Checked == nil {
		t.Fatalf(
			"the ax handle for %q exposes no `checked` field; "+
				"docs/manual/spec-language.md lists it on every element find returns",
			id,
		)
	}
	if *state.Checked != want {
		t.Errorf(
			"the ax handle reports %q checked=%v, want %v (its markup attribute reads %q)",
			id,
			*state.Checked,
			want,
			state.AttrChecked,
		)
	}
	if got := checkedInHierarchyDump(ctx, t, d, id); got != want {
		t.Errorf(
			"the hierarchy dump reports %q checked=%v, want %v",
			id,
			got,
			want,
		)
	}
}

// The rest of the boolean state the manual lists, on the same page.
//
// `selected` is read after the selection is moved off the markup's option, for
// the same reason `checked` is: the attribute records only where the page
// started.
func TestElementState_ReportsTheOtherDocumentedBooleans(t *testing.T) {
	server := httptest.NewServer(http.FileServer(http.Dir("testdata")))
	defer server.Close()

	d := New()
	defer d.Terminate(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := d.Launch(ctx, server.URL+"/element-state.html", false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	installStateProbe(ctx, t, d)

	requireBoolean(
		ctx,
		t,
		d,
		"save",
		"enabled",
		func(s elementState) *bool { return s.Enabled },
		true,
	)
	requireBoolean(
		ctx,
		t,
		d,
		"cancel",
		"enabled",
		func(s elementState) *bool { return s.Enabled },
		false,
	)

	requireBoolean(
		ctx,
		t,
		d,
		"filter-active",
		"selected",
		func(s elementState) *bool { return s.Selected },
		true,
	)
	requireBoolean(
		ctx,
		t,
		d,
		"filter-all",
		"selected",
		func(s elementState) *bool { return s.Selected },
		false,
	)
	if err := chromedp.Run(
		d.tabCtx,
		chromedp.Evaluate(`document.getElementById('filter').selectedIndex = 0`, nil),
	); err != nil {
		t.Fatalf("move the selection: %v", err)
	}
	requireBoolean(
		ctx,
		t,
		d,
		"filter-active",
		"selected",
		func(s elementState) *bool { return s.Selected },
		false,
	)
	requireBoolean(
		ctx,
		t,
		d,
		"filter-all",
		"selected",
		func(s elementState) *bool { return s.Selected },
		true,
	)

	clickElement(ctx, t, d, "id:editing")
	requireBoolean(
		ctx,
		t,
		d,
		"editing",
		"focused",
		func(s elementState) *bool { return s.Focused },
		true,
	)
	requireBoolean(
		ctx,
		t,
		d,
		"save",
		"focused",
		func(s elementState) *bool { return s.Focused },
		false,
	)
}

func requireBoolean(
	ctx context.Context,
	t *testing.T,
	d *Driver,
	id string,
	field string,
	read func(elementState) *bool,
	want bool,
) {
	t.Helper()
	got := read(elementStateFromWebRuntime(ctx, t, d, id))
	if got == nil {
		t.Fatalf(
			"the ax handle for %q exposes no `%s` field; "+
				"docs/manual/spec-language.md lists it on every element find returns",
			id, field,
		)
	}
	if *got != want {
		t.Errorf(
			"the ax handle reports %q %s=%v, want %v",
			id,
			field,
			*got,
			want,
		)
	}
}

// elementState is the boolean state one element reports, as pointers: a field
// the handle does not expose at all decodes as absent rather than as false.
type elementState struct {
	Checked     *bool  `json:"checked"`
	Enabled     *bool  `json:"enabled"`
	Focused     *bool  `json:"focused"`
	Selected    *bool  `json:"selected"`
	AttrChecked string `json:"attrChecked"`
}

func elementStateFromWebRuntime(
	ctx context.Context,
	t *testing.T,
	d *Driver,
	id string,
) elementState {
	t.Helper()
	var encoded string
	script := `JSON.stringify(window.__sanderlingElementState__(` + jsArgument(
		id,
	) + `))`
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(script, &encoded)); err != nil {
		t.Fatalf("read web runtime state for %q: %v", id, err)
	}
	if encoded == "null" {
		t.Fatalf("the web runtime resolved no element for id %q", id)
	}
	var state elementState
	if err := json.Unmarshal([]byte(encoded), &state); err != nil {
		t.Fatalf("decode web runtime state: %v", err)
	}
	return state
}

func checkedInHierarchyDump(
	ctx context.Context,
	t *testing.T,
	d *Driver,
	id string,
) bool {
	t.Helper()
	dump, err := d.Hierarchy(ctx)
	if err != nil {
		t.Fatalf("Hierarchy: %v", err)
	}
	tree, err := hierarchy.Parse(dump)
	if err != nil {
		t.Fatalf("parse hierarchy: %v", err)
	}
	element := tree.Find("id:" + id)
	if element == nil {
		t.Fatalf("the hierarchy dump holds no element with id %q", id)
	}
	return element.Checked
}

func clickElement(
	ctx context.Context,
	t *testing.T,
	d *Driver,
	selector string,
) {
	t.Helper()
	if err := d.TapSelector(ctx, selector); err != nil {
		t.Fatalf("TapSelector(%q): %v", selector, err)
	}
}

func installStateProbe(ctx context.Context, t *testing.T, d *Driver) {
	t.Helper()
	specSource := filepath.Join(repoRootDir(t), "pkg", "spec")
	probe, err := bundler.BundleWeb(bundler.WebOptions{
		EntryFile:      filepath.Join(specSource, "test", "dom-state-probe.ts"),
		WebRuntimeFile: filepath.Join(specSource, "src", "web-runtime.ts"),
	})
	if err != nil {
		t.Fatalf("bundle dom state probe: %v", err)
	}
	if err := d.InstallBundle(ctx, probe.JavaScript); err != nil {
		t.Fatalf("install dom state probe: %v", err)
	}
}

// Every key the manual offers, measured at the page.
//
// A key name the driver does not map presses nothing, and a spec clause written
// over it ("escape discards the edit in progress") can never fail: the run stays
// green having actuated nothing. The page records its own keydown events, so
// what is asserted here is what the DOM received, not what the driver sent.
func TestPressKey_ArrivesAtThePage(t *testing.T) {
	server := httptest.NewServer(http.FileServer(http.Dir("testdata")))
	defer server.Close()

	d := New()
	defer d.Terminate(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := d.Launch(ctx, server.URL+"/element-state.html", false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	for _, keyCase := range []struct{ key, want string }{
		{"enter", "Enter"},
		{"tab", "Tab"},
		{"escape", "Escape"},
		{"up", "ArrowUp"},
		{"down", "ArrowDown"},
		{"left", "ArrowLeft"},
		{"right", "ArrowRight"},
	} {
		t.Run(keyCase.key, func(t *testing.T) {
			forgetKeys(ctx, t, d)
			if err := d.PressKey(ctx, keyCase.key); err != nil {
				t.Fatalf("PressKey(%q): %v", keyCase.key, err)
			}
			got := keysSeenByThePage(ctx, t, d)
			if !slices.Equal(got, []string{keyCase.want}) {
				t.Errorf("PressKey(%q) reached the page as %v, want [%s]",
					keyCase.key, got, keyCase.want)
			}
		})
	}

	// back and home have no browser meaning, and reporting that is the whole
	// point: a key that quietly presses nothing is indistinguishable from a
	// requirement that holds.
	for _, key := range []string{"back", "home"} {
		t.Run(key+" is reported unsupported", func(t *testing.T) {
			forgetKeys(ctx, t, d)
			if err := d.PressKey(ctx, key); err == nil {
				t.Errorf("PressKey(%q) reported no error on web", key)
			}
			if got := keysSeenByThePage(ctx, t, d); len(got) != 0 {
				t.Errorf("PressKey(%q) reached the page as %v", key, got)
			}
		})
	}
}

func forgetKeys(ctx context.Context, t *testing.T, d *Driver) {
	t.Helper()
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(`window.__keys__ = []`, nil)); err != nil {
		t.Fatalf("reset recorded keys: %v", err)
	}
}

func keysSeenByThePage(ctx context.Context, t *testing.T, d *Driver) []string {
	t.Helper()
	var encoded string
	script := `JSON.stringify(window.__keys__)`
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(script, &encoded)); err != nil {
		t.Fatalf("read recorded keys: %v", err)
	}
	var keys []string
	if err := json.Unmarshal([]byte(encoded), &keys); err != nil {
		t.Fatalf("decode recorded keys: %v", err)
	}
	return keys
}

// The dump declares clickable, enabled, checked, selected and editable as
// flags, and a component keeps whatever it likes in the properties two of them
// are read from: a selector element names the selected item, not a boolean.
// Emitting the property raw cost the whole observation, because a dump is
// decoded as one document and one string in it fails all of it.
// pkg/spec/src/web-runtime.ts already answers `state.selected === true`, so the
// two hosts also disagreed about the same fact on the same page.
func TestElementState_AComponentPropertyDoesNotBlankTheTree(t *testing.T) {
	server := httptest.NewServer(http.FileServer(http.Dir("testdata")))
	defer server.Close()

	d := New()
	defer d.Terminate(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := d.Launch(ctx, server.URL+"/custom-element-flags.html", false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	dump, err := d.Hierarchy(ctx)
	if err != nil {
		t.Fatalf("Hierarchy: %v", err)
	}
	tree, err := hierarchy.Parse(dump)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if tree.UnreadableFlags != 0 {
		t.Errorf("UnreadableFlags = %d, want 0: the dump must send booleans for the fields it declares as flags", tree.UnreadableFlags)
	}
	tabs := tree.Find("id:tabs")
	if tabs == nil {
		t.Fatalf("the element holding the string property is missing from a tree of %d elements", len(tree.Elements))
	}
	if tabs.Selected {
		t.Error("selected must be false: the property holds an item name, not a flag")
	}
	if picker := tree.Find("id:picker"); picker == nil || picker.Checked {
		t.Errorf("checked must be false for a property holding a string, got %+v", picker)
	}
	if toggle := tree.Find("id:toggle"); toggle == nil || !toggle.Checked {
		t.Errorf("a real checkbox must still read checked, got %+v", toggle)
	}
}
