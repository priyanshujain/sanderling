//go:build browser

package chrome

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/priyanshujain/sanderling/internal/bundler"
	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// One page, two fact producers.
//
// Sanderling enumerates candidate actions from two independent hosts. The goja
// host reads the hierarchy dump this driver builds; the V8 host reads the live
// DOM through collectTargets in pkg/spec/src/web-runtime.ts. Both feed the SAME
// eligibility rule (pkg/spec/src/targets.ts), so the two enumerations agree only
// as far as the two producers agree on the facts.
//
// internal/verifier/host_parity_test.go and pkg/spec/test/host-parity.test.ts
// pin the other half: given identical facts, both hosts select identical
// candidates. They hand-author those facts on both sides, so neither producer is
// on their path, and three producer bugs lived in that gap: the dump emitted no
// `scrollable` attribute at all, it derived `clickable` from el.onclick (which
// React assigns to its root container, making the whole viewport a tap target
// here and nowhere else), and it rooted at document.body while the web runtime
// walks the whole document, hiding `html` and with it every page-level scroll.
//
// This test starts from one real DOM in a real browser and compares what the two
// producers derive from it, element by element. It found a fourth: the dump left
// `editable` unset rather than false, and an unset field sends the parser into
// the native EditText class-name heuristic, which reads a CSS class as an
// Android text widget.

// elementFacts is one element as a producer reports it: the tag, for readable
// failures, and every fact acceptsTarget consults.
type elementFacts struct {
	tag        string
	clickable  bool
	enabled    bool
	editable   bool
	scrollable bool
	hintText   string
	// handleClickable and handleEditable are the same facts on the ax element a
	// spec reaches through state.ax.find, a third place they are computed and the
	// one that has twice been the odd one out: clickable answered a hardcoded
	// true while the other two resolved a selector, and editable read the
	// INHERITED isContentEditable, which makes every span inside a contenteditable
	// container typeable.
	handleClickable bool
	handleEditable  bool
	positiveBounds  bool
}

// factRow pairs an element's facts with the id both producers key on, kept in
// enumeration order because the order is part of the picker's parity contract.
type factRow struct {
	id    string
	facts elementFacts
}

// parityPages are the pages both producers are compared on. Each one must
// exercise every fact both ways on its own (requireBothPolarities), so adding a
// page never weakens the comparison. fact-parity-shadow.html is shaped like a
// Compose for Web app: the whole UI lives inside a shadow root, which both
// producers have to descend into or they enumerate one node for an entire app.
var parityPages = []string{"fact-parity.html", "fact-parity-shadow.html"}

func TestHierarchy_DerivesTheSameFactsAsTheWebRuntime(t *testing.T) {
	server := httptest.NewServer(http.FileServer(http.Dir("testdata")))
	defer server.Close()

	for _, page := range parityPages {
		t.Run(page, func(t *testing.T) {
			d := New()
			defer d.Terminate(context.Background())
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if err := d.Launch(ctx, server.URL+"/"+page, false, nil); err != nil {
				t.Fatalf("Launch: %v", err)
			}

			dump, err := d.Hierarchy(ctx)
			if err != nil {
				t.Fatalf("Hierarchy: %v", err)
			}
			fromDump := factsFromHierarchyDump(t, dump)
			fromWebRuntime := factsFromWebRuntime(ctx, t, d)

			requireEveryElementNamed(t, "the hierarchy dump", fromDump)
			requireEveryElementNamed(t, "the web runtime", fromWebRuntime)
			requireBothPolarities(t, fromWebRuntime)
			requireTheHandleAgreesWithTheEnumeration(t, fromWebRuntime)
			compareEnumeratedElements(t, fromDump, fromWebRuntime)
			compareDerivedFacts(t, fromDump, fromWebRuntime)
		})
	}
}

// factsFromHierarchyDump reads the dump the way the goja host does: parse it,
// then take exactly the fields internal/verifier/worker.go targets() reads off
// each element.
func factsFromHierarchyDump(t *testing.T, dump string) []factRow {
	t.Helper()
	tree, err := hierarchy.Parse(dump)
	if err != nil {
		t.Fatalf("parse hierarchy: %v", err)
	}
	rows := make([]factRow, 0, len(tree.Elements))
	for _, element := range tree.Elements {
		rows = append(rows, factRow{
			id: element.ResourceID,
			facts: elementFacts{
				tag:        element.Attributes["tag"],
				clickable:  element.Clickable,
				enabled:    element.Enabled,
				editable:   element.Editable,
				scrollable: element.Attributes["scrollable"] == "true",
				hintText:   element.Attributes["hintText"],
				positiveBounds: hasPositiveBounds(
					element.Bounds.Width(),
					element.Bounds.Height(),
				),
			},
		})
	}
	return rows
}

// factsFromWebRuntime installs pkg/spec/test/dom-facts-probe.ts into the page
// and calls it. The probe is bundled through the production web bundler and
// reports what the production collectTargets derives, so the facts come from the
// shipped runtime rather than a copy of it; only the id-carrying readback is
// test-only.
func factsFromWebRuntime(
	ctx context.Context,
	t *testing.T,
	d *Driver,
) []factRow {
	t.Helper()
	specSource := filepath.Join(repoRootDir(t), "pkg", "spec")
	probe, err := bundler.BundleWeb(bundler.WebOptions{
		EntryFile:      filepath.Join(specSource, "test", "dom-facts-probe.ts"),
		WebRuntimeFile: filepath.Join(specSource, "src", "web-runtime.ts"),
	})
	if err != nil {
		t.Fatalf("bundle dom facts probe: %v", err)
	}
	if err := d.InstallBundle(ctx, probe.JavaScript); err != nil {
		t.Fatalf("install dom facts probe: %v", err)
	}

	var encoded string
	script := `JSON.stringify(window.__sanderlingDomFacts__())`
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(script, &encoded)); err != nil {
		t.Fatalf("read web runtime facts: %v", err)
	}
	var wire []struct {
		ID              string `json:"id"`
		Tag             string `json:"tag"`
		Clickable       bool   `json:"clickable"`
		Enabled         bool   `json:"enabled"`
		Editable        bool   `json:"editable"`
		Scrollable      bool   `json:"scrollable"`
		HintText        string `json:"hintText"`
		HandleClickable bool   `json:"handleClickable"`
		HandleEditable  bool   `json:"handleEditable"`
		Width           int    `json:"width"`
		Height          int    `json:"height"`
	}
	if err := json.Unmarshal([]byte(encoded), &wire); err != nil {
		t.Fatalf("decode web runtime facts: %v", err)
	}
	rows := make([]factRow, 0, len(wire))
	for _, item := range wire {
		rows = append(rows, factRow{
			id: item.ID,
			facts: elementFacts{
				tag:             item.Tag,
				clickable:       item.Clickable,
				enabled:         item.Enabled,
				editable:        item.Editable,
				scrollable:      item.Scrollable,
				hintText:        item.HintText,
				handleClickable: item.HandleClickable,
				handleEditable:  item.HandleEditable,
				positiveBounds:  hasPositiveBounds(item.Width, item.Height),
			},
		})
	}
	return rows
}

// hasPositiveBounds is the positiveBounds fact of pkg/spec/src/targets.ts,
// applied to both producers so the geometry comparison cannot drift from the
// rule it stands in for.
func hasPositiveBounds(width, height int) bool {
	return width > 0 && height > 0
}

// requireEveryElementNamed fails when a producer enumerates an element the
// fixture gave no id, because such an element cannot be joined and would sit in
// the comparison invisibly.
func requireEveryElementNamed(t *testing.T, producer string, rows []factRow) {
	t.Helper()
	for index, row := range rows {
		if row.id == "" {
			t.Fatalf(
				"%s enumerated an unnamed <%s> at position %d; every element in "+
					"testdata/fact-parity.html must carry an id to be comparable",
				producer,
				row.facts.tag,
				index,
			)
		}
	}
}

// requireBothPolarities keeps the comparison from going vacuous. A fixture, or
// an emulated viewport, that leaves a fact constant would compare false against
// false on every element and pass while proving nothing about that fact.
func requireBothPolarities(t *testing.T, rows []factRow) {
	t.Helper()
	for _, fact := range []struct {
		name string
		read func(elementFacts) bool
	}{
		{"clickable", func(f elementFacts) bool { return f.clickable }},
		{"enabled", func(f elementFacts) bool { return f.enabled }},
		{"editable", func(f elementFacts) bool { return f.editable }},
		{"scrollable", func(f elementFacts) bool { return f.scrollable }},
		{"hintText", func(f elementFacts) bool { return f.hintText != "" }},
		{"positiveBounds", func(f elementFacts) bool { return f.positiveBounds }},
	} {
		var sawTrue, sawFalse bool
		for _, row := range rows {
			if fact.read(row.facts) {
				sawTrue = true
			} else {
				sawFalse = true
			}
		}
		if !sawTrue || !sawFalse {
			t.Errorf(
				"testdata/fact-parity.html no longer exercises %s both ways (the web "+
					"runtime saw true=%v, false=%v), so comparing it proves nothing",
				fact.name,
				sawTrue,
				sawFalse,
			)
		}
	}
}

// requireTheHandleAgreesWithTheEnumeration compares the V8 host against itself.
// An element a spec reaches through state.ax and the same element in the
// enumeration must be clickable, and typeable, to the same degree, or a spec
// taps a container the picker calls inert and types into a box the picker calls
// read-only. The handle resolves each fact by element.matches over a selector
// while the enumeration resolves it by membership of the set that selector
// queried, and this is where the two answers are held together over a real page:
// an [onclick] attribute, an onclick property that is not one, a <span> whose
// contenteditable is inherited from its container, elements inside a shadow root.
func requireTheHandleAgreesWithTheEnumeration(t *testing.T, rows []factRow) {
	t.Helper()
	for _, row := range rows {
		for _, fact := range []struct {
			name        string
			handle      bool
			enumeration bool
		}{
			{"clickable", row.facts.handleClickable, row.facts.clickable},
			{"editable", row.facts.handleEditable, row.facts.editable},
		} {
			if fact.handle != fact.enumeration {
				t.Errorf(
					"%q (<%s>): the ax handle reports %s=%v, the enumeration reports "+
						"%s=%v",
					row.id,
					row.facts.tag,
					fact.name,
					fact.handle,
					fact.name,
					fact.enumeration,
				)
			}
		}
	}
}

// compareEnumeratedElements is the check that the two producers walk the same
// document. It is what notices a producer that roots at body and never sees
// `html`, or one that enumerates the head subtree the other drops.
func compareEnumeratedElements(
	t *testing.T,
	fromDump, fromWebRuntime []factRow,
) {
	t.Helper()
	dumpIDs := enumeratedIDs(fromDump)
	webIDs := enumeratedIDs(fromWebRuntime)
	for _, row := range fromWebRuntime {
		if !slices.Contains(dumpIDs, row.id) {
			t.Errorf(
				"the hierarchy dump never enumerated %q (<%s>); the web runtime does, "+
					"so the goja host cannot offer a single action on it",
				row.id,
				row.facts.tag,
			)
		}
	}
	for _, row := range fromDump {
		if !slices.Contains(webIDs, row.id) {
			t.Errorf(
				"the web runtime never enumerated %q (<%s>); the hierarchy dump does, "+
					"so the V8 host cannot offer a single action on it",
				row.id,
				row.facts.tag,
			)
		}
	}
	if len(dumpIDs) == len(webIDs) && !slices.Equal(dumpIDs, webIDs) {
		t.Errorf(
			"the two producers enumerate the same elements in different order\n"+
				" dump=%v\n  web=%v",
			dumpIDs,
			webIDs,
		)
	}
}

// compareDerivedFacts compares the facts fact by fact, over every element both
// producers saw, so a failure names the element and the fact that diverged.
func compareDerivedFacts(t *testing.T, fromDump, fromWebRuntime []factRow) {
	t.Helper()
	webByID := map[string]elementFacts{}
	for _, row := range fromWebRuntime {
		webByID[row.id] = row.facts
	}
	for _, row := range fromDump {
		web, ok := webByID[row.id]
		if !ok {
			continue
		}
		dump := row.facts
		if dump.tag != web.tag {
			t.Errorf(
				"%q: the hierarchy dump calls it <%s>, the web runtime <%s>; the id "+
					"join is resolving to different elements",
				row.id,
				dump.tag,
				web.tag,
			)
		}
		if dump.hintText != web.hintText {
			t.Errorf(
				"%q (<%s>): the hierarchy dump names the field %q, the web runtime "+
					"names it %q; the model is shown a different control on each host",
				row.id,
				dump.tag,
				dump.hintText,
				web.hintText,
			)
		}
		for _, fact := range []struct {
			name string
			dump bool
			web  bool
		}{
			{"clickable", dump.clickable, web.clickable},
			{"enabled", dump.enabled, web.enabled},
			{"editable", dump.editable, web.editable},
			{"scrollable", dump.scrollable, web.scrollable},
			{"positiveBounds", dump.positiveBounds, web.positiveBounds},
		} {
			if fact.dump != fact.web {
				t.Errorf(
					"%q (<%s>): the hierarchy dump derives %s=%v, the web runtime "+
						"derives %s=%v",
					row.id,
					dump.tag,
					fact.name,
					fact.dump,
					fact.name,
					fact.web,
				)
			}
		}
	}
}

func enumeratedIDs(rows []factRow) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.id)
	}
	return ids
}

func repoRootDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller path")
	}
	return filepath.Clean(
		filepath.Join(filepath.Dir(thisFile), "..", "..", ".."),
	)
}
