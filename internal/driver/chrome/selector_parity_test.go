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
		selector string
		want     []string
	}{
		{"idPrefix:customer_row_", []string{"customer_row_a1", "customer_row_b2"}},
		{"idPrefix:customer_", []string{"customer_list", "customer_row_a1", "customer_row_b2"}},
		{"idPrefix:invoice_row_", nil},
		{"id:summary_card", []string{"summary_card"}},
		{"descPrefix:customer_row_", []string{"customer_row_a1", "customer_row_b2"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.selector, func(t *testing.T) {
			native := selectorIDsFromDump(tree, testCase.selector)
			web := selectorIDsFromWebRuntime(ctx, t, d, testCase.selector)
			if !slices.Equal(native, testCase.want) {
				t.Errorf("hierarchy matched %v, want %v", native, testCase.want)
			}
			if !slices.Equal(web, testCase.want) {
				t.Errorf("the web runtime matched %v, want %v", web, testCase.want)
			}
		})
	}
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
	request, err := json.Marshal(selector)
	if err != nil {
		t.Fatalf("encode selector: %v", err)
	}
	var encoded string
	script := `JSON.stringify(window.__sanderlingSelectorMatches__(` + string(request) + `))`
	if err := chromedp.Run(d.tabCtx, chromedp.Evaluate(script, &encoded)); err != nil {
		t.Fatalf("read web runtime matches for %q: %v", selector, err)
	}
	var ids []string
	if err := json.Unmarshal([]byte(encoded), &ids); err != nil {
		t.Fatalf("decode web runtime matches: %v", err)
	}
	return ids
}
