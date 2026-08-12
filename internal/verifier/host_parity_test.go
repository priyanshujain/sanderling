package verifier

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// hostParityScreen is the canonical screen both hosts are driven over: one row
// per fact combination that any verb distinguishes. Each host builds it in its
// own model (this file as a hierarchy tree, pkg/spec/test/host-parity.test.ts as
// a DOM), enumerates every verb, and asserts the SAME committed golden.
var hostParityScreen = []struct {
	name           string
	clickable      bool
	enabled        bool
	editable       bool
	scrollable     bool
	positiveBounds bool
}{
	{name: "root", enabled: true, scrollable: true, positiveBounds: true},
	{name: "save", clickable: true, enabled: true, positiveBounds: true},
	{name: "cancel", clickable: true, positiveBounds: true},
	{name: "amount", enabled: true, editable: true, positiveBounds: true},
	{name: "list", enabled: true, scrollable: true, positiveBounds: true},
	{name: "row", enabled: true, positiveBounds: true},
	{name: "collapsed", clickable: true, enabled: true},
}

// hostParityTreeJSON is hostParityScreen as the native host sees it. Tree order
// is pre-order, so it matches hostParityScreen index for index.
const hostParityTreeJSON = `{
  "attributes": {"resource-id": "root", "scrollable": "true", "bounds": "[0,0,400,800]"},
  "enabled": true,
  "children": [
    {"attributes": {"resource-id": "save", "bounds": "[0,0,200,60]"}, "clickable": true, "enabled": true, "children": []},
    {"attributes": {"resource-id": "cancel", "bounds": "[200,0,400,60]"}, "clickable": true, "enabled": false, "children": []},
    {"attributes": {"resource-id": "amount", "bounds": "[0,100,400,160]"}, "editable": true, "enabled": true, "children": []},
    {"attributes": {"resource-id": "list", "scrollable": "true", "bounds": "[0,200,400,600]"}, "enabled": true, "children": []},
    {"attributes": {"resource-id": "row", "bounds": "[0,600,400,680]"}, "enabled": true, "children": []},
    {"attributes": {"resource-id": "collapsed", "bounds": "[0,0,0,0]"}, "clickable": true, "enabled": true, "children": []}
  ]
}`

// TestHostsAgreeOnTargetEligibility is the guard on the claim that one
// specification induces one action space on every platform. The native host and
// the web host used to route verbs themselves and had drifted: web sent
// `swipes` to scrollable containers only, so a swipe on a list row was
// reachable on Android and unreachable on web for the same spec.
//
// Per-verb eligibility now has ONE definition (pkg/spec/src/targets.ts); a host
// reports facts and never filters. This test is what notices if a second
// definition grows back on either side: both hosts enumerate the same canonical
// screen and must name the same targets, verb for verb, in the same order.
// pkg/spec/test/host-parity.test.ts asserts the SAME golden from the web host,
// so a match on both sides proves the two hosts agree without either invoking
// the other.
func TestHostsAgreeOnTargetEligibility(t *testing.T) {
	golden := loadHostParityGolden(t)
	for _, verb := range policyVerbs {
		t.Run(verb, func(t *testing.T) {
			want, ok := golden[verb]
			if !ok {
				t.Fatalf("golden has no entry for %s", verb)
			}
			verifier := newVerifier(t, WithSeed(0x5eed))
			loadActionSpec(t, verifier, fmt.Sprintf(
				"import { %s } from \"@sanderling/spec\";\nglobalThis.actions = %s;", verb, verb))
			pushTree(t, verifier, hostParityTreeJSON)

			entries, err := verifier.enumerateBuiltin(verb)
			if err != nil {
				t.Fatalf("enumerate %s: %v", verb, err)
			}
			if len(entries) == 0 {
				t.Fatalf("%s enumerated nothing at all", verb)
			}
			got := []string{}
			for _, entry := range entries {
				if entry.targetIndex < 0 {
					continue
				}
				if entry.targetIndex >= len(hostParityScreen) {
					t.Fatalf("%s produced target index %d, off the %d-row screen",
						verb, entry.targetIndex, len(hostParityScreen))
				}
				got = append(got, hostParityScreen[entry.targetIndex].name)
			}
			if !slices.Equal(got, want) {
				t.Errorf("native host targets for %s\n got=%v\nwant=%v", verb, got, want)
			}
		})
	}
}

func loadHostParityGolden(t *testing.T) map[string][]string {
	t.Helper()
	path, err := filepath.Abs("../../pkg/spec/test/fixtures/host-parity-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	golden := map[string][]string{}
	if err := json.Unmarshal(body, &golden); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	return golden
}
