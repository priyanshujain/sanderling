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
			name:     "class",
			selector: "class:status",
			object:   objectSelector("class", "status"),
			want:     []string{"nested_row", "nested_badge"},
		},
		{
			// className is the DOM property name for the same attribute, and
			// the two names have to name the same elements: the web runtime
			// compiles both to the same class query, while the dump carries
			// the fact under `class` alone and no alias reached it, so this
			// key matched the row and the badge on one host and NOTHING on the
			// other. The key is accepted, so no unknown-key error fires, and a
			// property over the missing element passes having checked nothing.
			name:     "className",
			selector: "className:status",
			object:   objectSelector("className", "status"),
			want:     []string{"nested_row", "nested_badge"},
		},
		{
			// Every name for the accessible label has to name one element. Four
			// of the six reached it on one host only: label and
			// accessibilityLabel aliased onto accessibilityText alone, which
			// the ios sidecar writes and this dump does not, and alias
			// expansion is one level; ariaLabel and contentDescription aliased
			// onto nothing. accessibilityText was the mirror image, resolving
			// against the dump and reaching no DOM attribute of that name.
			name:     "ariaLabel",
			selector: "ariaLabel:login_email",
			object:   objectSelector("ariaLabel", "login_email"),
			want:     []string{"login_email"},
		},
		{
			name:     "contentDescription",
			selector: "contentDescription:login_email",
			object:   objectSelector("contentDescription", "login_email"),
			want:     []string{"login_email"},
		},
		{
			name:     "label",
			selector: "label:login_email",
			object:   objectSelector("label", "login_email"),
			want:     []string{"login_email"},
		},
		{
			name:     "accessibilityLabel",
			selector: "accessibilityLabel:login_email",
			object:   objectSelector("accessibilityLabel", "login_email"),
			want:     []string{"login_email"},
		},
		{
			name:     "accessibilityText",
			selector: "accessibilityText:login_email",
			object:   objectSelector("accessibilityText", "login_email"),
			want:     []string{"login_email"},
		},
		{
			// The two ios names for the identifier, which resolved against the
			// dump through an alias and against no DOM attribute at all.
			name:     "identifier",
			selector: "identifier:summary_card",
			object:   objectSelector("identifier", "summary_card"),
			want:     []string{"summary_card"},
		},
		{
			name:     "accessibilityIdentifier",
			selector: "accessibilityIdentifier:summary_card",
			object:   objectSelector("accessibilityIdentifier", "summary_card"),
			want:     []string{"summary_card"},
		},
		{
			// The ios name for the class, the same way round.
			name:     "elementType",
			selector: "elementType:status",
			object:   objectSelector("elementType", "status"),
			want:     []string{"nested_row", "nested_badge"},
		},
		{
			// Compose for Web writes a test tag as data-testid. testTag reached
			// the three identifier keys and not that one, and testID reached
			// nothing at all, so both named every row of the list on web and no
			// element here.
			name:     "testID",
			selector: "testID:customer-row",
			object:   objectSelector("testID", "customer-row"),
			want:     []string{"customer_row_a1", "customer_row_b2"},
		},
		{
			name:     "testTag",
			selector: "testTag:customer-row",
			object:   objectSelector("testTag", "customer-row"),
			want:     []string{"customer_row_a1", "customer_row_b2"},
		},
		{
			// A custom element's tag name holds a real tag name inside it, so a
			// substring rule answered tag:li with <todo-list> and tag:a with
			// <todo-app>: the container, not the row the author named.
			name:     "tag against a page of custom elements",
			selector: "tag:li",
			object:   objectSelector("tag", "li"),
			want:     []string{"todo_1", "todo_2"},
		},
		{
			name:     "tag naming the custom element itself",
			selector: "tag:todo-list",
			object:   objectSelector("tag", "todo-list"),
			want:     []string{"todo_list"},
		},
		{
			name:     "tag naming an anchor",
			selector: "tag:a",
			object:   objectSelector("tag", "a"),
			want:     []string{"todo_link"},
		},
		{
			// secure names no attribute the markup writes: both producers derive
			// it from the field's type. Matched as a raw attribute it reached
			// nothing here while resolving fine against the dump, and `secure`
			// being an accepted key meant no unknown-key error said so.
			name:     "secure",
			selector: "secure:true",
			object:   objectSelector("secure", "true"),
			want:     []string{"login_password"},
		},
		{
			// false is every editable field that is not a password entry, not
			// everything that is not one: a control that is no field at all
			// reports null, the way android reports null for everything, and
			// answers to neither value.
			name:     "not secure",
			selector: "secure:false",
			object:   objectSelector("secure", "false"),
			want:     []string{"login_email", "login_note", "login_terms"},
		},
		{
			// The head subtree renders nothing, so the hierarchy dump drops it
			// (buildTree in driver.go) and so does the enumeration the picker
			// walks (targetElements in web-runtime.ts). A selector resolving
			// into it names an element the goja host cannot see at all.
			name:     "tag naming the head element",
			selector: "tag:head",
			object:   objectSelector("tag", "head"),
			want:     nil,
		},
		{
			name:     "tag naming an element inside the head",
			selector: "tag:title",
			object:   objectSelector("tag", "title"),
			want:     nil,
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

// A multi-key object selector concatenates its parts into ONE compound CSS
// selector, and a type selector is valid only at the head of a compound. secure
// resolves to a type selector, so pairing it with any key that sorts before it
// turned the whole selector into a parse error: querySelectorAll throws, and
// what a spec sees is an exception out of the extractor rather than an element.
func TestSelectors_SecureCombinesWithAnotherKey(t *testing.T) {
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

	selector := hierarchy.Selector{Filters: []hierarchy.AttrFilter{
		{Attr: "id", Value: "login_password"},
		{Attr: "secure", Value: "true"},
	}}
	want := []string{"login_password"}
	if native := selectorIDsFromDumpObject(tree, selector); !slices.Equal(native, want) {
		t.Errorf("hierarchy matched %v, want %v", native, want)
	}
	encoded := objectSelectorJSON(selector)
	if web := selectorIDsFromWebRuntime(ctx, t, d, encoded); !slices.Equal(web, want) {
		t.Errorf("the web runtime matched %v for %s, want %v", web, encoded, want)
	}
}

// The other seven boolean states are derived from the live element the same way
// secure is, and were reached the same wrong way: as a markup attribute, which
// builds [clickable="true"] and matches nothing on any page. The key is
// accepted, so no unknown-key error fires, and the worked example in
// docs/manual/spec-language.md, find({testTag: ..., clickable: true}), resolved
// to no element at all on web while resolving against the dump on the goja host.
//
// Half the states are asked inside one container, because a state the whole page
// has an opinion about (everything is enabled, almost nothing is checked)
// answers with most of the document and a want list nobody can check by reading.
func TestSelectors_BooleanStatesNameWhatBothProducersReport(t *testing.T) {
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

	cases := []struct {
		name     string
		selector string
		scope    string
		want     []string
	}{
		{
			name:     "clickable",
			selector: "clickable:true",
			want: []string{
				"login_email", "login_password", "login_note", "login_remember",
				"state_save", "state_cancel", "state_submit", "state_remember",
				"state_agree", "state_month", "todo_link",
			},
		},
		{
			name:     "not clickable",
			selector: "clickable:false",
			scope:    "state_row",
			want:     []string{"state_january", "state_february"},
		},
		{
			// A control that carries no disabled property is marked by
			// aria-disabled alone, and both producers read both.
			name:     "not enabled",
			selector: "enabled:false",
			want:     []string{"state_cancel", "state_submit"},
		},
		{
			name:     "enabled",
			selector: "enabled:true",
			scope:    "state_row",
			want: []string{
				"state_save", "state_remember", "state_agree", "state_month",
				"state_january", "state_february",
			},
		},
		{
			name:     "focused",
			selector: "focused:true",
			want:     []string{"state_save"},
		},
		{
			name:     "not focused",
			selector: "focused:false",
			scope:    "state_row",
			want: []string{
				"state_cancel", "state_submit", "state_remember", "state_agree",
				"state_month", "state_january", "state_february",
			},
		},
		{
			// state_remember was ticked by script and carries no checked
			// attribute; state_agree carries the attribute and was cleared. The
			// markup attribute is the page's starting state, so a selector
			// reading it names the pair the wrong way round.
			name:     "checked",
			selector: "checked:true",
			want:     []string{"state_remember"},
		},
		{
			name:     "not checked",
			selector: "checked:false",
			scope:    "state_row",
			want: []string{
				"state_save", "state_cancel", "state_submit", "state_agree",
				"state_month", "state_january", "state_february",
			},
		},
		{
			// The first option of a select is selected without the markup
			// saying so anywhere.
			name:     "selected",
			selector: "selected:true",
			want:     []string{"state_january"},
		},
		{
			name:     "not selected",
			selector: "selected:false",
			scope:    "state_row",
			want: []string{
				"state_save", "state_cancel", "state_submit", "state_remember",
				"state_agree", "state_month", "state_february",
			},
		},
		{
			// editable and scrollable are derived the same way and were reached
			// the same wrong way, as markup attributes nothing carries.
			name:     "editable",
			selector: "editable:true",
			want:     []string{"login_email", "login_password", "login_note", "login_terms"},
		},
		{
			name:     "not editable",
			selector: "editable:false",
			scope:    "login_form",
			want:     []string{"login_remember"},
		},
		{
			name:     "scrollable",
			selector: "scrollable:true",
			scope:    "scroll_row",
			want:     []string{"scroll_box"},
		},
		{
			// Both producers state scrollable only where it holds, so the
			// container that does not scroll answers to neither value, the way
			// an element that is no field at all answers to neither value of
			// secure.
			name:     "not scrollable",
			selector: "scrollable:false",
			scope:    "scroll_row",
			want:     nil,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			key, value, _ := strings.Cut(testCase.selector, ":")
			object := objectSelector(key, value)
			encoded := objectSelectorJSON(object)
			if testCase.scope != "" {
				scope := objectSelector("id", testCase.scope)
				path := []hierarchy.Selector{scope, object}
				encoded = "[" + objectSelectorJSON(scope) + "," + encoded + "]"
				if native := selectorIDsFromDumpPath(tree, path); !slices.Equal(
					native,
					testCase.want,
				) {
					t.Errorf("the dump matched %v under %s, want %v",
						native, testCase.scope, testCase.want)
				}
			} else {
				if native := selectorIDsFromDump(tree, testCase.selector); !slices.Equal(
					native,
					testCase.want,
				) {
					t.Errorf("the dump matched %v for %s, want %v",
						native, testCase.selector, testCase.want)
				}
				if web := selectorIDsFromWebRuntime(
					ctx, t, d, testCase.selector,
				); !slices.Equal(web, testCase.want) {
					t.Errorf("the web runtime matched %v for %s, want %v",
						web, testCase.selector, testCase.want)
				}
				if native := selectorIDsFromDumpObject(tree, object); !slices.Equal(
					native,
					testCase.want,
				) {
					t.Errorf("the dump object form matched %v, want %v",
						native, testCase.want)
				}
			}
			if web := selectorIDsFromWebRuntime(ctx, t, d, encoded); !slices.Equal(
				web,
				testCase.want,
			) {
				t.Errorf("the web runtime matched %v for %s, want %v",
					web, encoded, testCase.want)
			}
		})
	}
}

// A type selector is valid only at the head of a compound, and a multi-key
// object selector concatenates its parts into one, so {id, tag} built
// '[id="state_month"]select' and querySelectorAll threw a SyntaxError. Which of
// the two a spec got depended on the order its author wrote the keys in.
func TestSelectors_TagCombinesWithAnotherKey(t *testing.T) {
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

	selector := hierarchy.Selector{Filters: []hierarchy.AttrFilter{
		{Attr: "id", Value: "state_month"},
		{Attr: "tag", Value: "select"},
	}}
	want := []string{"state_month"}
	if native := selectorIDsFromDumpObject(tree, selector); !slices.Equal(native, want) {
		t.Errorf("hierarchy matched %v, want %v", native, want)
	}
	encoded := objectSelectorJSON(selector)
	if web := selectorIDsFromWebRuntime(ctx, t, d, encoded); !slices.Equal(web, want) {
		t.Errorf("the web runtime matched %v for %s, want %v", web, encoded, want)
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

// text written beside another key was dropped, so the web runtime matched on
// the other key alone: {data-testid, text} selected every row carrying the tag
// where internal/hierarchy selected the one row the author named. Matching MORE
// than the spec said is silent, which is the failure this file exists to catch:
// a find lands on a row nobody wrote and every property over it still passes.
//
// The innermost rule stays where internal/hierarchy holds it, over what the
// WHOLE selector matched. nested_row keeps its match because the badge under it
// carries a different id, and state_month keeps one because the option carrying
// "January" is not clickable: resolving text to its own innermost match before
// the other keys narrow anything answers with nothing in both.
func TestSelectors_TextCombinesWithAnotherKey(t *testing.T) {
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

	cases := []struct {
		name    string
		filters []hierarchy.AttrFilter
		want    []string
	}{
		{
			name: "text beside the tag every row of the list carries",
			filters: []hierarchy.AttrFilter{
				{Attr: "data-testid", Value: "customer-row"},
				{Attr: "text", Value: "Alice"},
			},
			want: []string{"customer_row_a1"},
		},
		{
			// Object keys iterate in the order the author wrote them, and an
			// earlier fix had to keep a rule valid wherever in the compound it
			// lands.
			name: "text before the tag every row of the list carries",
			filters: []hierarchy.AttrFilter{
				{Attr: "text", Value: "Alice"},
				{Attr: "data-testid", Value: "customer-row"},
			},
			want: []string{"customer_row_a1"},
		},
		{
			name: "text beside the id of the row carrying it",
			filters: []hierarchy.AttrFilter{
				{Attr: "id", Value: "customer_row_a1"},
				{Attr: "text", Value: "Alice"},
			},
			want: []string{"customer_row_a1"},
		},
		{
			// The row named by the id carries different text, so the selector
			// names nothing at all. Dropping text answered with the row.
			name: "text beside the id of a row carrying something else",
			filters: []hierarchy.AttrFilter{
				{Attr: "id", Value: "customer_row_a1"},
				{Attr: "text", Value: "Bob"},
			},
			want: nil,
		},
		{
			name: "text before the id of a row carrying something else",
			filters: []hierarchy.AttrFilter{
				{Attr: "text", Value: "Bob"},
				{Attr: "id", Value: "customer_row_a1"},
			},
			want: nil,
		},
		{
			name: "text beside the id of the row a badge repeats it in",
			filters: []hierarchy.AttrFilter{
				{Attr: "id", Value: "nested_row"},
				{Attr: "text", Value: "Sent"},
			},
			want: []string{"nested_row"},
		},
		{
			// Both carry the class and both carry the text, so the row is a
			// match the badge under it already made.
			name: "text beside a class the row and the badge under it share",
			filters: []hierarchy.AttrFilter{
				{Attr: "class", Value: "status"},
				{Attr: "text", Value: "Sent"},
			},
			want: []string{"nested_badge"},
		},
		{
			name: "text beside a state no query can express",
			filters: []hierarchy.AttrFilter{
				{Attr: "text", Value: "January"},
				{Attr: "clickable", Value: "true"},
			},
			want: []string{"state_month"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			selector := hierarchy.Selector{Filters: testCase.filters}
			if native := selectorIDsFromDumpObject(tree, selector); !slices.Equal(
				native,
				testCase.want,
			) {
				t.Errorf("the dump matched %v, want %v", native, testCase.want)
			}
			encoded := objectSelectorJSON(selector)
			if web := selectorIDsFromWebRuntime(ctx, t, d, encoded); !slices.Equal(
				web,
				testCase.want,
			) {
				t.Errorf("the web runtime matched %v for %s, want %v",
					web, encoded, testCase.want)
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

// objectSelectorJSON encodes the selector as the object a spec writes, keys in
// the order the filters state them. A JS object iterates in insertion order, and
// where in the compound a key lands is what decided whether the selector parsed
// at all, so a map here would only ever test the alphabetical order.
func objectSelectorJSON(sel hierarchy.Selector) string {
	parts := make([]string, 0, len(sel.Filters))
	for _, filter := range sel.Filters {
		key, err := json.Marshal(filter.Attr)
		if err != nil {
			panic(err)
		}
		value, err := json.Marshal(filter.Value)
		if err != nil {
			panic(err)
		}
		parts = append(parts, string(key)+":"+string(value))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func selectorIDsFromDumpPath(tree *hierarchy.Tree, path []hierarchy.Selector) []string {
	var ids []string
	for _, node := range tree.FindAllBySelectorPath(path) {
		ids = append(ids, node.Element.ResourceID)
	}
	return ids
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
