// Package ioscompanion talks to the simulator companion to drive an iOS
// simulator. This file is pure data: it maps runes to USB HID keyboard
// usage IDs. A later module turns these key presses into HID events.
package ioscompanion

// USB HID keyboard usage IDs.
const (
	usageA            = 4
	usage1            = 30
	usage0            = 39
	usageReturn       = 40
	usageEscape       = 41
	usageTab          = 43
	usageSpace        = 44
	usageBackspace    = 42
	usageMinus        = 45
	usageEqual        = 46
	usageLeftBracket  = 47
	usageRightBracket = 48
	usageBackslash    = 49
	usageSemicolon    = 51
	usageApostrophe   = 52
	usageGrave        = 53
	usageComma        = 54
	usagePeriod       = 55
	usageSlash        = 56

	usageLeftShift = 225

	// LeftGUI is the command key. VKey is the letter V. Together they form
	// the paste chord (command+V).
	LeftGUI = 227
	VKey    = 25
)

// KeyPress is a single key with whether the shift modifier is held.
type KeyPress struct {
	Usage uint32
	Shift bool
}

// shiftedSymbols maps a shifted-symbol rune to the unshifted key it lives on.
var shiftedSymbols = map[rune]uint32{
	'!': usage1,
	'@': usage1 + 1,
	'#': usage1 + 2,
	'$': usage1 + 3,
	'%': usage1 + 4,
	'^': usage1 + 5,
	'&': usage1 + 6,
	'*': usage1 + 7,
	'(': usage1 + 8,
	')': usage0,
	'_': usageMinus,
	'+': usageEqual,
	'{': usageLeftBracket,
	'}': usageRightBracket,
	'|': usageBackslash,
	':': usageSemicolon,
	'"': usageApostrophe,
	'~': usageGrave,
	'<': usageComma,
	'>': usagePeriod,
	'?': usageSlash,
}

// unshiftedSymbols maps a symbol rune typed without shift to its key.
var unshiftedSymbols = map[rune]uint32{
	'-':  usageMinus,
	'=':  usageEqual,
	'[':  usageLeftBracket,
	']':  usageRightBracket,
	'\\': usageBackslash,
	';':  usageSemicolon,
	'\'': usageApostrophe,
	'`':  usageGrave,
	',':  usageComma,
	'.':  usagePeriod,
	'/':  usageSlash,
}

// charKey returns the key press for a rune and whether the rune is mappable.
func charKey(r rune) (KeyPress, bool) {
	switch {
	case r >= 'a' && r <= 'z':
		return KeyPress{Usage: uint32(usageA + (r - 'a'))}, true
	case r >= 'A' && r <= 'Z':
		return KeyPress{Usage: uint32(usageA + (r - 'A')), Shift: true}, true
	case r >= '1' && r <= '9':
		return KeyPress{Usage: uint32(usage1 + (r - '1'))}, true
	case r == '0':
		return KeyPress{Usage: usage0}, true
	case r == ' ':
		return KeyPress{Usage: usageSpace}, true
	case r == '\n':
		return KeyPress{Usage: usageReturn}, true
	case r == '\t':
		return KeyPress{Usage: usageTab}, true
	}
	if usage, ok := unshiftedSymbols[r]; ok {
		return KeyPress{Usage: usage}, true
	}
	if usage, ok := shiftedSymbols[r]; ok {
		return KeyPress{Usage: usage, Shift: true}, true
	}
	return KeyPress{}, false
}

// typeString returns the ordered key presses for every mappable rune in s.
// Unmappable runes are collected in order in skipped.
func typeString(s string) (presses []KeyPress, skipped []rune) {
	for _, r := range s {
		press, ok := charKey(r)
		if !ok {
			skipped = append(skipped, r)
			continue
		}
		presses = append(presses, press)
	}
	return presses, skipped
}

// backspaces returns n backspace key presses.
func backspaces(n int) []KeyPress {
	presses := make([]KeyPress, 0, n)
	for i := 0; i < n; i++ {
		presses = append(presses, KeyPress{Usage: usageBackspace})
	}
	return presses
}
