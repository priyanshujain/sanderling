package inspect

import (
	"testing"
)

func TestWatcher_UnsubscribeRemovesChannel(t *testing.T) {
	w := NewWatcher(t.TempDir())
	first := w.Subscribe()
	second := w.Subscribe()
	third := w.Subscribe()

	if count := len(w.subscribers); count != 3 {
		t.Fatalf("expected 3 subscribers, got %d", count)
	}

	w.Unsubscribe(second)

	if count := len(w.subscribers); count != 2 {
		t.Fatalf("expected 2 subscribers after Unsubscribe, got %d", count)
	}

	// broadcast should still notify remaining subscribers
	w.broadcast()
	select {
	case <-first:
	default:
		t.Error("first subscriber did not receive broadcast")
	}
	select {
	case <-third:
	default:
		t.Error("third subscriber did not receive broadcast")
	}
}

func TestWatcher_UnsubscribeUnknownChannelIsNoop(t *testing.T) {
	w := NewWatcher(t.TempDir())
	existing := w.Subscribe()

	stranger := make(chan struct{})
	w.Unsubscribe(stranger)

	if count := len(w.subscribers); count != 1 {
		t.Fatalf("expected 1 subscriber after no-op Unsubscribe, got %d", count)
	}

	w.broadcast()
	select {
	case <-existing:
	default:
		t.Error("existing subscriber did not receive broadcast")
	}
}
