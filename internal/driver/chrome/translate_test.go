package chrome

import "testing"

func TestTranslateStringSelector_KnownKeys(t *testing.T) {
	cases := []struct {
		selector string
		want     string
		isXPath  bool
	}{
		{"id:email", `[id="email"]`, false},
		{"resource-id:account-name", `[id="account-name"]`, false},
		{"class:btn-primary", `.btn-primary`, false},
		{"tag:button", `button`, false},
		{"text:Sign in", `//*[normalize-space(text())="Sign in"]`, true},
		{"desc:logout", `[aria-label="logout"]`, false},
		{"label:logout", `[aria-label="logout"]`, false},
		{"accessibilityLabel:logout", `[aria-label="logout"]`, false},
		{"aria-label:Sign in", `[aria-label="Sign in"]`, false},
		{"descPrefix:account:", `[aria-label^="account:"]`, false},
		{"testTag:submit", `[data-testid="submit"]`, false},
		{"testID:submit", `[data-testid="submit"]`, false},
		{"placeholder:Email", `[placeholder="Email"]`, false},
	}
	for _, testCase := range cases {
		got, isXPath, err := TranslateStringSelector(testCase.selector)
		if err != nil {
			t.Errorf("%q: unexpected error %v", testCase.selector, err)
			continue
		}
		if got != testCase.want || isXPath != testCase.isXPath {
			t.Errorf("%q: got (%q, xpath=%v), want (%q, xpath=%v)",
				testCase.selector, got, isXPath, testCase.want, testCase.isXPath)
		}
	}
}

func TestTranslateStringSelector_UnknownPrefixPassesThrough(t *testing.T) {
	got, _, err := TranslateStringSelector("role:button")
	if err != nil {
		t.Fatal(err)
	}
	if got != `[role="button"]` {
		t.Errorf("unknown prefix should map to attribute selector, got %q", got)
	}
}

func TestTranslateStringSelector_EscapesQuotes(t *testing.T) {
	got, _, err := TranslateStringSelector(`label:say "hi"`)
	if err != nil {
		t.Fatal(err)
	}
	if got != `[aria-label="say \"hi\""]` {
		t.Errorf("expected escaped quotes, got %q", got)
	}
}

func TestTranslateStringSelector_RejectsMissingPrefix(t *testing.T) {
	if _, _, err := TranslateStringSelector("foo"); err == nil {
		t.Error("expected error for missing prefix")
	}
	if _, _, err := TranslateStringSelector(""); err == nil {
		t.Error("expected error for empty selector")
	}
}
