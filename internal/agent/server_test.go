package agent

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSDK drives the client side of an agent connection the way the real SDK
// would: HELLO on connect, then respond to PAUSE with STATE, honor RESUME,
// and close on GOODBYE.
type fakeSDK struct {
	conn         net.Conn
	snapshotFunc func(id uint64) map[string]json.RawMessage
}

func (f *fakeSDK) sendHello(version, platform, appPackage string) error {
	return WriteMessage(f.conn, Hello(version, platform, appPackage))
}

func (f *fakeSDK) serveOne() error {
	message, err := ReadMessage(f.conn)
	if err != nil {
		return err
	}
	switch message.Type {
	case MessageTypePause:
		snapshots := f.snapshotFunc(message.ID)
		return WriteMessage(f.conn, State(message.ID, snapshots))
	case MessageTypeResume:
		return nil
	case MessageTypeGoodbye:
		return nil
	default:
		return nil
	}
}

func newLoopbackServer(t *testing.T) *Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	return NewServer(listener)
}

func TestServer_AcceptHandshake(t *testing.T) {
	server := newLoopbackServer(t)

	connectErr := make(chan error, 1)
	go func() {
		client, err := net.Dial("tcp", server.Addr().String())
		if err != nil {
			connectErr <- err
			return
		}
		sdk := &fakeSDK{conn: client}
		connectErr <- sdk.sendHello("0.0.1", "android", "com.example")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := server.Accept(ctx)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer conn.Close()

	if got := conn.Hello(); got.Type != MessageTypeHello || got.Version != "0.0.1" || got.AppPackage != "com.example" {
		t.Errorf("unexpected hello: %+v", got)
	}
	if err := <-connectErr; err != nil {
		t.Fatalf("client side: %v", err)
	}
}

func TestServer_SnapshotAndRelease(t *testing.T) {
	server := newLoopbackServer(t)

	var wg sync.WaitGroup
	wg.Go(func() {
		client, err := net.Dial("tcp", server.Addr().String())
		if err != nil {
			t.Errorf("dial: %v", err)
			return
		}
		sdk := &fakeSDK{
			conn: client,
			snapshotFunc: func(id uint64) map[string]json.RawMessage {
				return map[string]json.RawMessage{
					"screen":         json.RawMessage(`"home"`),
					"ledger.balance": json.RawMessage(`1500`),
				}
			},
		}
		if err := sdk.sendHello("0.0.1", "android", "com.x"); err != nil {
			t.Errorf("hello: %v", err)
			return
		}
		for range 2 {
			if err := sdk.serveOne(); err != nil {
				t.Errorf("pause: %v", err)
				return
			}
			if err := sdk.serveOne(); err != nil {
				t.Errorf("resume: %v", err)
				return
			}
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := server.Accept(ctx)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer conn.Close()

	for expected := uint64(1); expected <= 2; expected++ {
		state, err := conn.Snapshot(ctx)
		if err != nil {
			t.Fatalf("Snapshot #%d: %v", expected, err)
		}
		if state.ID != expected {
			t.Errorf("snapshot #%d: id=%d", expected, state.ID)
		}
		if string(state.Snapshots["screen"]) != `"home"` {
			t.Errorf("snapshot #%d: screen=%s", expected, state.Snapshots["screen"])
		}
		if err := conn.Release(ctx); err != nil {
			t.Fatalf("Release #%d: %v", expected, err)
		}
	}
	wg.Wait()
}

func TestServer_AcceptRejectsProtocolVersionMismatch(t *testing.T) {
	server := newLoopbackServer(t)

	go func() {
		client, err := net.Dial("tcp", server.Addr().String())
		if err != nil {
			return
		}
		defer client.Close()
		mismatched := Hello("0.0.1", "android", "com.x")
		mismatched.ProtocolVersion = ProtocolVersion + 99
		_ = WriteMessage(client, mismatched)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := server.Accept(ctx)
	if err == nil || !strings.Contains(err.Error(), "protocol version mismatch") {
		t.Fatalf("expected protocol-version-mismatch error, got %v", err)
	}
}

func TestServer_AcceptRequiresHello(t *testing.T) {
	server := newLoopbackServer(t)

	go func() {
		client, err := net.Dial("tcp", server.Addr().String())
		if err != nil {
			return
		}
		defer client.Close()
		// Send a PAUSE instead of HELLO — server should reject.
		_ = WriteMessage(client, Pause(1))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := server.Accept(ctx)
	if err == nil || !strings.Contains(err.Error(), "expected HELLO") {
		t.Fatalf("expected HELLO-required error, got %v", err)
	}
}

func TestServer_AcceptCancelsOnContext(t *testing.T) {
	server := newLoopbackServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	acceptErr := make(chan error, 1)
	go func() { _, err := server.Accept(ctx); acceptErr <- err }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-acceptErr:
		if err == nil {
			t.Errorf("expected error after cancel, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Errorf("accept did not return after cancel")
	}
}

func TestConn_SnapshotRejectsIDMismatch(t *testing.T) {
	server := newLoopbackServer(t)

	go func() {
		client, _ := net.Dial("tcp", server.Addr().String())
		defer client.Close()
		_ = WriteMessage(client, Hello("0.0.1", "android", "com.x"))
		// Read the PAUSE but respond with a wrong id.
		msg, _ := ReadMessage(client)
		_ = WriteMessage(client, State(msg.ID+99, map[string]json.RawMessage{}))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := server.Accept(ctx)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer conn.Close()

	_, err = conn.Snapshot(ctx)
	if err == nil || !strings.Contains(err.Error(), "id mismatch") {
		t.Errorf("expected id-mismatch error, got %v", err)
	}
}

func TestConn_CloseSendsGoodbye(t *testing.T) {
	server := newLoopbackServer(t)

	received := make(chan Message, 1)
	go func() {
		client, _ := net.Dial("tcp", server.Addr().String())
		defer client.Close()
		_ = WriteMessage(client, Hello("0.0.1", "android", "com.x"))
		// Drain until GOODBYE.
		for {
			msg, err := ReadMessage(client)
			if err != nil {
				return
			}
			if msg.Type == MessageTypeGoodbye {
				received <- msg
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := server.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-received:
		if msg.Reason != "shutdown" {
			t.Errorf("expected reason=shutdown, got %q", msg.Reason)
		}
	case <-time.After(time.Second):
		t.Error("client did not receive GOODBYE")
	}
}

// TestConn_SnapshotAfterAcceptContextCancel guards against a race in
// readWithDeadline where the watcher goroutine from Accept could clobber the
// conn's read deadline with a past time after Accept returned, causing the
// next read on the same conn (Snapshot) to time out instantly.
func TestConn_SnapshotAfterAcceptContextCancel(t *testing.T) {
	for iteration := range 50 {
		server := newLoopbackServer(t)

		clientDone := make(chan struct{})
		go func() {
			defer close(clientDone)
			client, err := net.Dial("tcp", server.Addr().String())
			if err != nil {
				return
			}
			defer client.Close()
			if err := WriteMessage(client, Hello("0.0.1", "android", "com.x")); err != nil {
				return
			}
			msg, err := ReadMessage(client)
			if err != nil {
				return
			}
			_ = WriteMessage(client, State(msg.ID, map[string]json.RawMessage{"ok": json.RawMessage(`true`)}))
		}()

		acceptCtx, acceptCancel := context.WithTimeout(context.Background(), time.Second)
		conn, err := server.Accept(acceptCtx)
		acceptCancel()
		if err != nil {
			t.Fatalf("iteration %d: Accept: %v", iteration, err)
		}

		snapCtx, snapCancel := context.WithTimeout(context.Background(), 2*time.Second)
		state, err := conn.Snapshot(snapCtx)
		snapCancel()
		if err != nil {
			t.Fatalf("iteration %d: Snapshot: %v", iteration, err)
		}
		if string(state.Snapshots["ok"]) != `true` {
			t.Errorf("iteration %d: unexpected snapshots: %v", iteration, state.Snapshots)
		}
		conn.Close()
		<-clientDone
	}
}

func TestConn_SnapshotTimesOutIfSDKSilent(t *testing.T) {
	server := newLoopbackServer(t)

	go func() {
		client, _ := net.Dial("tcp", server.Addr().String())
		defer client.Close()
		_ = WriteMessage(client, Hello("0.0.1", "android", "com.x"))
		// Never respond to PAUSE.
		time.Sleep(2 * time.Second)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := server.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	fastCtx, fastCancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer fastCancel()
	_, err = conn.Snapshot(fastCtx)
	if err == nil {
		t.Errorf("expected timeout error, got nil")
	}
}
