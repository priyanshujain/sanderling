// Package ioscompanion drives an iOS simulator through the native simulator
// companion. This file composes text input and gestures into HID streams and
// implements the pasteboard fallback for text that the hardware keyboard
// cannot type (anything outside the mappable rune set, such as accented
// letters or emoji).
package ioscompanion

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/priyanshujain/sanderling/internal/driver/ioscompanion/transport"
)

// DefaultDoubleTapGapMilliseconds is a sensible inter-tap gap for a synthesized
// double tap. The driver owns the real default; gesture composers take the gap
// as a parameter so the value stays configurable.
const DefaultDoubleTapGapMilliseconds = 70

// pasteVerifyTimeout bounds the whole paste-and-verify loop. Dismissing the
// permission dialog blacks out the accessibility bridge for around 2.5s (the
// dump collapses to the root element), and the pasted value only becomes
// readable once it recovers, so the budget has to outlast that blackout.
const pasteVerifyTimeout = 8 * time.Second

// pastePoll is the interval between describe-all reads while verifying a paste.
const pastePoll = 250 * time.Millisecond

// dialogSettle is the pause after tapping the dialog's allow button and after
// refocusing the field, before the paste chord is resent.
const dialogSettle = 200 * time.Millisecond

// runner abstracts the simulator-companion side effects InputText needs so the
// decision logic stays testable without a live device. The driver supplies a
// real implementation; tests supply a fake.
type runner interface {
	// setPasteboard places text on the simulator pasteboard.
	setPasteboard(ctx context.Context, text string) error
	// sendHID sends one HID stream to the companion.
	sendHID(ctx context.Context, events ...transport.HIDEvent) error
	// describeAll returns the flat describe-all accessibility dump.
	describeAll(ctx context.Context) ([]byte, error)
	// sleep waits, respecting context cancellation.
	sleep(ctx context.Context, duration time.Duration) error
}

// simctlRunner is the production runner. It shells out to simctl for the
// pasteboard and uses the transport companion for everything else.
type simctlRunner struct {
	companion transport.Companion
	udid      string
}

func (r simctlRunner) setPasteboard(ctx context.Context, text string) error {
	command := exec.CommandContext(ctx, "xcrun", "simctl", "pbcopy", r.udid)
	command.Stdin = strings.NewReader(text)
	return command.Run()
}

func (r simctlRunner) sendHID(ctx context.Context, events ...transport.HIDEvent) error {
	return r.companion.SendHID(ctx, events...)
}

func (r simctlRunner) describeAll(ctx context.Context) ([]byte, error) {
	info, err := r.companion.AccessibilityInfo(ctx)
	if err != nil {
		return nil, err
	}
	return []byte(info), nil
}

func (r simctlRunner) sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// fieldTarget identifies the focused field for the pasteboard path: its
// AXUniqueId (to confirm the paste landed) and its on-screen center (to refocus
// after dismissing the permission dialog).
type fieldTarget struct {
	identifier string
	centerX    float64
	centerY    float64
}

// usesPasteboard reports whether text takes the pasteboard path. Only
// unmappable runes force it: on this OS generation every external pasteboard
// write re-triggers the paste-permission dialog and dismissing it blacks out
// the accessibility bridge for seconds, so the hardware keyboard stays the
// default for everything it can express.
func usesPasteboard(text string) bool {
	_, skipped := typeString(text)
	return len(skipped) > 0
}

// inputText types text into the focused field. Mappable text goes through the
// hardware keyboard in one HID stream; anything else falls back to the
// pasteboard. The field target is only consulted on the pasteboard path.
func inputText(ctx context.Context, run runner, text string, field fieldTarget) error {
	if !usesPasteboard(text) {
		presses, _ := typeString(text)
		return run.sendHID(ctx, keyPressEvents(presses)...)
	}
	return pasteText(ctx, run, text, field)
}

// pasteText copies the full text to the pasteboard, sends the paste chord
// once, and polls until the field reflects the text. The chord is re-sent ONLY
// after dismissing a permission dialog (the dialog swallowed that paste);
// re-sending it on a slow render would paste the text twice. When the field
// cannot be verified (no identifier resolved), one chord plus a settle is the
// best available behavior.
func pasteText(ctx context.Context, run runner, text string, field fieldTarget) error {
	if err := run.setPasteboard(ctx, text); err != nil {
		return fmt.Errorf("set pasteboard: %w", err)
	}
	if err := run.sendHID(ctx, pasteChordEvents()...); err != nil {
		return fmt.Errorf("send paste chord: %w", err)
	}
	verifiable := field.identifier != ""
	maxPolls := int(pasteVerifyTimeout / pastePoll)
	for poll := 0; poll < maxPolls; poll++ {
		dump, err := run.describeAll(ctx)
		if err != nil {
			return fmt.Errorf("describe accessibility: %w", err)
		}
		if pasteLanded(dump, field.identifier, text) {
			return nil
		}
		if button, found := findAllowPasteButton(dump); found {
			// The dialog swallowed the paste; dismiss it, refocus, and resend
			// the chord exactly once. Dismissing blacks out the bridge, so the
			// landed value only appears on a later poll.
			if err := run.sendHID(ctx, tapEvents(button.centerX, button.centerY)...); err != nil {
				return fmt.Errorf("tap allow button: %w", err)
			}
			if err := run.sleep(ctx, dialogSettle); err != nil {
				return err
			}
			if err := run.sendHID(ctx, tapEvents(field.centerX, field.centerY)...); err != nil {
				return fmt.Errorf("refocus field: %w", err)
			}
			if err := run.sleep(ctx, dialogSettle); err != nil {
				return err
			}
			if err := run.sendHID(ctx, pasteChordEvents()...); err != nil {
				return fmt.Errorf("send paste chord: %w", err)
			}
			continue
		}
		if !verifiable {
			// Without a field identifier the paste cannot be confirmed. The
			// chord went out and no dialog is blocking it, so one settle is the
			// best available behavior.
			return run.sleep(ctx, pastePoll)
		}
		// Field not yet showing the text: either the bridge is still blacked
		// out from the dialog or the paste has not rendered. Keep polling until
		// the value lands or the budget runs out.
		if err := run.sleep(ctx, pastePoll); err != nil {
			return err
		}
	}
	return fmt.Errorf("paste did not land within %s", pasteVerifyTimeout)
}

// eraseBackspaceThreshold is the largest erase still sent as individual
// backspaces. Backspaces render progressively on the simulator (tens of
// milliseconds per character), so clearing a long field key-by-key leaves the
// screen churning long after the HID call returns and races whatever input
// follows. Above the threshold the field is cleared atomically instead.
const eraseBackspaceThreshold = 3

// eraseText deletes characterCount characters from the focused field. Small
// counts go as backspaces in one HID stream; larger counts clear the whole
// field via select-all plus one backspace. The runner asks for the field's
// full length when it pre-erases (replace semantics), so treating a large
// count as clear-the-field matches its intent while landing in one frame.
func eraseText(ctx context.Context, run runner, characterCount int) error {
	if characterCount <= 0 {
		return nil
	}
	if characterCount <= eraseBackspaceThreshold {
		return run.sendHID(ctx, keyPressEvents(backspaces(characterCount))...)
	}
	events := append(selectAllChordEvents(), keyPressEvents(backspaces(1))...)
	return run.sendHID(ctx, events...)
}

// keyPressEvents flattens key presses into a HID event stream. A shifted press
// is wrapped with left-shift down before and up after, so the shift modifier is
// held only for that key.
func keyPressEvents(presses []KeyPress) []transport.HIDEvent {
	events := make([]transport.HIDEvent, 0, len(presses)*2)
	for _, press := range presses {
		if press.Shift {
			events = append(events, transport.KeyDown(usageLeftShift))
		}
		events = append(events, transport.KeyDown(press.Usage), transport.KeyUp(press.Usage))
		if press.Shift {
			events = append(events, transport.KeyUp(usageLeftShift))
		}
	}
	return events
}

// pasteChordEvents is the command+V chord: command down, V down, V up,
// command up.
func pasteChordEvents() []transport.HIDEvent {
	return []transport.HIDEvent{
		transport.KeyDown(LeftGUI),
		transport.KeyDown(VKey),
		transport.KeyUp(VKey),
		transport.KeyUp(LeftGUI),
	}
}

// selectAllChordEvents is the command+A chord selecting the focused field's
// whole content.
func selectAllChordEvents() []transport.HIDEvent {
	return []transport.HIDEvent{
		transport.KeyDown(LeftGUI),
		transport.KeyDown(usageA),
		transport.KeyUp(usageA),
		transport.KeyUp(LeftGUI),
	}
}

// tapEvents is a single tap: finger down then up at one point.
func tapEvents(x, y float64) []transport.HIDEvent {
	return []transport.HIDEvent{transport.TouchDown(x, y), transport.TouchUp(x, y)}
}

// doubleTapEvents is two taps in one stream separated by gapMilliseconds.
func doubleTapEvents(x, y float64, gapMilliseconds float64) []transport.HIDEvent {
	return []transport.HIDEvent{
		transport.TouchDown(x, y), transport.TouchUp(x, y),
		transport.Delay(gapMilliseconds),
		transport.TouchDown(x, y), transport.TouchUp(x, y),
	}
}

// longPressEvents is a finger held down for holdMilliseconds before lifting.
func longPressEvents(x, y float64, holdMilliseconds float64) []transport.HIDEvent {
	return []transport.HIDEvent{
		transport.TouchDown(x, y),
		transport.Delay(holdMilliseconds),
		transport.TouchUp(x, y),
	}
}

// allowPasteButton is the located allow button of the paste-permission dialog.
type allowPasteButton struct {
	centerX float64
	centerY float64
}

// allowPasteLabels are the known en-US labels of the paste dialog's accept
// button. iOS also surfaces a reject button ("Don't Allow Paste"), so a plain
// "Allow" substring match would be ambiguous; the located labels are matched
// exactly against the trimmed AXLabel.
var allowPasteLabels = []string{"Allow Paste", "Allow"}

// findAllowPasteButton locates the allow button of the paste-permission dialog
// in a describe-all dump. It first matches a button whose label is a known
// allow label. Failing that, it applies a conservative fallback: if exactly one
// enabled button is present in the dump, that lone button is taken to be the
// dialog's allow control. The fallback is deliberately narrow so it cannot fire
// on an ordinary screen full of buttons; the dialog is modal and collapses the
// dump to its own controls.
func findAllowPasteButton(dump []byte) (allowPasteButton, bool) {
	elements := decodeDump(dump)

	for _, element := range elements {
		if element.Type != "Button" {
			continue
		}
		label := strings.TrimSpace(stringValue(element.AXLabel))
		if isRejectPasteLabel(label) {
			continue
		}
		for _, allow := range allowPasteLabels {
			if label == allow {
				if center, ok := buttonCenter(element); ok {
					return center, true
				}
			}
		}
	}

	var soleEnabled allowPasteButton
	enabledButtons := 0
	for _, element := range elements {
		if element.Type != "Button" || !element.Enabled {
			continue
		}
		if isRejectPasteLabel(strings.TrimSpace(stringValue(element.AXLabel))) {
			continue
		}
		center, ok := buttonCenter(element)
		if !ok {
			continue
		}
		enabledButtons++
		soleEnabled = center
	}
	if enabledButtons == 1 {
		return soleEnabled, true
	}
	return allowPasteButton{}, false
}

// isRejectPasteLabel reports whether label is the dialog's reject button. iOS
// renders the apostrophe as a right single quotation mark (U+2019), so both the
// curly and straight forms are checked.
func isRejectPasteLabel(label string) bool {
	return strings.Contains(label, "Don’t Allow Paste") ||
		strings.Contains(label, "Don't Allow Paste")
}

func buttonCenter(element rawElement) (allowPasteButton, bool) {
	frame := element.Frame
	if !finite(frame.X) || !finite(frame.Y) || !finite(frame.Width) || !finite(frame.Height) {
		return allowPasteButton{}, false
	}
	if frame.Width == 0 && frame.Height == 0 {
		return allowPasteButton{}, false
	}
	return allowPasteButton{
		centerX: frame.X + frame.Width/2,
		centerY: frame.Y + frame.Height/2,
	}, true
}

// pasteLanded reports whether the field identified by fieldIdentifier now shows
// expectedText in its AXValue. The paste appends at the cursor, so a substring
// match (rather than equality) is used: the field may already hold text. A
// secure field masks its value as bullets, making content verification
// impossible; a non-empty all-bullet value counts as landed.
func pasteLanded(dump []byte, fieldIdentifier, expectedText string) bool {
	if fieldIdentifier == "" || expectedText == "" {
		return false
	}
	for _, element := range decodeDump(dump) {
		if stringValue(element.AXUniqueID) != fieldIdentifier {
			continue
		}
		value := stringValue(element.AXValue)
		if strings.Contains(value, expectedText) {
			return true
		}
		return isMaskedValue(value)
	}
	return false
}

// isMaskedValue reports whether value is a secure field's masked content:
// non-empty and made up entirely of bullet characters.
func isMaskedValue(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r != '•' {
			return false
		}
	}
	return true
}

// decodeDump parses a flat describe-all dump into elements, reusing the same
// element shape and per-element tolerance as the hierarchy mapper: a single
// malformed entry is skipped rather than discarding the whole dump.
func decodeDump(dump []byte) []rawElement {
	var rawElements []json.RawMessage
	if len(dump) > 0 {
		_ = json.Unmarshal(dump, &rawElements)
	}
	elements := make([]rawElement, 0, len(rawElements))
	for _, raw := range rawElements {
		var element rawElement
		if err := json.Unmarshal(raw, &element); err != nil {
			continue
		}
		elements = append(elements, element)
	}
	return elements
}
