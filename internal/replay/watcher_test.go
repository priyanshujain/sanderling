package replay

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcher_RunCoalescesCreateBurstIntoOneBroadcast(t *testing.T) {
	directory := t.TempDir()
	w := NewWatcher(directory)
	w.debounce = 30 * time.Millisecond
	events := w.Subscribe()

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = w.Run(ctx) }()
	defer cancel()
	waitForWatch(t, w.debounce)

	for i := 0; i < 5; i++ {
		mustCreate(t, filepath.Join(directory, "run-"+string(rune('a'+i)), "meta.json"))
	}

	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("burst of creates produced no broadcast")
	}
	if extra := drainWithin(events, 4*w.debounce); extra != 0 {
		t.Errorf("burst yielded %d extra broadcasts, want a single coalesced one", extra)
	}
}

func TestWatcher_RunIgnoresWriteAndChmod(t *testing.T) {
	directory := t.TempDir()
	existing := filepath.Join(directory, "marker")
	if err := os.WriteFile(existing, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := NewWatcher(directory)
	w.debounce = 30 * time.Millisecond
	events := w.Subscribe()

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = w.Run(ctx) }()
	defer cancel()
	waitForWatch(t, w.debounce)

	if err := os.WriteFile(existing, []byte("touched"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(existing, 0o600); err != nil {
		t.Fatal(err)
	}

	if got := drainWithin(events, 6*w.debounce); got != 0 {
		t.Errorf("write/chmod produced %d broadcasts, want 0", got)
	}
}

func TestWatcher_RunCancelClosesSubscribers(t *testing.T) {
	w := NewWatcher(t.TempDir())
	w.debounce = 30 * time.Millisecond
	events := w.Subscribe()

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = w.Run(ctx) }()
	waitForWatch(t, w.debounce)
	cancel()

	select {
	case _, ok := <-events:
		if ok {
			// drain any pending broadcast, then expect close
			_, ok = <-events
		}
		if ok {
			t.Fatal("subscriber channel should be closed after ctx cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber channel was not closed after ctx cancel")
	}

	post := w.Subscribe()
	select {
	case _, ok := <-post:
		if ok {
			t.Error("Subscribe after shutdown should return a pre-closed channel")
		}
	case <-time.After(time.Second):
		t.Error("Subscribe after shutdown blocked instead of returning a closed channel")
	}
}

func mustCreate(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func waitForWatch(t *testing.T, debounce time.Duration) {
	t.Helper()
	time.Sleep(10 * debounce)
}

func drainWithin(events <-chan struct{}, window time.Duration) int {
	count := 0
	deadline := time.After(window)
	for {
		select {
		case <-events:
			count++
		case <-deadline:
			return count
		}
	}
}

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
