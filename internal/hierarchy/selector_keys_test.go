package hierarchy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// selectorKeysGolden is the cross-runtime contract for object selectors: the
// keys both runtimes accept, and the diagnostic both raise for a key neither
// can match.
type selectorKeysGolden struct {
	Keys              []string `json:"keys"`
	UnknownKeyExample []string `json:"unknownKeyExample"`
	UnknownKeyMessage string   `json:"unknownKeyMessage"`
}

// Both runtimes reject an object-selector key they do not know, so the two key
// lists have to be one list. Were they to drift, a spec would be accepted by
// the runtime that lists the key and fail the run on the one that does not, and
// the difference would only show on the platform nobody ran first.
// pkg/spec/test/selector-keys.test.ts asserts the SAME file from the web side.
func TestSelectorKeysMatchTheCrossRuntimeList(t *testing.T) {
	golden := loadSelectorKeysGolden(t)
	if got := SelectorKeys(); !slices.Equal(got, golden.Keys) {
		t.Errorf("native key list\n got=%v\nwant=%v", got, golden.Keys)
	}
}

// An author who hits this on Android and again on web must read one sentence,
// not two dialects of it.
func TestUnknownSelectorKeyMessageMatchesTheCrossRuntimeText(t *testing.T) {
	golden := loadSelectorKeysGolden(t)
	if got := UnknownSelectorKeyMessage(golden.UnknownKeyExample); got != golden.UnknownKeyMessage {
		t.Errorf("native message\n got=%q\nwant=%q", got, golden.UnknownKeyMessage)
	}
}

func TestSelectorKeysAreSorted(t *testing.T) {
	keys := SelectorKeys()
	if !slices.IsSorted(keys) {
		t.Errorf("keys must stay sorted so the two lists compare readably: %v", keys)
	}
}

func loadSelectorKeysGolden(t *testing.T) selectorKeysGolden {
	t.Helper()
	path, err := filepath.Abs("../../pkg/spec/test/fixtures/selector-keys.json")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read selector keys: %v", err)
	}
	var golden selectorKeysGolden
	if err := json.Unmarshal(body, &golden); err != nil {
		t.Fatalf("decode selector keys: %v", err)
	}
	return golden
}
