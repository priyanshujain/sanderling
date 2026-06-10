package sidecar

import (
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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

	longPresses    []int32
	doubleTaps     []int32
	swipes         []*driverpb.SwipeRequest
	erases         []int32
	pressedKeys    []string
	metricsBundles []string
	logsRequests   []*driverpb.RecentLogsRequest
	logEntries     []*driverpb.LogEntry
	metrics        *driverpb.MetricsResponse

	healthError error
	tapError    error
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
	if s.tapError != nil {
		return nil, s.tapError
	}
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

func (s *fakeServer) LongPress(_ context.Context, point *driverpb.Point) (*driverpb.Empty, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.longPresses = append(s.longPresses, point.GetX(), point.GetY())
	return &driverpb.Empty{}, nil
}

func (s *fakeServer) DoubleTap(_ context.Context, point *driverpb.Point) (*driverpb.Empty, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.doubleTaps = append(s.doubleTaps, point.GetX(), point.GetY())
	return &driverpb.Empty{}, nil
}

func (s *fakeServer) Swipe(_ context.Context, request *driverpb.SwipeRequest) (*driverpb.Empty, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.swipes = append(s.swipes, request)
	return &driverpb.Empty{}, nil
}

func (s *fakeServer) EraseText(_ context.Context, request *driverpb.EraseTextRequest) (*driverpb.Empty, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.erases = append(s.erases, request.GetCharacterCount())
	return &driverpb.Empty{}, nil
}

func (s *fakeServer) PressKey(_ context.Context, request *driverpb.PressKeyRequest) (*driverpb.Empty, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.pressedKeys = append(s.pressedKeys, request.GetKey())
	return &driverpb.Empty{}, nil
}

func (s *fakeServer) RecentLogs(_ context.Context, request *driverpb.RecentLogsRequest) (*driverpb.LogEntries, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.logsRequests = append(s.logsRequests, request)
	return &driverpb.LogEntries{Entries: s.logEntries}, nil
}

func (s *fakeServer) Metrics(_ context.Context, request *driverpb.MetricsRequest) (*driverpb.MetricsResponse, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.metricsBundles = append(s.metricsBundles, request.GetBundleId())
	if s.metrics != nil {
		return s.metrics, nil
	}
	return &driverpb.MetricsResponse{}, nil
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

func (s *fakeServer) Snapshot(_ context.Context, _ *driverpb.Empty) (*driverpb.SnapshotResponse, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return &driverpb.SnapshotResponse{
		Hierarchy:  &driverpb.HierarchyJSON{Json: s.hierarchy},
		Screenshot: &driverpb.Image{Png: s.imagePNG, Width: s.imageWidth, Height: s.imageHeight},
	}, nil
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
	state.fake.mutex.Lock()
	state.fake.healthReady = false
	state.fake.mutex.Unlock()
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

func TestClient_Health_SurfacesRPCError(t *testing.T) {
	state := newHarness(t)
	state.fake.mutex.Lock()
	state.fake.healthError = status.Error(codes.Unavailable, "boom")
	state.fake.mutex.Unlock()
	client, err := Dial(state.address)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if _, err := client.Health(context.Background()); err == nil {
		t.Fatal("expected Health to surface the RPC error, got nil")
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

func TestClient_LaunchClearStateReinstallsOnAndroid(t *testing.T) {
	state := newHarness(t)
	client, _ := Dial(state.address)
	defer client.Close()
	client.SetPlatform("android")
	client.SetClearStateReinstall("serial123", "/tmp/app.apk", io.Discard)

	var got struct {
		serial, bundleID, apkPath string
	}
	called := 0
	client.reinstallApp = func(_ context.Context, serial, bundleID, apkPath string, _ io.Writer) error {
		called++
		got.serial, got.bundleID, got.apkPath = serial, bundleID, apkPath
		return nil
	}

	if err := client.Launch(context.Background(), "app.folio", true, nil); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("reinstall called %d times, want 1", called)
	}
	if got.serial != "serial123" || got.bundleID != "app.folio" || got.apkPath != "/tmp/app.apk" {
		t.Errorf("reinstall args wrong: %+v", got)
	}
	// The sidecar must not also clear: the host reinstall already reset state.
	if state.fake.clearState {
		t.Error("sidecar clearState should be false after host reinstall")
	}
	if state.fake.launchedBundleID != "app.folio" {
		t.Errorf("launched bundle wrong: %q", state.fake.launchedBundleID)
	}
}

func TestClient_LaunchClearStateReinstallFailureAborts(t *testing.T) {
	state := newHarness(t)
	client, _ := Dial(state.address)
	defer client.Close()
	client.SetPlatform("android")
	client.SetClearStateReinstall("", "/tmp/app.apk", io.Discard)
	client.reinstallApp = func(_ context.Context, _, _, _ string, _ io.Writer) error {
		return context.DeadlineExceeded
	}

	if err := client.Launch(context.Background(), "app.folio", true, nil); err == nil {
		t.Fatal("expected launch to fail when reinstall fails")
	}
	if state.fake.launchedBundleID == "app.folio" {
		t.Error("sidecar launch should not be called after a failed reinstall")
	}
}

func TestClient_LaunchClearStateWithoutApkPathUsesSidecarClear(t *testing.T) {
	state := newHarness(t)
	client, _ := Dial(state.address)
	defer client.Close()
	client.SetPlatform("android")
	client.reinstallApp = func(_ context.Context, _, _, _ string, _ io.Writer) error {
		t.Fatal("reinstall should not run without an apk path")
		return nil
	}

	if err := client.Launch(context.Background(), "app.folio", true, nil); err != nil {
		t.Fatal(err)
	}
	if !state.fake.clearState {
		t.Error("without an apk path the sidecar clearState path must remain")
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

// TestClient_ScalarForwardingRPCs pins that each single-payload RPC forwards
// its argument unchanged to the captured request: a dropped/mistyped text,
// idle duration, erase count, or logical key would change device behavior with
// no other field to disambiguate it.
func TestClient_ScalarForwardingRPCs(t *testing.T) {
	tests := []struct {
		name  string
		call  func(c *Client) error
		check func(t *testing.T, s *fakeServer)
	}{
		{"InputText", func(c *Client) error { return c.InputText(context.Background(), "hello world") }, func(t *testing.T, s *fakeServer) {
			if len(s.inputs) != 1 || s.inputs[0] != "hello world" {
				t.Errorf("inputs wrong: %v", s.inputs)
			}
		}},
		{"WaitForIdle", func(c *Client) error { return c.WaitForIdle(context.Background(), 250*time.Millisecond) }, func(t *testing.T, s *fakeServer) {
			if len(s.idleMillis) != 1 || s.idleMillis[0] != 250 {
				t.Errorf("idleMillis wrong: %v", s.idleMillis)
			}
		}},
		{"EraseText", func(c *Client) error { return c.EraseText(context.Background(), 7) }, func(t *testing.T, s *fakeServer) {
			if len(s.erases) != 1 || s.erases[0] != 7 {
				t.Errorf("erase count wrong: %v", s.erases)
			}
		}},
		{"PressKey", func(c *Client) error { return c.PressKey(context.Background(), "back") }, func(t *testing.T, s *fakeServer) {
			if len(s.pressedKeys) != 1 || s.pressedKeys[0] != "back" {
				t.Errorf("pressed key wrong: %v", s.pressedKeys)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newHarness(t)
			client, _ := Dial(state.address)
			defer client.Close()
			if err := tt.call(client); err != nil {
				t.Fatal(err)
			}
			tt.check(t, state.fake)
		})
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

func TestClient_SnapshotPairsHierarchyAndScreenshot(t *testing.T) {
	state := newHarness(t)
	client, _ := Dial(state.address)
	defer client.Close()

	hierarchyJSON, image, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hierarchyJSON != `{"x":1}` {
		t.Errorf("hierarchy wrong: %q", hierarchyJSON)
	}
	if image.Width != 1080 || image.Height != 2340 || len(image.PNG) != 1 {
		t.Errorf("image wrong: %+v", image)
	}
}

// TestClient_RPCErrorSurfaces is the representative check that a gRPC error
// status from the sidecar propagates out of an action RPC instead of being
// swallowed into a nil error. Per-method coverage of the sidecar-side status
// mapping lives in the sidecar server tests.
func TestClient_RPCErrorSurfaces(t *testing.T) {
	state := newHarness(t)
	state.fake.mutex.Lock()
	state.fake.tapError = status.Error(codes.Internal, "device offline")
	state.fake.mutex.Unlock()
	client, _ := Dial(state.address)
	defer client.Close()

	err := client.Tap(context.Background(), 1, 2)
	if err == nil {
		t.Fatal("expected Tap to surface the gRPC error, got nil")
	}
	if status.Code(err) != codes.Internal {
		t.Errorf("expected INTERNAL code, got %v", status.Code(err))
	}
}

// TestClient_DoubleTapSelectorFiresTwice confirms the selector fallback
// composes exactly two taps; a broken composition would single-tap and the
// double-tap gesture would silently degrade.
func TestClient_DoubleTapSelectorFiresTwice(t *testing.T) {
	state := newHarness(t)
	client, _ := Dial(state.address)
	defer client.Close()

	if err := client.DoubleTapSelector(context.Background(), "id:home"); err != nil {
		t.Fatal(err)
	}
	state.fake.mutex.Lock()
	defer state.fake.mutex.Unlock()
	if len(state.fake.tapSelectors) != 2 || state.fake.tapSelectors[0] != "id:home" || state.fake.tapSelectors[1] != "id:home" {
		t.Errorf("expected two taps on id:home, got %v", state.fake.tapSelectors)
	}
}

// TestClient_DoubleTapSelectorCancelBetweenTaps confirms a context cancelled
// during the inter-tap gap fires exactly one tap and returns ctx.Err() - it
// must neither double-fire nor swallow the cancellation.
func TestClient_DoubleTapSelectorCancelBetweenTaps(t *testing.T) {
	state := newHarness(t)
	client, _ := Dial(state.address)
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			state.fake.mutex.Lock()
			n := len(state.fake.tapSelectors)
			state.fake.mutex.Unlock()
			if n >= 1 {
				cancel()
				return
			}
		}
	}()

	err := client.DoubleTapSelector(ctx, "id:home")
	if err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("expected context error, got %v", err)
	}
	state.fake.mutex.Lock()
	defer state.fake.mutex.Unlock()
	if len(state.fake.tapSelectors) != 1 {
		t.Errorf("expected exactly one tap before cancellation, got %d", len(state.fake.tapSelectors))
	}
}

// TestClient_SwipeForwardsEndpointsInOrder pins down that From keeps the start
// point and To the end point. A from/to transposition would send the swipe in
// the reverse direction on-device while every other assertion still passed.
func TestClient_SwipeForwardsEndpointsInOrder(t *testing.T) {
	state := newHarness(t)
	client, _ := Dial(state.address)
	defer client.Close()

	if err := client.Swipe(context.Background(), 10, 20, 300, 400, 150*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if len(state.fake.swipes) != 1 {
		t.Fatalf("expected 1 swipe, got %d", len(state.fake.swipes))
	}
	got := state.fake.swipes[0]
	if got.GetFrom().GetX() != 10 || got.GetFrom().GetY() != 20 {
		t.Errorf("from wrong: %+v", got.GetFrom())
	}
	if got.GetTo().GetX() != 300 || got.GetTo().GetY() != 400 {
		t.Errorf("to wrong: %+v", got.GetTo())
	}
	if got.GetDurationMillis() != 150 {
		t.Errorf("duration wrong: %d", got.GetDurationMillis())
	}
}

// TestClient_PointRPCsForwardCoordinates guards against x/y swaps in the point
// RPCs that have no other field to disambiguate which axis is which.
func TestClient_PointRPCsForwardCoordinates(t *testing.T) {
	tests := []struct {
		name string
		call func(c *Client) error
		got  func(s *fakeServer) []int32
	}{
		{"LongPress", func(c *Client) error { return c.LongPress(context.Background(), 12, 34) }, func(s *fakeServer) []int32 { return s.longPresses }},
		{"DoubleTap", func(c *Client) error { return c.DoubleTap(context.Background(), 12, 34) }, func(s *fakeServer) []int32 { return s.doubleTaps }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newHarness(t)
			client, _ := Dial(state.address)
			defer client.Close()
			if err := tt.call(client); err != nil {
				t.Fatal(err)
			}
			got := tt.got(state.fake)
			if len(got) != 2 || got[0] != 12 || got[1] != 34 {
				t.Errorf("coordinates wrong: %v", got)
			}
		})
	}
}

// TestClient_MetricsMapsResponseFields catches CPU/heap/total being read off
// the wrong proto field, which would mislabel a memory regression as CPU.
func TestClient_MetricsMapsResponseFields(t *testing.T) {
	state := newHarness(t)
	state.fake.mutex.Lock()
	state.fake.metrics = &driverpb.MetricsResponse{CpuPercent: 12.5, HeapBytes: 100, TotalMemoryBytes: 200}
	state.fake.mutex.Unlock()
	client, _ := Dial(state.address)
	defer client.Close()

	got, err := client.Metrics(context.Background(), "com.example")
	if err != nil {
		t.Fatal(err)
	}
	if got.CPUPercent != 12.5 || got.HeapBytes != 100 || got.TotalMemoryBytes != 200 {
		t.Errorf("metrics mapping wrong: %+v", got)
	}
	if len(state.fake.metricsBundles) != 1 || state.fake.metricsBundles[0] != "com.example" {
		t.Errorf("bundle id not forwarded: %v", state.fake.metricsBundles)
	}
}

// TestClient_RecentLogsSinceBranches pins both arms of the zero-time guard: a
// zero time must send sinceMillis=0 (don't accidentally floor to epoch via
// UnixMilli of a zero Time, which is a huge negative number), a real time must
// forward its unix-millis. The response decode is checked alongside.
func TestClient_RecentLogsSinceBranches(t *testing.T) {
	realTime := time.UnixMilli(1_700_000_000_000)
	tests := []struct {
		name      string
		since     time.Time
		wantMilli int64
	}{
		{"zero time forwards 0", time.Time{}, 0},
		{"real time forwards unix millis", realTime, realTime.UnixMilli()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newHarness(t)
			state.fake.mutex.Lock()
			state.fake.logEntries = []*driverpb.LogEntry{{UnixMillis: 5, Level: "E", Tag: "t", Message: "boom"}}
			state.fake.mutex.Unlock()
			client, _ := Dial(state.address)
			defer client.Close()

			logs, err := client.RecentLogs(context.Background(), tt.since, "E")
			if err != nil {
				t.Fatal(err)
			}
			state.fake.mutex.Lock()
			defer state.fake.mutex.Unlock()
			req := state.fake.logsRequests[len(state.fake.logsRequests)-1]
			if req.GetSinceUnixMillis() != tt.wantMilli {
				t.Errorf("sinceUnixMillis = %d, want %d", req.GetSinceUnixMillis(), tt.wantMilli)
			}
			if req.GetLevelAtLeast() != "E" {
				t.Errorf("levelAtLeast = %q, want E", req.GetLevelAtLeast())
			}
			if len(logs) != 1 || logs[0].Level != "E" || logs[0].Message != "boom" || logs[0].UnixMillis != 5 || logs[0].Tag != "t" {
				t.Errorf("decoded logs wrong: %+v", logs)
			}
		})
	}
}
