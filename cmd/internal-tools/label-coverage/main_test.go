package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMeasureRanksSelectorsByDurability(t *testing.T) {
	counts := measure([]element{
		{
			Class:      "Button",
			ResourceID: "app:id/save",
			Text:       "Save",
			Clickable:  true,
		},
		{
			Class:       "Button",
			Description: "Filter",
			Text:        "Filter",
			Clickable:   true,
		},
		{Class: "View", Text: "Ramesh Kumar", Clickable: true},
		{Class: "View", Clickable: true},
		{Class: "EditText", ResourceID: "app:id/amount", Editable: true},
		{Class: "TextView", Text: "Balance", Clickable: false},
	})
	if counts.Elements != 6 {
		t.Fatalf("elements: want 6, got %d", counts.Elements)
	}
	if counts.Interactive != 5 {
		t.Fatalf("interactive: want 5, got %d", counts.Interactive)
	}
	if counts.ByIdentifier != 2 {
		t.Errorf("by identifier: want 2, got %d", counts.ByIdentifier)
	}
	if counts.ByDescription != 1 {
		t.Errorf("by description: want 1, got %d", counts.ByDescription)
	}
	if counts.ByTextOnly != 1 {
		t.Errorf("by text only: want 1, got %d", counts.ByTextOnly)
	}
	if counts.Unaddressable != 1 {
		t.Errorf("unaddressable: want 1, got %d", counts.Unaddressable)
	}
}

// The row description a merged Compose semantics node produces carries the
// balance and a relative age that reprices itself every month. It is present,
// so a presence check scores it as a selector; it matches only the run that
// recorded it.
func TestVolatileDescriptionsAreNotCountedAsStable(t *testing.T) {
	row := "Ramesh ji, 95, Pending Collection Since 15 months, Due"
	if !volatile(row) {
		t.Fatalf("expected %q to be treated as data-carrying", row)
	}
	counts := measure([]element{
		{Class: "Button", Description: row, Clickable: true},
		{Class: "Button", Description: "Filter", Clickable: true},
	})
	if counts.ByVolatile != 1 {
		t.Errorf("volatile: want 1, got %d", counts.ByVolatile)
	}
	if counts.ByDescription != 1 {
		t.Errorf("stable description: want 1, got %d", counts.ByDescription)
	}
	if got := counts.stableShare(); got != 50 {
		t.Fatalf("stable share: want 50, got %.1f", got)
	}
	if len(counts.Needing) != 1 ||
		!strings.Contains(counts.Needing[0], "carries its own data") {
		t.Fatalf(
			"the volatile row belongs on the work list, got %v",
			counts.Needing,
		)
	}
}

func TestVolatileLeavesPlainLabelsAlone(t *testing.T) {
	for _, label := range []string{"Filter", "Search", "Add Relationship", "Share", "Skip"} {
		if volatile(label) {
			t.Errorf("%q should count as a durable label", label)
		}
	}
	for _, label := range []string{"₹95", "2 days ago", "Due today", "Pending Since 15 months", "Edited on 11 Jun 2026"} {
		if !volatile(label) {
			t.Errorf("%q carries data and should not count as durable", label)
		}
	}
}

func TestStableShareExcludesDataDependentText(t *testing.T) {
	counts := measure([]element{
		{ResourceID: "app:id/add", Clickable: true},
		{Text: "Ramesh Kumar", Clickable: true},
		{Text: "Suresh Patel", Clickable: true},
		{Clickable: true},
	})
	if got := counts.stableShare(); got != 25 {
		t.Fatalf("stable share: want 25, got %.1f", got)
	}
}

func TestMeasureCountsRepeatedIdentifiersAsAmbiguous(t *testing.T) {
	counts := measure([]element{
		{ResourceID: "app:id/row", Text: "Ramesh", Clickable: true},
		{ResourceID: "app:id/row", Text: "Suresh", Clickable: true},
		{ResourceID: "app:id/add", Clickable: true},
	})
	if counts.AmbiguousIDs != 2 {
		t.Fatalf("ambiguous: want 2, got %d", counts.AmbiguousIDs)
	}
}

func TestAccumulateKeepsRichestObservationPerScreen(t *testing.T) {
	trace := writeTrace(
		t,
		`{"step":0,"screen":"ledger","hierarchy":{"elements":[{"resourceId":"app:id/add","clickable":true}]}}
{"step":1,"screen":"ledger","hierarchy":{"elements":[{"resourceId":"app:id/add","clickable":true},{"text":"Ramesh","clickable":true},{"class":"View","clickable":true}]}}
{"step":2,"screen":"home","hierarchy":{"elements":[{"description":"Filter","clickable":true}]}}
`,
	)
	byScreen := map[string]*Counts{}
	if err := accumulate(trace, byScreen, ""); err != nil {
		t.Fatal(err)
	}
	ledger := byScreen["ledger"]
	if ledger.Interactive != 3 {
		t.Fatalf("ledger interactive: want 3, got %d", ledger.Interactive)
	}
	if ledger.Observations != 2 {
		t.Fatalf("ledger observations: want 2, got %d", ledger.Observations)
	}
	if ledger.Unaddressable != 1 {
		t.Errorf("ledger unaddressable: want 1, got %d", ledger.Unaddressable)
	}
	if byScreen["home"].ByDescription != 1 {
		t.Errorf(
			"home by description: want 1, got %d",
			byScreen["home"].ByDescription,
		)
	}
}

// An idling probe observes one screen many times. Counting each observation
// would report how long the explorer sat there rather than what it could reach.
func TestAccumulateCountsIdenticalHierarchiesOnce(t *testing.T) {
	line := `{"step":0,"screen":"ledger","hierarchy":{"elements":[{"resourceId":"app:id/add","clickable":true}]}}` + "\n"
	byScreen := map[string]*Counts{}
	if err := accumulate(writeTrace(t, strings.Repeat(line, 5)), byScreen, ""); err != nil {
		t.Fatal(err)
	}
	if got := byScreen["ledger"].Observations; got != 1 {
		t.Fatalf("observations: want 1, got %d", got)
	}
}

func TestAccumulateSkipsStepsWithoutHierarchy(t *testing.T) {
	byScreen := map[string]*Counts{}
	err := accumulate(writeTrace(t, `{"step":0,"screen":"ledger"}
{"step":1,"screen":"ledger","hierarchy":{"elements":[]}}
`), byScreen, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(byScreen) != 0 {
		t.Fatalf("want no screens, got %d", len(byScreen))
	}
}

func TestShapeIgnoresRepetitionButNotComposition(t *testing.T) {
	threeRows := shape([]element{
		{ResourceID: "app:id/list"},
		{ResourceID: "app:id/row"},
		{ResourceID: "app:id/row"},
		{ResourceID: "app:id/row"},
	})
	fiveRows := shape([]element{
		{ResourceID: "app:id/list"},
		{ResourceID: "app:id/row"},
		{ResourceID: "app:id/row"},
		{ResourceID: "app:id/row"},
		{ResourceID: "app:id/row"},
		{ResourceID: "app:id/row"},
	})
	if threeRows != fiveRows {
		t.Fatalf(
			"row count changed the screen identity: %s vs %s",
			threeRows,
			fiveRows,
		)
	}
	withDialog := shape([]element{
		{ResourceID: "app:id/list"},
		{ResourceID: "app:id/row"},
		{ResourceID: "app:id/confirm_dialog"},
	})
	if withDialog == threeRows {
		t.Fatal(
			"a screen showing a dialog must not collapse into the screen behind it",
		)
	}
}

func TestAccumulateSeparatesUnnamedScreensByShape(t *testing.T) {
	byScreen := map[string]*Counts{}
	err := accumulate(
		writeTrace(
			t,
			`{"step":0,"hierarchy":{"elements":[{"resourceId":"app:id/ledger","clickable":true}]}}
{"step":1,"hierarchy":{"elements":[{"resourceId":"app:id/add_transaction","clickable":true}]}}
`,
		),
		byScreen,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(byScreen) != 2 {
		t.Fatalf("want 2 screens, got %d", len(byScreen))
	}
	for name := range byScreen {
		if !strings.HasPrefix(name, "shape:") {
			t.Errorf("unnamed screen should be keyed by shape, got %q", name)
		}
	}
}

func TestCollectFindsTracesUnderRunDirectories(t *testing.T) {
	root := t.TempDir()
	for _, run := range []string{"run-1", "run-2"} {
		directory := filepath.Join(root, run)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "trace.jsonl"), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	traces, err := collect([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(traces) != 2 {
		t.Fatalf("want 2 traces, got %d: %v", len(traces), traces)
	}
}

func writeTrace(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A status bar contributes three well-labelled controls to every capture. An
// app with no identifiers of its own scores 100 percent if they are counted.
func TestAccumulateDropsSystemUIByDefault(t *testing.T) {
	trace := writeTrace(
		t,
		`{"step":0,"screen":"ledger","hierarchy":{"elements":[
{"resourceId":"com.android.systemui:id/clock","package":"com.android.systemui","clickable":true},
{"resourceId":"com.android.systemui:id/battery","package":"com.android.systemui","clickable":true},
{"class":"android.view.View","package":"com.fixture.merchant.debug","clickable":true}]}}
`,
	)
	byScreen := map[string]*Counts{}
	if err := accumulate(trace, byScreen, ""); err != nil {
		t.Fatal(err)
	}
	counts := byScreen["ledger"]
	if counts.Interactive != 1 {
		t.Fatalf(
			"interactive: want 1 after dropping system UI, got %d",
			counts.Interactive,
		)
	}
	if counts.ByIdentifier != 0 {
		t.Fatalf(
			"system identifiers leaked into the app's score: %d",
			counts.ByIdentifier,
		)
	}
	if got := counts.stableShare(); got != 0 {
		t.Fatalf("stable share: want 0, got %.1f", got)
	}
}

func TestAccumulatePackageFlagPinsExactly(t *testing.T) {
	trace := writeTrace(
		t,
		`{"step":0,"screen":"ledger","hierarchy":{"elements":[
{"resourceId":"other:id/x","package":"com.other.app","clickable":true},
{"class":"android.view.View","package":"com.fixture.merchant.debug","clickable":true}]}}
`,
	)
	byScreen := map[string]*Counts{}
	if err := accumulate(trace, byScreen, "com.fixture.merchant.debug"); err != nil {
		t.Fatal(err)
	}
	if got := byScreen["ledger"].Interactive; got != 1 {
		t.Fatalf("interactive: want 1, got %d", got)
	}
}

// Only window roots carry a package attribute in a hierarchy dump. Treating an
// unstamped node as foreign discards the application and reports a clean zero.
func TestScopedKeepsUnstampedNodes(t *testing.T) {
	elements := []element{
		{
			Package:    "com.android.systemui",
			ResourceID: "sysui:id/clock",
			Clickable:  true,
		},
		{Package: "com.fixture.merchant.debug", ResourceID: "app:id/root"},
		{Class: "android.view.View", Clickable: true},
	}
	if got := len(scoped(elements, "")); got != 2 {
		t.Fatalf("denylist mode: want 2 kept, got %d", got)
	}
	if got := len(scoped(elements, "com.fixture.merchant.debug")); got != 2 {
		t.Fatalf("pinned mode: want 2 kept, got %d", got)
	}
	if got := len(scoped(elements, "com.other.app")); got != 1 {
		t.Fatalf(
			"pinned to a foreign package: want 1 unstamped node kept, got %d",
			got,
		)
	}
}

// An avatar renders the first letter of the name beside it, so a one-character
// description is the name in disguise. Four of these were scored as durable on
// a real ledger before the rule existed.
func TestSingleCharacterDescriptionsAreAvatarInitials(t *testing.T) {
	for _, initial := range []string{"R", "C", "T", "A", "9", " R "} {
		if !volatile(initial) {
			t.Errorf(
				"%q is an avatar initial and changes when the record is renamed",
				initial,
			)
		}
	}
	for _, keypad := range []string{"+", "=", "AC"} {
		if volatile(keypad) {
			t.Errorf(
				"%q is a fixed control label and should count as durable",
				keypad,
			)
		}
	}
}

// A row tagged with its record's uuid names its role durably and its instance
// not at all, so an exact match on it survives exactly one run.
func TestDataCarryingIdentifiers(t *testing.T) {
	for _, identifier := range []string{
		"customer_row_5f338c10-feef-411c-a070-8999b4890a62",
		"com.fixture.merchant.debug:id/customer_row_5f338c10-feef-411c-a070-8999b4890a62",
		"txn_20260812",
	} {
		if !dataCarryingID(identifier) {
			t.Errorf("%q embeds the record it names", identifier)
		}
	}
	for _, identifier := range []string{
		"customer_supplier_list",
		"summary_card",
		"com.fixture.merchant.debug:id/buttonLogin",
		"button2",
		"add_relationship",
	} {
		if dataCarryingID(identifier) {
			t.Errorf("%q names a role and should count as durable", identifier)
		}
	}
}

func TestMeasureSeparatesDataCarryingIdentifiers(t *testing.T) {
	counts := measure([]element{
		{Class: "View", ResourceID: "summary_card", Clickable: true},
		{
			Class:      "View",
			ResourceID: "customer_row_5f338c10-feef-411c-a070-8999b4890a62",
			Clickable:  true,
		},
		{
			Class:      "View",
			ResourceID: "customer_row_8ea26690-f809-4d23-8560-9cca6d1a5bcd",
			Clickable:  true,
		},
	})
	if counts.ByIdentifier != 1 {
		t.Errorf("durable identifiers: want 1, got %d", counts.ByIdentifier)
	}
	if counts.ByDataID != 2 {
		t.Errorf("data-carrying identifiers: want 2, got %d", counts.ByDataID)
	}
	if got := counts.stableShare(); got < 33 || got > 34 {
		t.Errorf("stable share: want about 33.3, got %.1f", got)
	}
	if len(counts.Needing) != 2 {
		t.Fatalf(
			"both row identifiers belong on the work list, got %v",
			counts.Needing,
		)
	}
}

// A calculator key and an avatar initial are both one character. Only the
// screen they sit on tells them apart, so the keypad rule needs that context.
func TestKeypadDigitsAreDurableButAvatarInitialsAreNot(t *testing.T) {
	keypad := make([]element, 0, 10)
	for _, key := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "0"} {
		keypad = append(
			keypad,
			element{Class: "Button", Description: key, Clickable: true},
		)
	}
	counts := measure(keypad)
	if counts.ByDescription != 10 {
		t.Fatalf(
			"keypad keys are fixed labels: want 10 durable, got %d",
			counts.ByDescription,
		)
	}

	counts = measure([]element{
		{Class: "Button", Description: "R", Clickable: true},
		{Class: "Button", Description: "9", Clickable: true},
		{Class: "Button", Description: "Filter", Clickable: true},
	})
	if counts.ByVolatile != 2 {
		t.Fatalf(
			"avatar initials carry data: want 2 volatile, got %d",
			counts.ByVolatile,
		)
	}
	if counts.ByDescription != 1 {
		t.Fatalf("durable descriptions: want 1, got %d", counts.ByDescription)
	}
}
