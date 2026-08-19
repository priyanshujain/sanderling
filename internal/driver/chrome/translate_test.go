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
		{"class:btn-primary", `[class~="btn-primary"]`, false},
		{"tag:button", `button`, false},
		{
			"text:Sign in",
			`//*[contains(normalize-space(.), "Sign in") and not(.//*[contains(normalize-space(.), "Sign in")])]`,
			true,
		},
		{
			`text:Say "hi"`,
			`//*[contains(normalize-space(.), 'Say "hi"') and not(.//*[contains(normalize-space(.), 'Say "hi"')])]`,
			true,
		},
		{
			`text:it's`,
			`//*[contains(normalize-space(.), "it's") and not(.//*[contains(normalize-space(.), "it's")])]`,
			true,
		},
		{
			`text:it's "fine"`,
			`//*[contains(normalize-space(.), concat("it's ", '"', "fine", '"', "")) and not(.//*[contains(normalize-space(.), concat("it's ", '"', "fine", '"', ""))])]`,
			true,
		},
		// desc also accepts an iOS merged label, the way internal/hierarchy does.
		{"desc:logout", `:is([aria-label="logout"], [aria-label^="logout, "])`, false},
		{"label:logout", `[aria-label="logout"]`, false},
		{"accessibilityLabel:logout", `[aria-label="logout"]`, false},
		{"aria-label:Sign in", `[aria-label="Sign in"]`, false},
		{"descPrefix:account:", `[aria-label^="account:"]`, false},
		// Must stay the string pkg/spec/src/web-runtime.ts builds for the same
		// selector; the two translators feed the same page.
		{"idPrefix:customer_row_", `[id^="customer_row_"]`, false},
		{"testTag:submit", `:is([data-testid="submit"], [id="submit"])`, false},
		{"testID:submit", `[data-testid="submit"]`, false},
		{"placeholder:Email", `[placeholder="Email"]`, false},
		// hintText and placeholderValue name the accessible-name ladder, which
		// no CSS says, so they reach nothing here rather than the field whose
		// placeholder happens to carry the value and whose hint is its
		// aria-label. Both matchers name that field by its aria-label alone, so
		// a tap by placeholder acts on an element nobody selected.
		{"hintText:Email", `[hintText*="Email"]`, false},
		{"placeholderValue:Email", `[placeholderValue*="Email"]`, false},
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
	if got != `[role*="button"]` {
		t.Errorf(
			"unknown prefix should map to a substring attribute selector, got %q",
			got,
		)
	}
	got, _, err = TranslateStringSelector("aria-expanded:true")
	if err != nil {
		t.Fatal(err)
	}
	if got != `[aria-expanded="true"]` {
		t.Errorf(
			"a boolean value should map to an exact attribute selector, got %q",
			got,
		)
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

func TestTranslateStringSelector_RejectsUnsafePrefix(t *testing.T) {
	cases := []string{
		`foo]:has(*),body[x:value`,
		`x y:value`,
		`x"y:value`,
		`*:value`,
		`(:value`,
	}
	for _, selector := range cases {
		if _, _, err := TranslateStringSelector(selector); err == nil {
			t.Errorf("%q: expected error for unsafe prefix", selector)
		}
	}
}

func TestCSSEscape_ControlCharactersAndNUL(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"plain ascii", "hello", "hello"},
		{"double quote", `say "hi"`, `say \"hi\"`},
		{"backslash", `a\b`, `a\\b`},
		{"newline", "a\nb", `a\A b`},
		{"carriage return", "a\rb", `a\D b`},
		{"form feed", "a\fb", `a\C b`},
		{"NUL replaced", "a\x00b", "a�b"},
		{"DEL", "a\x7Fb", `a\7F b`},
		{"non-ascii passes through", "café", "café"},
	}
	for _, testCase := range cases {
		got := cssEscape(testCase.input)
		if got != testCase.want {
			t.Errorf("%s: cssEscape(%q) = %q, want %q",
				testCase.name, testCase.input, got, testCase.want)
		}
	}
}
