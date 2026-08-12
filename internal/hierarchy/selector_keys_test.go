package hierarchy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Both runtimes reject an object-selector key they do not know, so the two key
// lists have to be one list. Were they to drift, a spec would be accepted by
// the runtime that lists the key and fail the run on the one that does not, and
// the difference would only show on the platform nobody ran first.
// pkg/spec/test/selector-keys.test.ts asserts the SAME file from the web side.
func TestSelectorKeysMatchTheCrossRuntimeList(t *testing.T) {
	path, err := filepath.Abs("../../pkg/spec/test/fixtures/selector-keys.json")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read selector keys: %v", err)
	}
	var want []string
	if err := json.Unmarshal(body, &want); err != nil {
		t.Fatalf("decode selector keys: %v", err)
	}
	if got := SelectorKeys(); !slices.Equal(got, want) {
		t.Errorf("native key list\n got=%v\nwant=%v", got, want)
	}
}

func TestSelectorKeysAreSorted(t *testing.T) {
	keys := SelectorKeys()
	if !slices.IsSorted(keys) {
		t.Errorf("keys must stay sorted so the two lists compare readably: %v", keys)
	}
}
