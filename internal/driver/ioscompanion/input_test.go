package ioscompanion

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/priyanshujain/sanderling/internal/driver/ioscompanion/transport"
)

func eventsEqual(t *testing.T, got, want []transport.HIDEvent) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("event count: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Fatalf("event %d differs", i)
		}
	}
}

func TestKeyPressEvents(t *testing.T) {
	tests := []struct {
		name    string
		presses []KeyPress
		want    []transport.HIDEvent
	}{
		{
			name:    "lowercase letter is down then up, no shift",
			presses: []KeyPress{{Usage: usageA}},
			want:    []transport.HIDEvent{transport.KeyDown(usageA), transport.KeyUp(usageA)},
		},
		{
			name:    "shifted letter wraps with left shift down and up",
			presses: []KeyPress{{Usage: usageA, Shift: true}},
			want: []transport.HIDEvent{
				transport.KeyDown(usageLeftShift),
				transport.KeyDown(usageA),
				transport.KeyUp(usageA),
				transport.KeyUp(usageLeftShift),
			},
		},
		{
			name:    "empty input yields no events",
			presses: nil,
			want:    []transport.HIDEvent{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			eventsEqual(t, keyPressEvents(test.presses), test.want)
		})
	}
}

func TestKeyPressEventsMixedString(t *testing.T) {
	presses, skipped := typeString("aB")
	if len(skipped) != 0 {
		t.Fatalf("unexpected skipped runes: %v", skipped)
	}
	got := keyPressEvents(presses)
	want := []transport.HIDEvent{
		transport.KeyDown(usageA), transport.KeyUp(usageA),
		transport.KeyDown(usageLeftShift),
		transport.KeyDown(usageA + 1),
		transport.KeyUp(usageA + 1),
		transport.KeyUp(usageLeftShift),
	}
	eventsEqual(t, got, want)
}

func TestPasteChordEvents(t *testing.T) {
	want := []transport.HIDEvent{
		transport.KeyDown(LeftGUI),
		transport.KeyDown(VKey),
		transport.KeyUp(VKey),
		transport.KeyUp(LeftGUI),
	}
	eventsEqual(t, pasteChordEvents(), want)
}

func TestTapEvents(t *testing.T) {
	want := []transport.HIDEvent{transport.TouchDown(12, 34), transport.TouchUp(12, 34)}
	eventsEqual(t, tapEvents(12, 34), want)
}

func TestDoubleTapEvents(t *testing.T) {
	want := []transport.HIDEvent{
		transport.TouchDown(5, 6), transport.TouchUp(5, 6),
		transport.Delay(70),
		transport.TouchDown(5, 6), transport.TouchUp(5, 6),
	}
	eventsEqual(t, doubleTapEvents(5, 6, DefaultDoubleTapGapMilliseconds), want)
}

func TestLongPressEvents(t *testing.T) {
	want := []transport.HIDEvent{
		transport.TouchDown(8, 9),
		transport.Delay(500),
		transport.TouchUp(8, 9),
	}
	eventsEqual(t, longPressEvents(8, 9, 500), want)
}

func loadDialogDump(t *testing.T) []byte {
	t.Helper()
	dump, err := os.ReadFile(filepath.Join("testdata", "paste-dialog.json"))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	return dump
}

func TestFindAllowPasteButton(t *testing.T) {
	tests := []struct {
		name      string
		dump      string
		wantFound bool
		wantX     float64
		wantY     float64
	}{
		{
			name:      "exact Allow Paste wins over Don't Allow Paste",
			dump:      string(loadDialogDump(t)),
			wantFound: true,
			wantX:     280, // 210 + 140/2
			wantY:     465, // 440 + 50/2
		},
		{
			name:      "plain Allow label matches",
			dump:      `[{"type":"Button","AXLabel":"Allow","frame":{"x":100,"y":100,"width":40,"height":20},"enabled":true}]`,
			wantFound: true,
			wantX:     120,
			wantY:     110,
		},
		{
			name:      "sole enabled button fallback",
			dump:      `[{"type":"StaticText","AXLabel":"Paste?","frame":{"x":0,"y":0,"width":10,"height":10},"enabled":true},{"type":"Button","AXLabel":"OK","frame":{"x":50,"y":60,"width":100,"height":40},"enabled":true}]`,
			wantFound: true,
			wantX:     100,
			wantY:     80,
		},
		{
			name:      "no fallback when several enabled buttons present",
			dump:      `[{"type":"Button","AXLabel":"A","frame":{"x":0,"y":0,"width":10,"height":10},"enabled":true},{"type":"Button","AXLabel":"B","frame":{"x":20,"y":0,"width":10,"height":10},"enabled":true}]`,
			wantFound: false,
		},
		{
			name:      "reject button alone does not match",
			dump:      `[{"type":"Button","AXLabel":"Don’t Allow Paste","frame":{"x":0,"y":0,"width":10,"height":10},"enabled":true}]`,
			wantFound: false,
		},
		{
			name:      "empty dump finds nothing",
			dump:      ``,
			wantFound: false,
		},
		{
			name:      "malformed json finds nothing",
			dump:      `not json`,
			wantFound: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			button, found := findAllowPasteButton([]byte(test.dump))
			if found != test.wantFound {
				t.Fatalf("found: got %v, want %v", found, test.wantFound)
			}
			if !found {
				return
			}
			if button.centerX != test.wantX || button.centerY != test.wantY {
				t.Fatalf("center: got (%v,%v), want (%v,%v)", button.centerX, button.centerY, test.wantX, test.wantY)
			}
		})
	}
}

func TestPasteLanded(t *testing.T) {
	dump := `[{"type":"TextField","AXUniqueId":"NoteField","AXValue":"prefix Café ☕","frame":{"x":0,"y":0,"width":10,"height":10}}]`
	tests := []struct {
		name       string
		dump       string
		identifier string
		expected   string
		want       bool
	}{
		{name: "substring present", dump: dump, identifier: "NoteField", expected: "Café ☕", want: true},
		{name: "value not yet landed", dump: `[{"type":"TextField","AXUniqueId":"NoteField","AXValue":"prefix"}]`, identifier: "NoteField", expected: "Café", want: false},
		{name: "field absent", dump: dump, identifier: "MissingField", expected: "Café", want: false},
		{name: "empty identifier never matches", dump: dump, identifier: "", expected: "Café", want: false},
		{name: "empty expected never matches", dump: dump, identifier: "NoteField", expected: "", want: false},
		{name: "malformed dump never matches", dump: `nope`, identifier: "NoteField", expected: "Café", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := pasteLanded([]byte(test.dump), test.identifier, test.expected); got != test.want {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
}

// fakeRunner records side effects and serves scripted describe-all dumps.
type fakeRunner struct {
	pasteboard  string
	hidStreams  [][]transport.HIDEvent
	dumps       [][]byte
	dumpIndex   int
	setError    error
	sendError   error
	describeErr error
	sleepCount  int
}

func (f *fakeRunner) setPasteboard(ctx context.Context, text string) error {
	if f.setError != nil {
		return f.setError
	}
	f.pasteboard = text
	return nil
}

func (f *fakeRunner) sendHID(ctx context.Context, events ...transport.HIDEvent) error {
	if f.sendError != nil {
		return f.sendError
	}
	f.hidStreams = append(f.hidStreams, events)
	return nil
}

func (f *fakeRunner) describeAll(ctx context.Context) ([]byte, error) {
	if f.describeErr != nil {
		return nil, f.describeErr
	}
	if f.dumpIndex >= len(f.dumps) {
		return f.dumps[len(f.dumps)-1], nil
	}
	dump := f.dumps[f.dumpIndex]
	f.dumpIndex++
	return dump, nil
}

func (f *fakeRunner) sleep(ctx context.Context, duration time.Duration) error {
	f.sleepCount++
	return ctx.Err()
}

func TestInputTextFastPathUsesHardwareKeyboard(t *testing.T) {
	fake := &fakeRunner{}
	if err := inputText(context.Background(), fake, "hi", fieldTarget{}); err != nil {
		t.Fatalf("inputText: %v", err)
	}
	if fake.pasteboard != "" {
		t.Fatalf("fast path must not touch pasteboard, got %q", fake.pasteboard)
	}
	if len(fake.hidStreams) != 1 {
		t.Fatalf("fast path must send one HID stream, got %d", len(fake.hidStreams))
	}
	want := keyPressEvents([]KeyPress{{Usage: usageA + ('h' - 'a')}, {Usage: usageA + ('i' - 'a')}})
	eventsEqual(t, fake.hidStreams[0], want)
}

func TestInputTextPasteLandsImmediately(t *testing.T) {
	landed := `[{"type":"TextField","AXUniqueId":"F","AXValue":"Café"}]`
	fake := &fakeRunner{dumps: [][]byte{[]byte(landed)}}
	field := fieldTarget{identifier: "F", centerX: 100, centerY: 200}
	if err := inputText(context.Background(), fake, "Café", field); err != nil {
		t.Fatalf("inputText: %v", err)
	}
	if fake.pasteboard != "Café" {
		t.Fatalf("pasteboard: got %q", fake.pasteboard)
	}
	if len(fake.hidStreams) != 1 {
		t.Fatalf("expected one paste chord, got %d streams", len(fake.hidStreams))
	}
	eventsEqual(t, fake.hidStreams[0], pasteChordEvents())
}

func TestInputTextPasteDismissesDialogThenLands(t *testing.T) {
	dialog := string(loadDialogDump(t))
	landed := `[{"type":"TextField","AXUniqueId":"TxnNoteField","AXValue":"Café ☕ 😀"}]`
	// First check sees the dialog (which swallowed the initial chord); the
	// check after the retried chord sees the landed value.
	fake := &fakeRunner{dumps: [][]byte{[]byte(dialog), []byte(landed)}}
	field := fieldTarget{identifier: "TxnNoteField", centerX: 195, centerY: 222}
	if err := inputText(context.Background(), fake, "Café ☕ 😀", field); err != nil {
		t.Fatalf("inputText: %v", err)
	}
	// Streams: paste chord, tap allow, refocus field, paste chord again.
	if len(fake.hidStreams) != 4 {
		t.Fatalf("expected 4 HID streams, got %d", len(fake.hidStreams))
	}
	eventsEqual(t, fake.hidStreams[0], pasteChordEvents())
	eventsEqual(t, fake.hidStreams[1], tapEvents(280, 465))
	eventsEqual(t, fake.hidStreams[2], tapEvents(195, 222))
	eventsEqual(t, fake.hidStreams[3], pasteChordEvents())
}

func TestInputTextPasteFailsAfterAllAttempts(t *testing.T) {
	stuck := `[{"type":"TextField","AXUniqueId":"F","AXValue":""}]`
	fake := &fakeRunner{dumps: [][]byte{[]byte(stuck)}}
	field := fieldTarget{identifier: "F", centerX: 1, centerY: 2}
	err := inputText(context.Background(), fake, "😀", field)
	if err == nil {
		t.Fatal("expected error after exhausting the verify budget")
	}
	// The chord is sent exactly once: with no dialog on screen, re-sending it
	// on a slow render would paste the text twice.
	if len(fake.hidStreams) != 1 {
		t.Fatalf("expected exactly one paste chord, got %d", len(fake.hidStreams))
	}
	wantPolls := int(pasteVerifyTimeout / pastePoll)
	if fake.sleepCount != wantPolls {
		t.Fatalf("expected %d verify polls, got %d", wantPolls, fake.sleepCount)
	}
}

func TestInputTextPasteLandsAfterBridgeBlackout(t *testing.T) {
	// The field is absent (bridge blacked out by the dialog) for several
	// polls, then the value lands. The loop must keep waiting through the
	// blackout instead of failing.
	blacked := `[{"type":"Application"}]`
	landed := `[{"type":"TextField","AXUniqueId":"F","AXValue":"😀"}]`
	dumps := [][]byte{[]byte(blacked), []byte(blacked), []byte(blacked), []byte(landed)}
	fake := &fakeRunner{dumps: dumps}
	field := fieldTarget{identifier: "F", centerX: 1, centerY: 2}
	if err := inputText(context.Background(), fake, "😀", field); err != nil {
		t.Fatalf("inputText: %v", err)
	}
	// One chord, no dialog seen, success once the value appears.
	if len(fake.hidStreams) != 1 {
		t.Fatalf("expected exactly one paste chord, got %d", len(fake.hidStreams))
	}
}

func TestInputTextPasteUnverifiableFieldSingleChord(t *testing.T) {
	// No field identifier resolved: one chord, no dialog, success after the
	// settle without endless retries.
	noField := `[{"type":"StaticText","AXLabel":"whatever"}]`
	fake := &fakeRunner{dumps: [][]byte{[]byte(noField)}}
	if err := inputText(context.Background(), fake, "😀", fieldTarget{}); err != nil {
		t.Fatalf("inputText: %v", err)
	}
	if len(fake.hidStreams) != 1 {
		t.Fatalf("expected exactly one paste chord, got %d", len(fake.hidStreams))
	}
}

func TestEraseTextLargeCountClearsAtomically(t *testing.T) {
	fake := &fakeRunner{}
	if err := eraseText(context.Background(), fake, 40); err != nil {
		t.Fatalf("eraseText: %v", err)
	}
	if len(fake.hidStreams) != 1 {
		t.Fatalf("expected one stream, got %d", len(fake.hidStreams))
	}
	want := append(selectAllChordEvents(), keyPressEvents(backspaces(1))...)
	eventsEqual(t, fake.hidStreams[0], want)
}

func TestEraseTextSendsBackspaces(t *testing.T) {
	fake := &fakeRunner{}
	if err := eraseText(context.Background(), fake, 3); err != nil {
		t.Fatalf("eraseText: %v", err)
	}
	if len(fake.hidStreams) != 1 {
		t.Fatalf("expected one stream, got %d", len(fake.hidStreams))
	}
	want := keyPressEvents(backspaces(3))
	eventsEqual(t, fake.hidStreams[0], want)
}

func TestEraseTextZeroIsNoOp(t *testing.T) {
	fake := &fakeRunner{}
	if err := eraseText(context.Background(), fake, 0); err != nil {
		t.Fatalf("eraseText: %v", err)
	}
	if len(fake.hidStreams) != 0 {
		t.Fatalf("zero count must send nothing, got %d streams", len(fake.hidStreams))
	}
}

func TestUsesPasteboard(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"", false},
		{"a", false},
		{"abc", false},
		{"a long but fully mappable ascii string", false},
		{"-1", false},
		{"%s%n", false},
		{"😀", true},
		{"Café", true},
	}
	for _, c := range cases {
		if got := usesPasteboard(c.text); got != c.want {
			t.Errorf("usesPasteboard(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestPasteLandedMaskedSecureField(t *testing.T) {
	cases := []struct {
		name string
		dump string
		want bool
	}{
		{name: "all bullets counts as landed", dump: `[{"type":"TextField","AXUniqueId":"PW","AXValue":"•••••"}]`, want: true},
		{name: "empty secure field not landed", dump: `[{"type":"TextField","AXUniqueId":"PW","AXValue":""}]`, want: false},
		{name: "mixed bullets and text not masked", dump: `[{"type":"TextField","AXUniqueId":"PW","AXValue":"••a"}]`, want: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pasteLanded([]byte(c.dump), "PW", "secret9"); got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}
