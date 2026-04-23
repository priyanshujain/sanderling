package sidecar

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"

	driverpb "github.com/priyanshujain/sanderling/proto/driverpb"
)

type fakeServer struct {
	driverpb.UnimplementedDriverServer
	mutex sync.Mutex

	healthReady          bool
	healthCalls          int
	healthReadyAfterCall int

	launchedBundleID string
	clearState       bool
	terminateCalls   int
	taps             []int32
	tapSelectors     []string
	inputs           []string
	idleMillis       []int64
	hierarchy        string
	imagePNG         []byte
	imageWidth       int32
	imageHeight      int32

	healthError error
}

func (s *fakeServer) Health(_ context.Context, _ *driverpb.Empty) (*driverpb.HealthStatus, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.healthCalls++
	if s.healthError != nil {
		return nil, s.healthError
	}
	ready := s.healthReady
	if s.healthReadyAfterCall > 0 && s.healthCalls >= s.healthReadyAfterCall {
		ready = true
	}
	return &driverpb.HealthStatus{Ready: ready, Version: "test", Platform: "android"}, nil
}

func (s *fakeServer) Launch(_ context.Context, request *driverpb.LaunchRequest) (*driverpb.Empty, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.launchedBundleID = request.GetBundleId()
	s.clearState = request.GetClearState()
	return &driverpb.Empty{}, nil
}

func (s *fakeServer) Terminate(_ context.Context, _ *driverpb.Empty) (*driverpb.Empty, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.terminateCalls++
	return &driverpb.Empty{}, nil
}

func (s *fakeServer) Tap(_ context.Context, point *driverpb.Point) (*driverpb.Empty, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.taps = append(s.taps, point.GetX(), point.GetY())
	return &driverpb.Empty{}, nil
}

func (s *fakeServer) TapSelector(_ context.Context, selector *driverpb.Selector) (*driverpb.Empty, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.tapSelectors = append(s.tapSelectors, selector.GetValue())
	return &driverpb.Empty{}, nil
}

func (s *fakeServer) InputText(_ context.Context, text *driverpb.Text) (*driverpb.Empty, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.inputs = append(s.inputs, text.GetValue())
	return &driverpb.Empty{}, nil
}

func (s *fakeServer) WaitForIdle(_ context.Context, duration *driverpb.Duration) (*driverpb.Empty, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.idleMillis = append(s.idleMillis, duration.GetMillis())
	return &driverpb.Empty{}, nil
}

func (s *fakeServer) Hierarchy(_ context.Context, _ *driverpb.Empty) (*driverpb.HierarchyJSON, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return &driverpb.HierarchyJSON{Json: s.hierarchy}, nil
}

func (s *fakeServer) Screenshot(_ context.Context, _ *driverpb.Empty) (*driverpb.Image, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return &driverpb.Image{Png: s.imagePNG, Width: s.imageWidth, Height: s.imageHeight}, nil
}

type harness struct {
	server  *grpc.Server
	fake    *fakeServer
	address string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	fake := &fakeServer{healthReady: true, hierarchy: `{"x":1}`, imagePNG: []byte{0xFF}, imageWidth: 1080, imageHeight: 2340}
	driverpb.RegisterDriverServer(server, fake)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	return &harness{server: server, fake: fake, address: listener.Addr().String()}
}

func TestClient_HealthRoundTrip(t *testing.T) {
	state := newHarness(t)
	client, err := Dial(state.address)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	got, err := client.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Ready || got.Version != "test" || got.Platform != "android" {
		t.Errorf("unexpected health: %+v", got)
	}
}

func TestClient_WaitForHealth_PollsUntilReady(t *testing.T) {
	state := newHarness(t)
	state.fake.mutex.Lock()
	state.fake.healthReady = false
	state.fake.healthReadyAfterCall = 2
	state.fake.mutex.Unlock()
	client, err := Dial(state.address)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.WaitForHealth(ctx, 25*time.Millisecond); err != nil {
		t.Fatalf("WaitForHealth: %v", err)
	}
	state.fake.mutex.Lock()
	defer state.fake.mutex.Unlock()
	if state.fake.healthCalls < 2 {
		t.Errorf("expected at least 2 health polls, got %d", state.fake.healthCalls)
	}
}

func TestClient_WaitForHealth_HonorsContext(t *testing.T) {
	state := newHarness(t)
	state.fake.healthReady = false
	client, err := Dial(state.address)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err = client.WaitForHealth(ctx, 25*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("expected context error, got %v", err)
	}
}

func TestClient_LaunchAndTerminate(t *testing.T) {
	state := newHarness(t)
	client, _ := Dial(state.address)
	defer client.Close()

	if err := client.Launch(context.Background(), "com.example", true, nil); err != nil {
		t.Fatal(err)
	}
	if state.fake.launchedBundleID != "com.example" || !state.fake.clearState {
		t.Errorf("launch payload wrong: %+v", state.fake)
	}
	if err := client.Terminate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if state.fake.terminateCalls != 1 {
		t.Errorf("terminate calls: %d", state.fake.terminateCalls)
	}
}

func TestClient_TapAndTapSelector(t *testing.T) {
	state := newHarness(t)
	client, _ := Dial(state.address)
	defer client.Close()

	if err := client.Tap(context.Background(), 100, 250); err != nil {
		t.Fatal(err)
	}
	if len(state.fake.taps) != 2 || state.fake.taps[0] != 100 || state.fake.taps[1] != 250 {
		t.Errorf("tap coordinates wrong: %v", state.fake.taps)
	}

	if err := client.TapSelector(context.Background(), "id:home"); err != nil {
		t.Fatal(err)
	}
	if len(state.fake.tapSelectors) != 1 || state.fake.tapSelectors[0] != "id:home" {
		t.Errorf("selectors wrong: %v", state.fake.tapSelectors)
	}
}

func TestClient_InputText(t *testing.T) {
	state := newHarness(t)
	client, _ := Dial(state.address)
	defer client.Close()

	if err := client.InputText(context.Background(), "hello world"); err != nil {
		t.Fatal(err)
	}
	if len(state.fake.inputs) != 1 || state.fake.inputs[0] != "hello world" {
		t.Errorf("inputs wrong: %v", state.fake.inputs)
	}
}

func TestClient_HierarchyAndScreenshot(t *testing.T) {
	state := newHarness(t)
	client, _ := Dial(state.address)
	defer client.Close()

	hierarchy, err := client.Hierarchy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hierarchy != `{"x":1}` {
		t.Errorf("hierarchy wrong: %q", hierarchy)
	}

	image, err := client.Screenshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if image.Width != 1080 || image.Height != 2340 || len(image.PNG) != 1 {
		t.Errorf("image wrong: %+v", image)
	}
}

func TestClient_WaitForIdleForwardsMillis(t *testing.T) {
	state := newHarness(t)
	client, _ := Dial(state.address)
	defer client.Close()

	if err := client.WaitForIdle(context.Background(), 250*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if len(state.fake.idleMillis) != 1 || state.fake.idleMillis[0] != 250 {
		t.Errorf("idleMillis wrong: %v", state.fake.idleMillis)
	}
}
