package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

type Server struct {
	listener net.Listener
}

func NewServer(listener net.Listener) *Server {
	return &Server{listener: listener}
}

func (s *Server) Addr() net.Addr { return s.listener.Addr() }

// Accept waits for the next SDK client and performs the HELLO handshake.
// Only one Conn may be active at a time; subsequent Accepts block until the
// current connection closes.
func (s *Server) Accept(ctx context.Context) (*Conn, error) {
	cancelCloser := closeListenerOnCancel(ctx, s.listener)
	defer cancelCloser()

	rawConn, err := s.listener.Accept()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("accept: %w", err)
	}
	hello, err := readWithDeadline(ctx, rawConn)
	if err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("read hello: %w", err)
	}
	if hello.Type != MessageTypeHello {
		rawConn.Close()
		return nil, fmt.Errorf("expected HELLO, got %q", hello.Type)
	}
	return &Conn{rawConn: rawConn, hello: hello}, nil
}

func (s *Server) Close() error { return s.listener.Close() }

type Conn struct {
	rawConn net.Conn
	hello   Message
	nextID  uint64
}

func (c *Conn) Hello() Message { return c.hello }

func (c *Conn) RemoteAddr() net.Addr { return c.rawConn.RemoteAddr() }

// Snapshot sends PAUSE with a fresh id and blocks until the SDK returns the
// matching STATE. The SDK's main thread stays paused until Release is called.
func (c *Conn) Snapshot(ctx context.Context) (Message, error) {
	c.nextID++
	id := c.nextID

	if err := writeWithDeadline(ctx, c.rawConn, Pause(id)); err != nil {
		return Message{}, fmt.Errorf("send pause: %w", err)
	}
	message, err := readWithDeadline(ctx, c.rawConn)
	if err != nil {
		return Message{}, fmt.Errorf("read state: %w", err)
	}
	if message.Type != MessageTypeState {
		return Message{}, fmt.Errorf("expected STATE, got %q", message.Type)
	}
	if message.ID != id {
		return Message{}, fmt.Errorf("state id mismatch: sent %d, got %d", id, message.ID)
	}
	return message, nil
}

// Release sends RESUME, freeing the SDK's paused main thread.
func (c *Conn) Release(ctx context.Context) error {
	return writeWithDeadline(ctx, c.rawConn, Resume(c.nextID))
}

// Close sends GOODBYE (best effort) and closes the underlying connection.
func (c *Conn) Close() error {
	_ = writeWithDeadline(context.Background(), c.rawConn, Goodbye("shutdown"))
	return c.rawConn.Close()
}

func readWithDeadline(ctx context.Context, conn net.Conn) (Message, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetReadDeadline(deadline)
	}
	done := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		select {
		case <-ctx.Done():
			_ = conn.SetReadDeadline(time.Unix(1, 0))
		case <-done:
		}
	}()
	message, err := ReadMessage(conn)
	close(done)
	<-exited
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil && ctx.Err() != nil {
		return Message{}, ctx.Err()
	}
	return message, err
}

func writeWithDeadline(ctx context.Context, conn net.Conn, message Message) error {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetWriteDeadline(deadline)
		defer conn.SetWriteDeadline(time.Time{})
	}
	err := WriteMessage(conn, message)
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func closeListenerOnCancel(ctx context.Context, listener net.Listener) (cancel func()) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

// ErrClosed is returned when a Conn method is called after Close.
var ErrClosed = errors.New("agent: connection closed")
