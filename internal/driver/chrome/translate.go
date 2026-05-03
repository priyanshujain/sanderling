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
// attribute match by the same name so a Maestro-side addition (e.g. a future
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
	case "class":
		return `[class~="` + cssEscape(value) + `"]`, false, nil
	case "tag":
		return cssEscape(value), false, nil
	case "text":
		return `//*[normalize-space(text())="` + xpathEscape(value) + `"]`, true, nil
	case "desc", "label", "content-desc", "accessibilityLabel", "accessibilityText", "ariaLabel", "aria-label":
		return `[aria-label="` + cssEscape(value) + `"]`, false, nil
	case "descPrefix":
		return `[aria-label^="` + cssEscape(value) + `"]`, false, nil
	case "testTag", "testID", "testid", "data-testid":
		return `[data-testid="` + cssEscape(value) + `"]`, false, nil
	case "placeholder", "placeholderValue", "hintText":
		return `[placeholder="` + cssEscape(value) + `"]`, false, nil
	default:
		if !attrNamePattern.MatchString(kind) {
			return "", false, fmt.Errorf("unsafe selector prefix %q", kind)
		}
		return `[` + kind + `="` + cssEscape(value) + `"]`, false, nil
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
// tag names) — use an attribute selector form (`[class~="..."]`) instead.
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

func xpathEscape(value string) string {
	return strings.ReplaceAll(value, `"`, `\"`)
}
