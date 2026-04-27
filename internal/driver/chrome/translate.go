package chrome

import (
	"errors"
	"strings"
)

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
		return "." + cssEscape(value), false, nil
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
		return `[` + kind + `="` + cssEscape(value) + `"]`, false, nil
	}
}

// cssEscape escapes the subset of characters that break a CSS string literal
// inside `[attr="..."]`. Quotes and backslashes are escaped per CSSOM's
// CSS.escape rules; other printable bytes pass through.
func cssEscape(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for index := 0; index < len(value); index++ {
		c := value[index]
		switch c {
		case '\\', '"':
			builder.WriteByte('\\')
			builder.WriteByte(c)
		default:
			builder.WriteByte(c)
		}
	}
	return builder.String()
}

func xpathEscape(value string) string {
	return strings.ReplaceAll(value, `"`, `\"`)
}
