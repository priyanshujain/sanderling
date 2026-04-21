package inspect

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const watcherDebounce = 200 * time.Millisecond

// Watcher reports coalesced runs.changed events from the runs directory.
// Subscribe returns a channel that receives one event per debounce window.
// The watcher tolerates a missing runs directory by polling for it to appear.
type Watcher struct {
	directory   string
	debounce    time.Duration
	mutex       sync.Mutex
	subscribers []chan struct{}
	closed      bool
}

func NewWatcher(directory string) *Watcher {
	return &Watcher{directory: directory, debounce: watcherDebounce}
}

func (w *Watcher) Subscribe() <-chan struct{} {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	channel := make(chan struct{}, 4)
	if w.closed {
		close(channel)
		return channel
	}
	w.subscribers = append(w.subscribers, channel)
	return channel
}

// Unsubscribe removes a channel previously returned by Subscribe. The channel
// is not closed because broadcast snapshots subscribers without holding the
// mutex and a concurrent close would race with its non-blocking send.
// Safe to call multiple times; unknown channels are ignored.
func (w *Watcher) Unsubscribe(subscription <-chan struct{}) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	for index, channel := range w.subscribers {
		if (<-chan struct{})(channel) != subscription {
			continue
		}
		last := len(w.subscribers) - 1
		w.subscribers[index] = w.subscribers[last]
		w.subscribers[last] = nil
		w.subscribers = w.subscribers[:last]
		return
	}
}

// Run blocks until ctx is canceled, watching directory for create/remove/rename
// events and emitting one notification per debounce window to all subscribers.
func (w *Watcher) Run(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	if err := watchOrWaitForDirectory(ctx, watcher, w.directory); err != nil {
		return err
	}

	var pending bool
	timer := time.NewTimer(w.debounce)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			w.shutdown()
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				w.shutdown()
				return nil
			}
			if event.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			if !pending {
				pending = true
				timer.Reset(w.debounce)
			}
		case <-watcher.Errors:
			// Drop transient errors; SSE is best-effort.
		case <-timer.C:
			if pending {
				pending = false
				w.broadcast()
			}
		}
	}
}

func (w *Watcher) broadcast() {
	w.mutex.Lock()
	subscribers := append([]chan struct{}(nil), w.subscribers...)
	w.mutex.Unlock()
	for _, channel := range subscribers {
		select {
		case channel <- struct{}{}:
		default:
		}
	}
}

func (w *Watcher) shutdown() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.closed = true
	for _, channel := range w.subscribers {
		close(channel)
	}
	w.subscribers = nil
}

func watchOrWaitForDirectory(ctx context.Context, watcher *fsnotify.Watcher, directory string) error {
	for {
		err := watcher.Add(directory)
		if err == nil {
			return nil
		}
		if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second):
		}
	}
}
