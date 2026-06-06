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

// pasteAttempts bounds the paste-and-dismiss-dialog retry loop. Each attempt
// resends the paste chord and checks for the permission dialog or a landed
// value, so eight attempts comfortably covers the one-to-two dialogs iOS shows
// per app session plus a couple of refocus retries.
const pasteAttempts = 8

// pasteSettle is how long to wait after a paste chord before reading the
// hierarchy. The warm-paste measurement in the validated spike settled around
// 124ms; this leaves margin for a cold paste while keeping the loop tight.
const pasteSettle = 350 * time.Millisecond

// dialogSettle is the pause after tapping the dialog's allow button and after
// refocusing the field, before the next paste attempt.
const dialogSettle = 200 * time.Millisecond

// warmUpPrimer is the throwaway string pasted at session start so the
// permission dialog fires and gets handled before any real input.
const warmUpPrimer = "sanderling"

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

// inputText types text into the focused field. Mappable text goes through the
// hardware keyboard in one HID stream; anything else falls back to the
// pasteboard. The field target is only consulted on the pasteboard path.
func inputText(ctx context.Context, run runner, text string, field fieldTarget) error {
	presses, skipped := typeString(text)
	if len(skipped) == 0 {
		return run.sendHID(ctx, keyPressEvents(presses)...)
	}
	return pasteText(ctx, run, text, field)
}

// pasteText copies the full text to the pasteboard, sends the paste chord, and
// loops past the permission dialog until the field reflects the text or the
// attempts are exhausted.
func pasteText(ctx context.Context, run runner, text string, field fieldTarget) error {
	if err := run.setPasteboard(ctx, text); err != nil {
		return fmt.Errorf("set pasteboard: %w", err)
	}
	for attempt := 0; attempt < pasteAttempts; attempt++ {
		if err := run.sendHID(ctx, pasteChordEvents()...); err != nil {
			return fmt.Errorf("send paste chord: %w", err)
		}
		if err := run.sleep(ctx, pasteSettle); err != nil {
			return err
		}
		dump, err := run.describeAll(ctx)
		if err != nil {
			return fmt.Errorf("describe accessibility: %w", err)
		}
		if pasteLanded(dump, field.identifier, text) {
			return nil
		}
		button, found := findAllowPasteButton(dump)
		if !found {
			if err := run.sleep(ctx, dialogSettle); err != nil {
				return err
			}
			continue
		}
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
	}
	return fmt.Errorf("paste did not land after %d attempts", pasteAttempts)
}

// warmUpPaste pastes a throwaway primer once so the iOS pasteboard-permission
// dialog fires and gets dismissed at a moment the driver chooses (session
// start) rather than mid-run. It does not assert the paste landed: the goal is
// only to clear the dialog. A best-effort allow-button tap handles the prompt.
func warmUpPaste(ctx context.Context, run runner) error {
	if err := run.setPasteboard(ctx, warmUpPrimer); err != nil {
		return fmt.Errorf("set pasteboard: %w", err)
	}
	if err := run.sendHID(ctx, pasteChordEvents()...); err != nil {
		return fmt.Errorf("send paste chord: %w", err)
	}
	if err := run.sleep(ctx, pasteSettle); err != nil {
		return err
	}
	dump, err := run.describeAll(ctx)
	if err != nil {
		return fmt.Errorf("describe accessibility: %w", err)
	}
	if button, found := findAllowPasteButton(dump); found {
		if err := run.sendHID(ctx, tapEvents(button.centerX, button.centerY)...); err != nil {
			return fmt.Errorf("tap allow button: %w", err)
		}
	}
	return nil
}

// eraseText deletes characterCount characters from the focused field by sending
// that many backspaces in one HID stream.
func eraseText(ctx context.Context, run runner, characterCount int) error {
	if characterCount <= 0 {
		return nil
	}
	return run.sendHID(ctx, keyPressEvents(backspaces(characterCount))...)
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
// match (rather than equality) is used: the field may already hold text.
func pasteLanded(dump []byte, fieldIdentifier, expectedText string) bool {
	if fieldIdentifier == "" || expectedText == "" {
		return false
	}
	for _, element := range decodeDump(dump) {
		if stringValue(element.AXUniqueID) != fieldIdentifier {
			continue
		}
		return strings.Contains(stringValue(element.AXValue), expectedText)
	}
	return false
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
