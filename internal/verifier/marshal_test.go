package verifier

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dop251/goja"
)

// TestDecodeAction_AllKinds decodes the flat camelCase wire contract for every
// action kind from raw JSON. Bug class: an action field rename or a missing
// case silently mangles or drops actions on the decode side.
func TestDecodeAction_AllKinds(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want Action
	}{
		{
			name: "Tap",
			raw:  `{"kind":"Tap","selector":"id:btn","x":10,"y":20}`,
			want: Action{Kind: ActionKindTap, On: "id:btn", X: 10, Y: 20},
		},
		{
			name: "DoubleTap",
			raw:  `{"kind":"DoubleTap","selector":"id:btn","x":1,"y":2}`,
			want: Action{Kind: ActionKindDoubleTap, On: "id:btn", X: 1, Y: 2},
		},
		{
			name: "LongPress",
			raw:  `{"kind":"LongPress","selector":"id:btn","x":3,"y":4}`,
			want: Action{Kind: ActionKindLongPress, On: "id:btn", X: 3, Y: 4},
		},
		{
			name: "InputText",
			raw:  `{"kind":"InputText","selector":"id:field","text":"hi","x":5,"y":6}`,
			want: Action{Kind: ActionKindInputText, On: "id:field", Text: "hi", X: 5, Y: 6},
		},
		{
			name: "Swipe",
			raw:  `{"kind":"Swipe","fromX":1,"fromY":2,"toX":3,"toY":4,"durationMillis":250}`,
			want: Action{Kind: ActionKindSwipe, FromX: 1, FromY: 2, ToX: 3, ToY: 4, DurationMillis: 250},
		},
		{
			name: "Scroll",
			raw:  `{"kind":"Scroll","direction":"down","fromX":1,"fromY":2,"toX":3,"toY":4,"durationMillis":100}`,
			want: Action{Kind: ActionKindScroll, Direction: "down", FromX: 1, FromY: 2, ToX: 3, ToY: 4, DurationMillis: 100},
		},
		{
			name: "PressKey",
			raw:  `{"kind":"PressKey","key":"back"}`,
			want: Action{Kind: ActionKindPressKey, Key: "back"},
		},
		{
			name: "Wait",
			raw:  `{"kind":"Wait","durationMillis":500}`,
			want: Action{Kind: ActionKindWait, DurationMillis: 500},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := DecodeAction(json.RawMessage(testCase.raw))
			if err != nil {
				t.Fatalf("DecodeAction(%s): %v", testCase.raw, err)
			}
			if got != testCase.want {
				t.Errorf("DecodeAction(%s) = %+v, want %+v", testCase.raw, got, testCase.want)
			}
		})
	}
}

// TestDecodeAction_NullAndUnknown verifies a null/empty payload reports
// ErrNoAction and an unrecognized kind returns an error. Bug class: an unknown
// kind silently decoding to a zero Action (a no-op the runner dispatches).
func TestDecodeAction_NullAndUnknown(t *testing.T) {
	for _, raw := range []string{"null", ""} {
		if _, err := DecodeAction(json.RawMessage(raw)); !errors.Is(err, ErrNoAction) {
			t.Errorf("DecodeAction(%q): err = %v, want ErrNoAction", raw, err)
		}
	}
	if _, err := DecodeAction(json.RawMessage(`{"kind":"Teleport"}`)); err == nil {
		t.Error("DecodeAction(unknown kind): err = nil, want error")
	}
}

// TestLastActionObject_ExposesKindSpecificFields pushes a LastAction of each
// kind and asserts the JS-side lastAction object the spec reads carries the
// kind plus that kind's fields (on/from/direction/key). Bug class: specs
// gating on lastAction see wrong or missing fields.
func TestLastActionObject_ExposesKindSpecificFields(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.last = __sanderling__.extract(state => state.lastAction);
	`)

	read := func(t *testing.T, action *Action) *goja.Object {
		t.Helper()
		if err := verifier.PushSnapshot(SnapshotInput{Snapshots: Snapshots{}, LastAction: action}); err != nil {
			t.Fatal(err)
		}
		current := verifier.runtime.GlobalObject().Get("last").ToObject(verifier.runtime).Get("current")
		if goja.IsNull(current) || goja.IsUndefined(current) {
			t.Fatal("lastAction current is null/undefined")
		}
		return current.ToObject(verifier.runtime)
	}

	t.Run("Tap", func(t *testing.T) {
		obj := read(t, &Action{Kind: ActionKindTap, On: "id:btn"})
		if obj.Get("kind").String() != "Tap" {
			t.Errorf("kind = %q, want Tap", obj.Get("kind"))
		}
		if obj.Get("on").String() != "id:btn" {
			t.Errorf("on = %q, want id:btn", obj.Get("on"))
		}
	})

	t.Run("Swipe", func(t *testing.T) {
		obj := read(t, &Action{Kind: ActionKindSwipe, FromX: 1, FromY: 2, ToX: 3, ToY: 4})
		from := obj.Get("from").ToObject(verifier.runtime)
		if from.Get("x").ToInteger() != 1 || from.Get("y").ToInteger() != 2 {
			t.Errorf("from = (%v,%v), want (1,2)", from.Get("x"), from.Get("y"))
		}
		to := obj.Get("to").ToObject(verifier.runtime)
		if to.Get("x").ToInteger() != 3 || to.Get("y").ToInteger() != 4 {
			t.Errorf("to = (%v,%v), want (3,4)", to.Get("x"), to.Get("y"))
		}
	})

	t.Run("Scroll", func(t *testing.T) {
		obj := read(t, &Action{Kind: ActionKindScroll, Direction: "down", FromX: 5, FromY: 6})
		if obj.Get("direction").String() != "down" {
			t.Errorf("direction = %q, want down", obj.Get("direction"))
		}
		from := obj.Get("from").ToObject(verifier.runtime)
		if from.Get("x").ToInteger() != 5 || from.Get("y").ToInteger() != 6 {
			t.Errorf("from = (%v,%v), want (5,6)", from.Get("x"), from.Get("y"))
		}
	})

	t.Run("PressKey", func(t *testing.T) {
		obj := read(t, &Action{Kind: ActionKindPressKey, Key: "back"})
		if obj.Get("key").String() != "back" {
			t.Errorf("key = %q, want back", obj.Get("key"))
		}
	})
}

// TestLastAction_WebJSONMatchesTheGojaObject pins the two hosts to ONE shape.
// The goja host builds state.lastAction as a JS object; the web host receives
// EncodeLastAction's JSON and installs the parsed value as state.lastAction in
// the page. A field this side renames, drops or cases differently would leave a
// spec reading state.lastAction working on native and silently mismatching on
// web, which is the failure this whole path exists to prevent. Comparing
// goja's own JSON.stringify against the encoder is the strongest available
// statement that the two are the same object.
func TestLastAction_WebJSONMatchesTheGojaObject(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.last = __sanderling__.extract(state => JSON.stringify(state.lastAction));
	`)

	for _, testCase := range []struct {
		name   string
		action *Action
	}{
		{"nil", nil},
		{"Tap", &Action{Kind: ActionKindTap, On: "id:TxnSubmit", X: 12, Y: 34}},
		{"TapApplied", &Action{Kind: ActionKindTap, On: "id:TxnSubmit", Applied: true}},
		{"TapWithoutSelector", &Action{Kind: ActionKindTap, X: 12, Y: 34}},
		{"DoubleTap", &Action{Kind: ActionKindDoubleTap, On: `desc:say "hi" <b>`}},
		{"InputText", &Action{Kind: ActionKindInputText, On: "id:field", Text: "50"}},
		{"Swipe", &Action{Kind: ActionKindSwipe, FromX: 1, FromY: 2, ToX: 3, ToY: 4, DurationMillis: 250}},
		{"SwipeNoDuration", &Action{Kind: ActionKindSwipe, FromX: 1, FromY: 2, ToX: 3, ToY: 4}},
		{"Scroll", &Action{Kind: ActionKindScroll, Direction: "down", FromX: 5, FromY: 6, ToX: 5, ToY: 1}},
		{"PressKey", &Action{Kind: ActionKindPressKey, Key: "enter"}},
		{"Wait", &Action{Kind: ActionKindWait, DurationMillis: 500}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := verifier.PushSnapshot(SnapshotInput{
				Snapshots:  Snapshots{},
				LastAction: testCase.action,
			}); err != nil {
				t.Fatal(err)
			}
			handle := verifier.runtime.GlobalObject().Get("last").ToObject(verifier.runtime)
			goja := handle.Get("current").String()
			web := string(EncodeLastAction(testCase.action))
			if goja != web {
				t.Errorf("the two hosts disagree on state.lastAction\n goja: %s\n  web: %s", goja, web)
			}
		})
	}
}

// A spec has to be able to tell three things apart: no action ran, an action
// ran, and an action was dispatched whose fate the runner cannot vouch for.
// The third used to be reported as the first, which is how a property that
// reasons "an effect landed with no action to cause it" convicts an app over
// an RPC deadline.
func TestLastAction_SeparatesNoActionFromAnActionOfUnknownFate(t *testing.T) {
	verifier := newVerifier(t)
	mustLoad(t, verifier, `
		globalThis.fate = __sanderling__.extract(state =>
			state.lastAction === null ? "no action"
			: state.lastAction.applied === true ? "applied"
			: state.lastAction.applied === null ? "unknown"
			: "unreadable");
	`)

	for _, testCase := range []struct {
		name   string
		action *Action
		want   string
	}{
		{"nothing ran", nil, "no action"},
		{"dispatch confirmed", &Action{Kind: ActionKindTap, On: "id:TxnSubmit", Applied: true}, "applied"},
		{"dispatch unconfirmed", &Action{Kind: ActionKindTap, On: "id:TxnSubmit"}, "unknown"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := verifier.PushSnapshot(SnapshotInput{
				Snapshots:  Snapshots{},
				LastAction: testCase.action,
			}); err != nil {
				t.Fatal(err)
			}
			handle := verifier.runtime.GlobalObject().Get("fate").ToObject(verifier.runtime)
			if got := handle.Get("current").String(); got != testCase.want {
				t.Errorf("the spec read %q, want %q", got, testCase.want)
			}
		})
	}
}
