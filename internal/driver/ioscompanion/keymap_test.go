package ioscompanion

import (
	"reflect"
	"testing"
)

func TestCharKeyLetters(t *testing.T) {
	cases := []struct {
		r     rune
		usage uint32
		shift bool
	}{
		{'a', usageA, false},
		{'z', usageA + 25, false},
		{'m', usageA + 12, false},
		{'A', usageA, true},
		{'Z', usageA + 25, true},
		{'M', usageA + 12, true},
	}
	for _, c := range cases {
		press, ok := charKey(c.r)
		if !ok {
			t.Fatalf("charKey(%q) reported unmappable", c.r)
		}
		if press.Usage != c.usage || press.Shift != c.shift {
			t.Errorf("charKey(%q) = {%d,%v}, want {%d,%v}", c.r, press.Usage, press.Shift, c.usage, c.shift)
		}
	}
}

func TestCharKeyDigits(t *testing.T) {
	cases := []struct {
		r     rune
		usage uint32
	}{
		{'1', usage1},
		{'2', usage1 + 1},
		{'3', usage1 + 2},
		{'4', usage1 + 3},
		{'5', usage1 + 4},
		{'6', usage1 + 5},
		{'7', usage1 + 6},
		{'8', usage1 + 7},
		{'9', usage1 + 8},
		{'0', usage0},
	}
	for _, c := range cases {
		press, ok := charKey(c.r)
		if !ok {
			t.Fatalf("charKey(%q) reported unmappable", c.r)
		}
		if press.Usage != c.usage || press.Shift {
			t.Errorf("charKey(%q) = {%d,%v}, want {%d,false}", c.r, press.Usage, press.Shift, c.usage)
		}
	}
}

func TestCharKeyWhitespace(t *testing.T) {
	cases := []struct {
		r     rune
		usage uint32
	}{
		{' ', usageSpace},
		{'\n', usageReturn},
		{'\t', usageTab},
	}
	for _, c := range cases {
		press, ok := charKey(c.r)
		if !ok {
			t.Fatalf("charKey(%q) reported unmappable", c.r)
		}
		if press.Usage != c.usage || press.Shift {
			t.Errorf("charKey(%q) = {%d,%v}, want {%d,false}", c.r, press.Usage, press.Shift, c.usage)
		}
	}
}

func TestCharKeyUnshiftedSymbols(t *testing.T) {
	cases := []struct {
		r     rune
		usage uint32
	}{
		{'-', usageMinus},
		{'=', usageEqual},
		{'[', usageLeftBracket},
		{']', usageRightBracket},
		{'\\', usageBackslash},
		{';', usageSemicolon},
		{'\'', usageApostrophe},
		{'`', usageGrave},
		{',', usageComma},
		{'.', usagePeriod},
		{'/', usageSlash},
	}
	for _, c := range cases {
		press, ok := charKey(c.r)
		if !ok {
			t.Fatalf("charKey(%q) reported unmappable", c.r)
		}
		if press.Usage != c.usage || press.Shift {
			t.Errorf("charKey(%q) = {%d,%v}, want {%d,false}", c.r, press.Usage, press.Shift, c.usage)
		}
	}
}

func TestCharKeyShiftedSymbols(t *testing.T) {
	cases := []struct {
		r     rune
		usage uint32
	}{
		{'!', usage1},
		{'@', usage1 + 1},
		{'#', usage1 + 2},
		{'$', usage1 + 3},
		{'%', usage1 + 4},
		{'^', usage1 + 5},
		{'&', usage1 + 6},
		{'*', usage1 + 7},
		{'(', usage1 + 8},
		{')', usage0},
		{'_', usageMinus},
		{'+', usageEqual},
		{'{', usageLeftBracket},
		{'}', usageRightBracket},
		{'|', usageBackslash},
		{':', usageSemicolon},
		{'"', usageApostrophe},
		{'~', usageGrave},
		{'<', usageComma},
		{'>', usagePeriod},
		{'?', usageSlash},
	}
	for _, c := range cases {
		press, ok := charKey(c.r)
		if !ok {
			t.Fatalf("charKey(%q) reported unmappable", c.r)
		}
		if press.Usage != c.usage || !press.Shift {
			t.Errorf("charKey(%q) = {%d,%v}, want {%d,true}", c.r, press.Usage, press.Shift, c.usage)
		}
	}
}

func TestCharKeyUnmappable(t *testing.T) {
	for _, r := range []rune{'é', '世', '🙂', '\x00'} {
		if press, ok := charKey(r); ok {
			t.Errorf("charKey(%q) = {%d,%v}, want unmappable", r, press.Usage, press.Shift)
		}
	}
}

func TestTypeString(t *testing.T) {
	presses, skipped := typeString("Ab1!")
	want := []KeyPress{
		{Usage: usageA, Shift: true},
		{Usage: usageA + 1},
		{Usage: usage1},
		{Usage: usage1, Shift: true},
	}
	if !reflect.DeepEqual(presses, want) {
		t.Errorf("presses = %+v, want %+v", presses, want)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none", skipped)
	}
}

func TestTypeStringSkipsUnmappableInOrder(t *testing.T) {
	presses, skipped := typeString("café 🙂x")
	wantPresses := []KeyPress{
		{Usage: usageA + 2},
		{Usage: usageA},
		{Usage: usageA + 5},
		{Usage: usageSpace},
		{Usage: usageA + 23},
	}
	if !reflect.DeepEqual(presses, wantPresses) {
		t.Errorf("presses = %+v, want %+v", presses, wantPresses)
	}
	wantSkipped := []rune{'é', '🙂'}
	if !reflect.DeepEqual(skipped, wantSkipped) {
		t.Errorf("skipped = %q, want %q", skipped, wantSkipped)
	}
}

func TestBackspaces(t *testing.T) {
	if got := backspaces(0); len(got) != 0 {
		t.Errorf("backspaces(0) = %v, want empty", got)
	}
	got := backspaces(3)
	want := []KeyPress{
		{Usage: usageBackspace},
		{Usage: usageBackspace},
		{Usage: usageBackspace},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("backspaces(3) = %+v, want %+v", got, want)
	}
}

func TestPasteChordConstants(t *testing.T) {
	if LeftGUI != 227 {
		t.Errorf("LeftGUI = %d, want 227", LeftGUI)
	}
	if VKey != 25 {
		t.Errorf("VKey = %d, want 25", VKey)
	}
}
