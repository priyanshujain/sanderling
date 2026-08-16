//go:build browser

package chrome

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/priyanshujain/sanderling/internal/bundler"
	"github.com/priyanshujain/sanderling/internal/hierarchy"
)

// One page, two selector matchers.
//
// A selector means whatever the runtime resolving it decides. The goja host
// resolves it against the hierarchy dump through internal/hierarchy; the V8 host
// resolves it against the live DOM through pkg/spec/src/web-runtime.ts. Nothing
// makes the two agree, so a spec that targets a list of rows can act on them on
// one platform and find nothing on the other, and finding nothing is silent: the
// generator yields no action and the run still passes.
//
// This test starts from one real page in a real browser and asks both matchers
// the same questions.
func TestSelectors_ResolveTheSameElementsAsTheWebRuntime(t *testing.T) {
	server := httptest.NewServer(http.FileServer(http.Dir("testdata")))
	defer server.Close()

	d := New()
	defer d.Terminate(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := d.Launch(ctx, server.URL+"/selector-parity.html", false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	dump, err := d.Hierarchy(ctx)
	if err != nil {
		t.Fatalf("Hierarchy: %v", err)
	}
	tree, err := hierarchy.Parse(dump)
	if err != nil {
		t.Fatalf("parse hierarchy: %v", err)
	}
	installSelectorProbe(ctx, t, d)

	// want is stated here rather than derived, so a matcher that stops matching
	// on both sides at once cannot pass this by agreeing on nothing.
	cases := []struct {
		name     string
		selector string
		object   hierarchy.Selector
		want     []string
	}{
		{
			name:     "idPrefix",
			selector: "idPrefix:customer_row_",
			object:   objectSelector("idPrefix", "customer_row_"),
			want:     []string{"customer_row_a1", "customer_row_b2"},
		},
		{
			name:     "idPrefix spanning the container",
			selector: "idPrefix:customer_",
			object:   objectSelector("idPrefix", "customer_"),
			want:     []string{"customer_list", "customer_row_a1", "customer_row_b2"},
		},
		{
			name:     "idPrefix matching nothing",
			selector: "idPrefix:invoice_row_",
			object:   objectSelector("idPrefix", "invoice_row_"),
			want:     nil,
		},
		{
			name:     "id",
			selector: "id:summary_card",
			object:   objectSelector("id", "summary_card"),
			want:     []string{"summary_card"},
		},
		{
			name:     "descPrefix",
			selector: "descPrefix:customer_row_",
			object:   objectSelector("descPrefix", "customer_row_"),
			want:     []string{"customer_row_a1", "customer_row_b2"},
		},
		{
			name:     "desc",
			selector: "desc:customer_row_a1",
			object:   objectSelector("desc", "customer_row_a1"),
			want:     []string{"customer_row_a1"},
		},
		{
			name:     "data-testid",
			selector: "data-testid:customer-row",
			object:   objectSelector("data-testid", "customer-row"),
			want:     []string{"customer_row_a1", "customer_row_b2"},
		},
		{
			name:     "raw attribute, matched on a substring",
			selector: "data-state:sent",
			object:   objectSelector("data-state", "sent"),
			want:     []string{"status_badge"},
		},
		{
			// The root element answers a selector like any other: the string
			// form scans from the root down, and the object form used to start
			// at the root's children and lose it.
			name:     "id naming the root element",
			selector: "id:page",
			object:   objectSelector("id", "page"),
			want:     []string{"page"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			native := selectorIDsFromDump(tree, testCase.selector)
			web := selectorIDsFromWebRuntime(ctx, t, d, testCase.selector)
			if !slices.Equal(native, testCase.want) {
				t.Errorf("hierarchy matched %v, want %v", native, testCase.want)
			}
			if !slices.Equal(web, testCase.want) {
				t.Errorf("the web runtime matched %v, want %v", web, testCase.want)
			}

			// The object form has to mean the same thing as the string form on
			// both sides. It is the form the typed API pushes authors toward and
			// the one that used to fall through to a raw attribute lookup.
			nativeObject := selectorIDsFromDumpObject(tree, testCase.object)
			webObject := selectorIDsFromWebRuntime(ctx, t, d, objectSelectorJSON(testCase.object))
			if !slices.Equal(nativeObject, testCase.want) {
				t.Errorf("hierarchy object form matched %v, want %v", nativeObject, testCase.want)
			}
			if !slices.Equal(webObject, testCase.want) {
				t.Errorf("the web runtime object form matched %v, want %v", webObject, testCase.want)
			}
		})
	}
}

// `text:` is a substring match on text content wherever the spec runs
// (docs/manual/spec-language.md), so a badge reading "Sent ✓" answers to
// text:Sent on web the way it already does on Android and iOS, and one reading
// "3 unsent" out of two text nodes answers to text:unsent. It names the
// innermost match: an element's text is its whole subtree's text, so a badge's
// ancestors up to the document root read as matches too, and the deepest one is
// the element the author meant.
func TestSelectors_TextNamesTheInnermostMatchInEveryResolver(t *testing.T) {
	server := httptest.NewServer(http.FileServer(http.Dir("testdata")))
	defer server.Close()

	d := New()
	defer d.Terminate(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := d.Launch(ctx, server.URL+"/selector-parity.html", false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	dump, err := d.Hierarchy(ctx)
	if err != nil {
		t.Fatalf("Hierarchy: %v", err)
	}
	tree, err := hierarchy.Parse(dump)
	if err != nil {
		t.Fatalf("parse hierarchy: %v", err)
	}
	installSelectorProbe(ctx, t, d)

	// split_row is the ancestor that keeps its match: its own text carries the
	// value where no descendant of it does. nested_row is the ancestor that
	// loses it: its badge carries the value too, and the badge is the match.
	cases := []struct {
		name       string
		selector   string
		want       []string
		scope      string
		scopedWant []string
	}{
		{
			name:     "one text node",
			selector: "text:Sent",
			want: []string{
				"status_badge",
				"draft_badge",
				"split_row",
				"nested_badge",
			},
			scope:      "status_row",
			scopedWant: []string{"status_badge"},
		},
		{
			// React writes `{count} unsent` as two text nodes, and an XPath over
			// text() reads only the first of them.
			name:       "text split across text nodes",
			selector:   "text:unsent",
			want:       []string{"unsent_badge"},
			scope:      "unsent_row",
			scopedWant: []string{"unsent_badge"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			value := strings.TrimPrefix(testCase.selector, "text:")
			if native := selectorIDsFromDump(tree, testCase.selector); !slices.Equal(
				native,
				testCase.want,
			) {
				t.Errorf(
					"the dump matched %v for %s, want %v",
					native,
					testCase.selector,
					testCase.want,
				)
			}
			if web := selectorIDsFromWebRuntime(ctx, t, d, testCase.selector); !slices.Equal(
				web,
				testCase.want,
			) {
				t.Errorf(
					"the web runtime matched %v for %s, want %v",
					web,
					testCase.selector,
					testCase.want,
				)
			}
			object := objectSelectorJSON(objectSelector("text", value))
			if web := selectorIDsFromWebRuntime(ctx, t, d, object); !slices.Equal(
				web,
				testCase.want,
			) {
				t.Errorf(
					"the web runtime matched %v for %s, want %v",
					web,
					object,
					testCase.want,
				)
			}

			xpath, isXPath, err := TranslateStringSelector(testCase.selector)
			if err != nil {
				t.Fatalf(
					"TranslateStringSelector(%q): %v",
					testCase.selector,
					err,
				)
			}
			if !isXPath {
				t.Fatalf(
					"TranslateStringSelector(%q) returned CSS, want an XPath",
					testCase.selector,
				)
			}
			if overCDP := xpathIDsOverCDP(ctx, t, d, xpath); !slices.Equal(
				overCDP,
				testCase.want,
			) {
				t.Errorf(
					"the CDP selector %q matched %v, want %v",
					xpath,
					overCDP,
					testCase.want,
				)
			}

			// Scoped to one row, `text:` reads that row, the way every other
			// selector does: an XPath anchored at the document root answers for
			// the whole page however the caller scoped the lookup.
			path := []hierarchy.Selector{
				objectSelector("id", testCase.scope),
				objectSelector("text", value),
			}
			var scoped []string
			for _, node := range tree.FindAllBySelectorPath(path) {
				scoped = append(scoped, node.Element.ResourceID)
			}
			if !slices.Equal(scoped, testCase.scopedWant) {
				t.Errorf(
					"the dump matched %v within %s, want %v",
					scoped,
					testCase.scope,
					testCase.scopedWant,
				)
			}
			pathJSON := objectSelectorJSON(objectSelector("id", testCase.scope))
			pathJSON = "[" + pathJSON + "," + object + "]"
			if web := selectorIDsFromWebRuntime(ctx, t, d, pathJSON); !slices.Equal(
				web,
				testCase.scopedWant,
			) {
				t.Errorf(
					"the web runtime matched %v for %s, want %v",
					web,
					pathJSON,
					testCase.scopedWant,
				)
			}
		})
	}
}

// xpathIDsOverCDP resolves an XPath the way TapSelector does, over CDP against
// the live document, and reads back the ids it matched in document order.
func xpathIDsOverCDP(
	ctx context.Context,
	t *testing.T,
	d *Driver,
	xpath string,
) []string {
	t.Helper()
	var ids []string
	script := `(() => {
	  const found = document.evaluate(` + jsArgument(xpath) + `, document, null,
	    XPathResult.ORDERED_NODE_SNAPSHOT_TYPE, null);
	  const ids = [];
	  for (let i = 0; i < found.snapshotLength; i++) ids.push(found.snapshotItem(i).id);
	  return ids;
	})()`
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(script, &ids)); err != nil {
		t.Fatalf("resolve %q over CDP: %v", xpath, err)
	}
	return ids
}

// A third resolver reads the same selector: TapSelector hands it to CDP as the
// CSS TranslateStringSelector builds. The runner uses both within one InputText
// step, resolving the target in the dump and tapping it over CDP, so a selector
// the two read differently taps one element and reads the text of another.
func TestSelectors_DataTestIDNamesTheSameElementInTheDumpAndOverCDP(t *testing.T) {
	server := httptest.NewServer(http.FileServer(http.Dir("testdata")))
	defer server.Close()

	d := New()
	defer d.Terminate(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := d.Launch(ctx, server.URL+"/selector-parity.html", false, nil); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	dump, err := d.Hierarchy(ctx)
	if err != nil {
		t.Fatalf("Hierarchy: %v", err)
	}
	tree, err := hierarchy.Parse(dump)
	if err != nil {
		t.Fatalf("parse hierarchy: %v", err)
	}

	const selector = "data-testid:summary"
	element := tree.Find(selector)
	if element == nil {
		t.Fatalf("the dump resolves %s to nothing, so every step that names a target this way loses it", selector)
	}
	css, isXPath, err := TranslateStringSelector(selector)
	if err != nil {
		t.Fatalf("TranslateStringSelector(%q): %v", selector, err)
	}
	if isXPath {
		t.Fatalf("TranslateStringSelector(%q) returned an XPath, want CSS", selector)
	}
	var overCDP string
	script := `(document.querySelector(` + jsArgument(css) + `) || {}).id || ""`
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(script, &overCDP)); err != nil {
		t.Fatalf("resolve %q over CDP: %v", css, err)
	}
	if overCDP == "" {
		t.Fatalf("the CDP selector %q matched nothing", css)
	}
	if element.ResourceID != overCDP {
		t.Errorf("the dump resolves %s to %q, the CDP selector %q to %q",
			selector, element.ResourceID, css, overCDP)
	}
}

func objectSelector(key, value string) hierarchy.Selector {
	return hierarchy.Selector{Filters: []hierarchy.AttrFilter{{Attr: key, Value: value}}}
}

func objectSelectorJSON(sel hierarchy.Selector) string {
	fields := map[string]string{}
	for _, filter := range sel.Filters {
		fields[filter.Attr] = filter.Value
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func selectorIDsFromDumpObject(tree *hierarchy.Tree, sel hierarchy.Selector) []string {
	var ids []string
	for _, node := range tree.FindAllBySelector(sel) {
		ids = append(ids, node.Element.ResourceID)
	}
	return ids
}

// jsArgument passes an object or path selector through as the JS literal it
// already is, and quotes anything else as a string selector. A CSS attribute
// selector opens with a bracket too, so only well-formed JSON passes through.
func jsArgument(selector string) string {
	if json.Valid([]byte(selector)) &&
		(strings.HasPrefix(selector, "{") || strings.HasPrefix(selector, "[")) {
		return selector
	}
	quoted, err := json.Marshal(selector)
	if err != nil {
		return `""`
	}
	return string(quoted)
}

func selectorIDsFromDump(tree *hierarchy.Tree, selector string) []string {
	var ids []string
	for _, element := range tree.FindAll(selector) {
		ids = append(ids, element.ResourceID)
	}
	return ids
}

func installSelectorProbe(ctx context.Context, t *testing.T, d *Driver) {
	t.Helper()
	specSource := filepath.Join(repoRootDir(t), "pkg", "spec")
	probe, err := bundler.BundleWeb(bundler.WebOptions{
		EntryFile:      filepath.Join(specSource, "test", "dom-selector-probe.ts"),
		WebRuntimeFile: filepath.Join(specSource, "src", "web-runtime.ts"),
	})
	if err != nil {
		t.Fatalf("bundle dom selector probe: %v", err)
	}
	if err := d.InstallBundle(ctx, probe.JavaScript); err != nil {
		t.Fatalf("install dom selector probe: %v", err)
	}
}

func selectorIDsFromWebRuntime(
	ctx context.Context,
	t *testing.T,
	d *Driver,
	selector string,
) []string {
	t.Helper()
	var encoded string
	script := `JSON.stringify(window.__sanderlingSelectorMatches__(` + jsArgument(selector) + `))`
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(script, &encoded)); err != nil {
		t.Fatalf("read web runtime matches for %q: %v", selector, err)
	}
	var ids []string
	if err := json.Unmarshal([]byte(encoded), &ids); err != nil {
		t.Fatalf("decode web runtime matches: %v", err)
	}
	return ids
}
