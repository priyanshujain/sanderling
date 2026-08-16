package chrome

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// attrNamePattern matches HTML attribute names that are safe to drop into a
// CSS attribute selector without escaping. This avoids selectors like
// `foo]:has(*),body[x="..."]` that would escape the intended match.
var attrNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)

// TranslateStringSelector converts a legacy string selector ("id:foo",
// "descPrefix:bar") into a CSS selector or XPath expression usable from the
// chrome driver's TapSelector fallback path. The boolean return is true when
// the result is XPath rather than CSS. Unknown prefixes pass through to a CSS
// attribute match by the same name so a sidecar-side addition (e.g. a future
// "role:") works without a Sanderling release.
func TranslateStringSelector(selector string) (string, bool, error) {
	if selector == "" {
		return "", false, errors.New("empty selector")
	}
	colon := strings.IndexByte(selector, ':')
	if colon <= 0 {
		return "", false, errors.New("selector missing prefix (expected `kind:value`)")
	}
	kind := selector[:colon]
	value := selector[colon+1:]
	switch kind {
	case "id", "resource-id":
		return `[id="` + cssEscape(value) + `"]`, false, nil
	case "idPrefix":
		return `[id^="` + cssEscape(value) + `"]`, false, nil
	case "class":
		return `[class~="` + cssEscape(value) + `"]`, false, nil
	case "tag":
		return cssEscape(value), false, nil
	case "text":
		// Substring of the element's whole text, the way internal/hierarchy
		// reads the same selector: an element reading "Sent ✓" answers to
		// text:Sent on every platform, and one React wrote as `{count} unsent`
		// answers to text:unsent though its text arrives as two text nodes.
		// normalize-space(text()) would read only the first of them. The
		// not() clause is what keeps the badge's ancestors, up to <html>, from
		// answering for it.
		return `//*[` + innermostTextPredicate(value) + `]`, true, nil
	case "desc":
		// Mirrors the native rule: the label itself, or the label at the head of
		// an iOS merged label ("account_card:7, Tim, $100").
		escaped := cssEscape(value)
		return `:is([aria-label="` + escaped + `"], [aria-label^="` + escaped + `, "])`, false, nil
	case "label", "content-desc", "accessibilityLabel", "accessibilityText", "ariaLabel", "aria-label":
		return `[aria-label="` + cssEscape(value) + `"]`, false, nil
	case "descPrefix":
		return `[aria-label^="` + cssEscape(value) + `"]`, false, nil
	case "testTag":
		// Mirrors the in-page table and the native resource-id alias: a
		// testTag reaches the DOM as data-testid or as an id, depending on
		// the toolkit. `:is()` keeps this one compound selector.
		escaped := cssEscape(value)
		return `:is([data-testid="` + escaped + `"], [id="` + escaped + `"])`, false, nil
	case "testID", "testid", "data-testid":
		return `[data-testid="` + cssEscape(value) + `"]`, false, nil
	case "placeholder", "placeholderValue", "hintText":
		return `[placeholder="` + cssEscape(value) + `"]`, false, nil
	default:
		if !attrNamePattern.MatchString(kind) {
			return "", false, fmt.Errorf("unsafe selector prefix %q", kind)
		}
		operator := `*=`
		if value == "true" || value == "false" {
			operator = `=`
		}
		return `[` + kind + operator + `"` + cssEscape(value) + `"]`, false, nil
	}
}

// cssEscape escapes a value for use inside a CSS double-quoted string
// (`[attr="VALUE"]`). Per the CSSOM spec for serializing strings:
//   - U+0000 becomes U+FFFD (REPLACEMENT CHARACTER)
//   - control characters (U+0001-U+001F, U+007F) become \HEX escapes
//   - " and \ are escaped with a leading backslash
//   - everything else passes through, including non-ASCII
//
// Callers should not pass this output into identifier contexts (class names,
// tag names). Use an attribute selector form (`[class~="..."]`) instead.
func cssEscape(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		switch {
		case r == 0:
			builder.WriteRune(utf8.RuneError)
		case (r >= 0x01 && r <= 0x1F) || r == 0x7F:
			fmt.Fprintf(&builder, "\\%X ", r)
		case r == '\\' || r == '"':
			builder.WriteByte('\\')
			builder.WriteRune(r)
		default:
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

// innermostTextPredicate matches an element whose text contains value and whose
// descendants do not, which is the innermost match internal/hierarchy resolves
// the same selector to. The same predicate appears in
// pkg/spec/src/web-runtime.ts.
func innermostTextPredicate(value string) string {
	contains := `contains(normalize-space(.), ` + xpathStringLiteral(
		value,
	) + `)`
	return contains + ` and not(.//*[` + contains + `])`
}

// xpathStringLiteral wraps the value in a valid XPath 1.0 string literal.
// XPath 1.0 has no escape syntax, so a value containing both ' and " must be
// composed via concat(). The output already includes the surrounding quotes
// (or concat() call), so callers don't quote it again.
func xpathStringLiteral(value string) string {
	if !strings.ContainsRune(value, '"') {
		return `"` + value + `"`
	}
	if !strings.ContainsRune(value, '\'') {
		return `'` + value + `'`
	}
	parts := strings.Split(value, `"`)
	var builder strings.Builder
	builder.WriteString(`concat(`)
	for index, part := range parts {
		if index > 0 {
			builder.WriteString(`, '"', `)
		}
		builder.WriteByte('"')
		builder.WriteString(part)
		builder.WriteByte('"')
	}
	builder.WriteByte(')')
	return builder.String()
}
