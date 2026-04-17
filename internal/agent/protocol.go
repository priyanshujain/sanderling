package agent

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type MessageType string

const (
	MessageTypeHello         MessageType = "HELLO"
	MessageTypePause         MessageType = "PAUSE"
	MessageTypeResume        MessageType = "RESUME"
	MessageTypeState         MessageType = "STATE"
	MessageTypeExtractResult MessageType = "EXTRACT_RESULT"
	MessageTypeGoodbye       MessageType = "GOODBYE"
)

const MaxFrameSize = 16 * 1024 * 1024

type Message struct {
	Type MessageType `json:"type"`
	ID   uint64      `json:"id,omitempty"`

	Version    string `json:"version,omitempty"`
	Platform   string `json:"platform,omitempty"`
	AppPackage string `json:"app_package,omitempty"`

	Snapshots map[string]json.RawMessage `json:"snapshots,omitempty"`

	Extractor string          `json:"extractor,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`

	Reason string `json:"reason,omitempty"`
}

func Hello(version, platform, appPackage string) Message {
	return Message{Type: MessageTypeHello, Version: version, Platform: platform, AppPackage: appPackage}
}

func Pause(id uint64) Message { return Message{Type: MessageTypePause, ID: id} }

func Resume(id uint64) Message { return Message{Type: MessageTypeResume, ID: id} }

func State(id uint64, snapshots map[string]json.RawMessage) Message {
	return Message{Type: MessageTypeState, ID: id, Snapshots: snapshots}
}

func ExtractResult(id uint64, extractor string, result json.RawMessage, extractorError string) Message {
	return Message{
		Type:      MessageTypeExtractResult,
		ID:        id,
		Extractor: extractor,
		Result:    result,
		Error:     extractorError,
	}
}

func Goodbye(reason string) Message {
	return Message{Type: MessageTypeGoodbye, Reason: reason}
}

func WriteMessage(writer io.Writer, message Message) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if len(payload) > MaxFrameSize {
		return fmt.Errorf("frame of %d bytes exceeds maximum %d", len(payload), MaxFrameSize)
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := writer.Write(header[:]); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := writer.Write(payload); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}
	return nil
}

func ReadMessage(reader io.Reader) (Message, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return Message{}, err
	}
	length := binary.BigEndian.Uint32(header[:])
	if length > MaxFrameSize {
		return Message{}, fmt.Errorf("frame of %d bytes exceeds maximum %d", length, MaxFrameSize)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return Message{}, fmt.Errorf("read payload: %w", err)
	}
	var message Message
	if err := json.Unmarshal(payload, &message); err != nil {
		return Message{}, fmt.Errorf("unmarshal: %w", err)
	}
	if message.Type == "" {
		return Message{}, errors.New("missing type")
	}
	return message, nil
}
