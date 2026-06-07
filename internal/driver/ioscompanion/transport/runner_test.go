package transport

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"
)

var (
	_ Companion  = (*runnerCompanion)(nil)
	_ TextEditor = (*runnerCompanion)(nil)
)

// scriptedReply maps a method name to the raw JSON object the fake server writes
// back as the "result" field. A method absent from the script gets an empty
// object result.
type scriptedReply map[string]string

// fakeServer is an in-process runner stand-in. It accepts a single connection,
// records every decoded request, and answers from a scripted table.
type fakeServer struct {
	listener net.Listener
	address  string

	mutex    sync.Mutex
	requests []runnerRequest
}

func startFakeServer(t *testing.T, script scriptedReply) *fakeServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &fakeServer{listener: listener, address: listener.Addr().String()}
	go server.serve(script)
	t.Cleanup(func() { listener.Close() })
	return server
}

func (s *fakeServer) serve(script scriptedReply) {
	conn, err := s.listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var request runnerRequest
		if err := json.Unmarshal(line, &request); err != nil {
			return
		}
		s.mutex.Lock()
		s.requests = append(s.requests, request)
		s.mutex.Unlock()

		result := script[request.Method]
		if result == "" {
			result = "{}"
		}
		response := `{"id":` + strconv.Itoa(request.ID) + `,"result":` + result + "}\n"
		if _, err := conn.Write([]byte(response)); err != nil {
			return
		}
	}
}

func (s *fakeServer) recorded() []runnerRequest {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	out := make([]runnerRequest, len(s.requests))
	copy(out, s.requests)
	return out
}

func dialFake(t *testing.T, server *fakeServer, bundleID string) Companion {
	t.Helper()
	companion, err := DialRunner(server.address, "UDID-1234", bundleID)
	if err != nil {
		t.Fatalf("DialRunner: %v", err)
	}
	t.Cleanup(func() { companion.Close() })
	return companion
}

func TestSnapshotReMarshalsElements(t *testing.T) {
	server := startFakeServer(t, scriptedReply{
		"snapshot": `{"elements":[{"role":"button"},{"role":"text"}]}`,
	})
	companion := dialFake(t, server, "com.example.app")

	got, err := companion.AccessibilityInfo(context.Background())
	if err != nil {
		t.Fatalf("AccessibilityInfo: %v", err)
	}
	want := `[{"role":"button"},{"role":"text"}]`
	if got != want {
		t.Fatalf("elements = %s, want %s", got, want)
	}

	requests := server.recorded()
	if len(requests) != 1 {
		t.Fatalf("recorded %d requests, want 1", len(requests))
	}
	if requests[0].Method != "snapshot" {
		t.Fatalf("method = %s, want snapshot", requests[0].Method)
	}
	if requests[0].Params["bundleId"] != "com.example.app" {
		t.Fatalf("bundleId = %v, want com.example.app", requests[0].Params["bundleId"])
	}
}

func TestGestureEncodesDoubleTapStream(t *testing.T) {
	server := startFakeServer(t, nil)
	companion := dialFake(t, server, "com.example.app")

	err := companion.SendHID(context.Background(),
		TouchDown(10, 20),
		TouchUp(10, 20),
		Delay(60),
		TouchDown(10, 20),
		TouchUp(10, 20),
	)
	if err != nil {
		t.Fatalf("SendHID: %v", err)
	}

	requests := server.recorded()
	if len(requests) != 1 || requests[0].Method != "gesture" {
		t.Fatalf("requests = %+v, want one gesture", requests)
	}
	events, ok := requests[0].Params["events"].([]any)
	if !ok {
		t.Fatalf("events not an array: %T", requests[0].Params["events"])
	}
	want := []map[string]any{
		{"kind": "touchDown", "x": 10.0, "y": 20.0},
		{"kind": "touchUp", "x": 10.0, "y": 20.0},
		{"kind": "delay", "milliseconds": 60.0},
		{"kind": "touchDown", "x": 10.0, "y": 20.0},
		{"kind": "touchUp", "x": 10.0, "y": 20.0},
	}
	if len(events) != len(want) {
		t.Fatalf("got %d events, want %d", len(events), len(want))
	}
	for index, event := range events {
		if !reflect.DeepEqual(event, map[string]any(want[index])) {
			t.Fatalf("event %d = %v, want %v", index, event, want[index])
		}
	}
}

func TestSendHIDRejectsKeyEvents(t *testing.T) {
	server := startFakeServer(t, nil)
	companion := dialFake(t, server, "com.example.app")

	err := companion.SendHID(context.Background(), KeyDown(4), KeyUp(4))
	if err == nil {
		t.Fatal("expected error for key HID events")
	}
	if errors.Is(err, ErrCompanionUnavailable) {
		t.Fatalf("key rejection should not be a transport error: %v", err)
	}
	if requests := server.recorded(); len(requests) != 0 {
		t.Fatalf("expected nothing sent, got %+v", requests)
	}
}

func TestScreenshotBase64RoundTrip(t *testing.T) {
	original := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a}
	encoded := base64.StdEncoding.EncodeToString(original)
	server := startFakeServer(t, scriptedReply{
		"screenshot": `{"pngBase64":"` + encoded + `"}`,
	})
	companion := dialFake(t, server, "com.example.app")

	data, format, err := companion.Screenshot(context.Background())
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if format != "png" {
		t.Fatalf("format = %s, want png", format)
	}
	if !reflect.DeepEqual(data, original) {
		t.Fatalf("data = %v, want %v", data, original)
	}
}

func TestDescribeMapsScreenDescription(t *testing.T) {
	server := startFakeServer(t, scriptedReply{
		"describe": `{"widthPoints":390,"heightPoints":844,"scale":3.0}`,
	})
	companion := dialFake(t, server, "com.example.app")

	description, err := companion.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	want := ScreenDescription{WidthPoints: 390, HeightPoints: 844, Scale: 3.0}
	if description != want {
		t.Fatalf("description = %+v, want %+v", description, want)
	}
}

func TestTextEditorMethods(t *testing.T) {
	server := startFakeServer(t, nil)
	companion := dialFake(t, server, "com.example.app")
	editor := companion.(TextEditor)

	if err := editor.InputText(context.Background(), "hello"); err != nil {
		t.Fatalf("InputText: %v", err)
	}
	if err := editor.EraseText(context.Background(), 3); err != nil {
		t.Fatalf("EraseText: %v", err)
	}
	if err := editor.PressKey(context.Background(), "Enter"); err != nil {
		t.Fatalf("PressKey: %v", err)
	}

	requests := server.recorded()
	if len(requests) != 3 {
		t.Fatalf("recorded %d requests, want 3", len(requests))
	}
	if requests[0].Method != "typeText" || requests[0].Params["text"] != "hello" || requests[0].Params["replace"] != true {
		t.Fatalf("typeText request = %+v", requests[0])
	}
	if requests[1].Method != "eraseText" || requests[1].Params["count"] != 3.0 {
		t.Fatalf("eraseText request = %+v", requests[1])
	}
	if requests[2].Method != "pressKey" || requests[2].Params["key"] != "return" {
		t.Fatalf("pressKey request = %+v", requests[2])
	}
}

func TestPressKeyRejectsUnknownKey(t *testing.T) {
	server := startFakeServer(t, nil)
	companion := dialFake(t, server, "com.example.app")
	editor := companion.(TextEditor)

	err := editor.PressKey(context.Background(), "home")
	if err == nil {
		t.Fatal("expected error for unsupported key")
	}
	if errors.Is(err, ErrCompanionUnavailable) {
		t.Fatalf("unsupported key should not be a transport error: %v", err)
	}
	if requests := server.recorded(); len(requests) != 0 {
		t.Fatalf("expected nothing sent, got %+v", requests)
	}
}

func TestLaunchTerminateMapping(t *testing.T) {
	server := startFakeServer(t, nil)
	companion := dialFake(t, server, "com.example.app")

	if err := companion.Launch(context.Background(), "com.example.target", true); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := companion.Terminate(context.Background(), "com.example.target"); err != nil {
		t.Fatalf("Terminate: %v", err)
	}

	requests := server.recorded()
	if len(requests) != 2 {
		t.Fatalf("recorded %d requests, want 2", len(requests))
	}
	if requests[0].Method != "launch" ||
		requests[0].Params["bundleId"] != "com.example.target" ||
		requests[0].Params["foregroundIfRunning"] != true {
		t.Fatalf("launch request = %+v", requests[0])
	}
	if requests[1].Method != "terminate" || requests[1].Params["bundleId"] != "com.example.target" {
		t.Fatalf("terminate request = %+v", requests[1])
	}
}

func TestListAppsStateMapping(t *testing.T) {
	cases := []struct {
		state string
		want  ProcessState
	}{
		{"foreground", ProcessStateRunning},
		{"background", ProcessStateRunning},
		{"notRunning", ProcessStateNotRunning},
		{"unknown", ProcessStateUnknown},
	}
	for _, testCase := range cases {
		server := startFakeServer(t, scriptedReply{
			"appState": `{"state":"` + testCase.state + `"}`,
		})
		companion := dialFake(t, server, "com.example.app")

		apps, err := companion.ListApps(context.Background())
		if err != nil {
			t.Fatalf("ListApps(%s): %v", testCase.state, err)
		}
		if len(apps) != 1 {
			t.Fatalf("ListApps(%s) returned %d apps, want 1", testCase.state, len(apps))
		}
		app := apps[0]
		if app.BundleID != "com.example.app" || app.InstallType != "user" {
			t.Fatalf("app fields = %+v", app)
		}
		if app.ProcessState != testCase.want {
			t.Fatalf("state %s -> %v, want %v", testCase.state, app.ProcessState, testCase.want)
		}

		requests := server.recorded()
		if len(requests) != 1 || requests[0].Method != "appState" || requests[0].Params["bundleId"] != "com.example.app" {
			t.Fatalf("appState request = %+v", requests)
		}
	}
}

func TestServerErrorIsNotSentinel(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var request runnerRequest
		json.Unmarshal(line, &request)
		conn.Write([]byte(`{"id":` + strconv.Itoa(request.ID) + `,"error":"field not focused"}` + "\n"))
	}()

	companion, err := DialRunner(listener.Addr().String(), "UDID", "com.example.app")
	if err != nil {
		t.Fatalf("DialRunner: %v", err)
	}
	t.Cleanup(func() { companion.Close() })

	_, err = companion.Describe(context.Background())
	if err == nil {
		t.Fatal("expected server error")
	}
	if errors.Is(err, ErrCompanionUnavailable) {
		t.Fatalf("server error must not wrap the sentinel: %v", err)
	}
}

func TestServerClosesMidCallIsSentinel(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		// Read the request, then drop the connection without replying.
		bufio.NewReader(conn).ReadBytes('\n')
		conn.Close()
	}()

	companion, err := DialRunner(listener.Addr().String(), "UDID", "com.example.app")
	if err != nil {
		t.Fatalf("DialRunner: %v", err)
	}
	t.Cleanup(func() { companion.Close() })

	_, err = companion.Describe(context.Background())
	if err == nil {
		t.Fatal("expected error when server closes mid-call")
	}
	if !errors.Is(err, ErrCompanionUnavailable) {
		t.Fatalf("dropped connection must wrap the sentinel: %v", err)
	}
}

func TestContextCancellationUnblocksCall(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		// Accept and hold the connection open, never replying.
		bufio.NewReader(conn).ReadBytes('\n')
		<-make(chan struct{})
	}()

	companion, err := DialRunner(listener.Addr().String(), "UDID", "com.example.app")
	if err != nil {
		t.Fatalf("DialRunner: %v", err)
	}
	t.Cleanup(func() { companion.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	done := make(chan error, 1)
	go func() {
		_, callErr := companion.Describe(ctx)
		done <- callErr
	}()

	select {
	case callErr := <-done:
		if callErr == nil {
			t.Fatal("expected cancellation error")
		}
		if !errors.Is(callErr, ErrCompanionUnavailable) {
			t.Fatalf("cancellation error must wrap the sentinel: %v", callErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled call did not return within 2s")
	}
}

func TestResponseIDMismatchIsSentinel(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		bufio.NewReader(conn).ReadBytes('\n')
		// Reply with an id that cannot match any request.
		conn.Write([]byte(`{"id":9999,"result":{}}` + "\n"))
	}()

	companion, err := DialRunner(listener.Addr().String(), "UDID", "com.example.app")
	if err != nil {
		t.Fatalf("DialRunner: %v", err)
	}
	t.Cleanup(func() { companion.Close() })

	_, err = companion.Describe(context.Background())
	if err == nil {
		t.Fatal("expected id-mismatch error")
	}
	if !errors.Is(err, ErrCompanionUnavailable) {
		t.Fatalf("id mismatch must wrap the sentinel: %v", err)
	}
}

func TestTypeTextAppendsWithoutReplace(t *testing.T) {
	server := startFakeServer(t, nil)
	companion := dialFake(t, server, "com.example.app")
	typer := companion.(TextTyper)

	if err := typer.TypeText(context.Background(), "héllo 🌟", false); err != nil {
		t.Fatalf("TypeText: %v", err)
	}

	requests := server.recorded()
	if len(requests) != 1 {
		t.Fatalf("recorded %d requests, want 1", len(requests))
	}
	request := requests[0]
	if request.Method != "typeText" || request.Params["text"] != "héllo 🌟" || request.Params["replace"] != false {
		t.Fatalf("typeText request = %+v", request)
	}
}
