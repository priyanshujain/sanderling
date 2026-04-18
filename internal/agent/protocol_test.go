package agent

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func roundTrip(t *testing.T, message Message) Message {
	t.Helper()
	var buffer bytes.Buffer
	if err := WriteMessage(&buffer, message); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	got, err := ReadMessage(&buffer)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	return got
}

func TestRoundTrip_Hello(t *testing.T) {
	got := roundTrip(t, Hello("0.0.1", "android", "in.okcredit.merchant"))
	if got.Type != MessageTypeHello || got.Version != "0.0.1" || got.Platform != "android" || got.AppPackage != "in.okcredit.merchant" {
		t.Fatalf("hello round-trip failed: %+v", got)
	}
	if got.ProtocolVersion != ProtocolVersion {
		t.Errorf("protocol_version: got %d, want %d", got.ProtocolVersion, ProtocolVersion)
	}
}

func TestRoundTrip_PauseResume(t *testing.T) {
	for _, builder := range []func(uint64) Message{Pause, Resume} {
		got := roundTrip(t, builder(42))
		if got.ID != 42 {
			t.Errorf("id round-trip failed: %+v", got)
		}
	}
}

func TestRoundTrip_State(t *testing.T) {
	snapshots := map[string]json.RawMessage{
		"screen":         json.RawMessage(`"customer_ledger"`),
		"ledger.balance": json.RawMessage(`1500`),
		"is_signed_in":   json.RawMessage(`true`),
	}
	got := roundTrip(t, State(7, snapshots))
	if got.Type != MessageTypeState || got.ID != 7 {
		t.Fatalf("state envelope wrong: %+v", got)
	}
	if string(got.Snapshots["screen"]) != `"customer_ledger"` {
		t.Errorf("screen snapshot wrong: %s", got.Snapshots["screen"])
	}
	if string(got.Snapshots["ledger.balance"]) != `1500` {
		t.Errorf("balance snapshot wrong: %s", got.Snapshots["ledger.balance"])
	}
}

func TestRoundTrip_ExtractResult(t *testing.T) {
	got := roundTrip(t, ExtractResult(1, "ledger.balance", json.RawMessage(`2500`), ""))
	if got.Extractor != "ledger.balance" || string(got.Result) != `2500` {
		t.Fatalf("extract result round-trip failed: %+v", got)
	}

	failed := roundTrip(t, ExtractResult(2, "ledger.balance", nil, "no active customer"))
	if failed.Error != "no active customer" {
		t.Errorf("extract error round-trip failed: %+v", failed)
	}
}

func TestRoundTrip_Goodbye(t *testing.T) {
	got := roundTrip(t, Goodbye("app terminated"))
	if got.Type != MessageTypeGoodbye || got.Reason != "app terminated" {
		t.Fatalf("goodbye round-trip failed: %+v", got)
	}
}

func TestWriteMessage_FrameFormat(t *testing.T) {
	var buffer bytes.Buffer
	if err := WriteMessage(&buffer, Pause(99)); err != nil {
		t.Fatal(err)
	}
	raw := buffer.Bytes()
	if len(raw) < 4 {
		t.Fatalf("frame too short: %d bytes", len(raw))
	}
	length := binary.BigEndian.Uint32(raw[:4])
	if int(length) != len(raw)-4 {
		t.Errorf("header length %d mismatches payload length %d", length, len(raw)-4)
	}
	if !strings.Contains(string(raw[4:]), `"type":"PAUSE"`) {
		t.Errorf("payload does not contain PAUSE type: %s", raw[4:])
	}
}

func TestReadMessage_ShortReaderReturnsEOF(t *testing.T) {
	_, err := ReadMessage(bytes.NewReader(nil))
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected EOF on empty reader, got %v", err)
	}
}

func TestReadMessage_OversizedFrameRejected(t *testing.T) {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(MaxFrameSize+1))
	_, err := ReadMessage(bytes.NewReader(header[:]))
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("expected oversized-frame error, got %v", err)
	}
}

func TestReadMessage_MissingTypeRejected(t *testing.T) {
	var buffer bytes.Buffer
	payload := []byte(`{"id":1}`)
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	buffer.Write(header[:])
	buffer.Write(payload)

	_, err := ReadMessage(&buffer)
	if err == nil || !strings.Contains(err.Error(), "missing type") {
		t.Errorf("expected missing-type error, got %v", err)
	}
}

func TestWriteMessage_StreamsMultipleFrames(t *testing.T) {
	var buffer bytes.Buffer
	messages := []Message{
		Hello("v", "android", "com.x"),
		Pause(1),
		State(1, map[string]json.RawMessage{"x": json.RawMessage(`42`)}),
		Resume(1),
		Goodbye("done"),
	}
	for _, message := range messages {
		if err := WriteMessage(&buffer, message); err != nil {
			t.Fatal(err)
		}
	}
	for index, want := range messages {
		got, err := ReadMessage(&buffer)
		if err != nil {
			t.Fatalf("frame %d: %v", index, err)
		}
		if got.Type != want.Type {
			t.Errorf("frame %d: got type %q, want %q", index, got.Type, want.Type)
		}
	}
}
