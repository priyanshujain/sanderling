package inspect

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/priyanshujain/sanderling/internal/trace"
)

var testAssetsFS fs.FS = fstest.MapFS{
	"index.html": &fstest.MapFile{
		Data: []byte(`<!doctype html><html><body><div id="root"></div></body></html>`),
	},
}

func newFixtureServer(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	startedAt := time.Date(2026, 4, 17, 18, 0, 0, 0, time.UTC)
	endedAt := startedAt.Add(5 * time.Second)
	writeRun(t, root, "run-a", trace.Meta{
		StartedAt: startedAt,
		EndedAt:   &endedAt,
		SpecPath:  "spec.ts",
		Seed:      42,
		Platform:  "android",
		BundleID:  "com.example",
	}, []trace.Step{
		{Index: 1, Timestamp: startedAt, Screen: "Home"},
		{Index: 2, Timestamp: startedAt.Add(time.Second), Screen: "Home", Action: &trace.Action{Kind: "tap"}},
		{Index: 3, Timestamp: startedAt.Add(2 * time.Second), Screen: "Cart", Violations: []string{"propA"}},
	})
	writeRun(t, root, "run-b", trace.Meta{
		StartedAt: startedAt.Add(time.Hour),
	}, []trace.Step{
		{Index: 1, Timestamp: startedAt.Add(time.Hour)},
	})

	screenshotsDirectory := filepath.Join(root, "run-a", "screenshots")
	if err := os.MkdirAll(screenshotsDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	pngBody := []byte{0x89, 'P', 'N', 'G', 0, 1, 2, 3}
	if err := os.WriteFile(filepath.Join(screenshotsDirectory, "step-00001.png"), pngBody, 0o644); err != nil {
		t.Fatal(err)
	}

	server, err := NewServer(ServerOptions{RunsDirectory: root, AssetsFS: testAssetsFS})
	if err != nil {
		t.Fatal(err)
	}
	return server, root
}

func TestHandleRunsList_OrdersAndCountsViolations(t *testing.T) {
	server, _ := newFixtureServer(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var summaries []RunSummary
	if err := json.Unmarshal(recorder.Body.Bytes(), &summaries); err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("len = %d", len(summaries))
	}
	if summaries[0].ID != "run-b" {
		t.Errorf("first id = %q, want run-b (most recent)", summaries[0].ID)
	}
	if summaries[1].ViolationCount != 1 {
		t.Errorf("run-a violation count = %d, want 1", summaries[1].ViolationCount)
	}
	if !summaries[0].InProgress {
		t.Error("run-b should be in_progress (no ended_at)")
	}
}

func TestHandleRunDetail_DecodesMetaAndStepSummaries(t *testing.T) {
	server, _ := newFixtureServer(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/runs/run-a", nil)
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var detail RunDetail
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Meta.BundleID != "com.example" {
		t.Errorf("bundle = %q", detail.Meta.BundleID)
	}
	if len(detail.Steps) != 3 {
		t.Fatalf("steps len = %d", len(detail.Steps))
	}
	if detail.Steps[1].ActionKind != "tap" {
		t.Errorf("step 2 action = %q", detail.Steps[1].ActionKind)
	}
	if !detail.Steps[2].HasViolations {
		t.Error("step 3 should HasViolations")
	}
}

func TestHandleStep_ReturnsFullStep(t *testing.T) {
	server, _ := newFixtureServer(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/runs/run-a/steps/3", nil)
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var step trace.Step
	if err := json.Unmarshal(recorder.Body.Bytes(), &step); err != nil {
		t.Fatal(err)
	}
	if step.Index != 3 || step.Screen != "Cart" {
		t.Errorf("step = %+v", step)
	}
	if len(step.Violations) != 1 {
		t.Errorf("violations = %v", step.Violations)
	}
}

func TestHandleStep_ErrorCases(t *testing.T) {
	server, _ := newFixtureServer(t)
	cases := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{"unknown run", "/api/runs/nope", http.StatusNotFound},
		{"non-numeric step", "/api/runs/run-a/steps/abc", http.StatusBadRequest},
		{"out-of-range step", "/api/runs/run-a/steps/999", http.StatusNotFound},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, testCase.path, nil)
			server.Handler().ServeHTTP(recorder, request)
			if recorder.Code != testCase.wantStatus {
				t.Errorf("status = %d, want %d, body=%s", recorder.Code, testCase.wantStatus, recorder.Body.String())
			}
		})
	}
}

func TestScreenshot_ServesWhitelistedPNG(t *testing.T) {
	server, _ := newFixtureServer(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/runs/run-a/screenshots/step-00001.png", nil)
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if recorder.Body.Len() == 0 {
		t.Error("empty body")
	}
}

func TestScreenshot_RejectsTraversalAndBadNames(t *testing.T) {
	server, _ := newFixtureServer(t)
	cases := []string{
		"/api/runs/run-a/screenshots/../meta.json",
		"/api/runs/run-a/screenshots/..%2Fmeta.json",
		"/api/runs/run-a/screenshots/step-00001.txt",
		"/api/runs/run-a/screenshots/.png",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path, nil)
			server.Handler().ServeHTTP(recorder, request)
			if recorder.Code == http.StatusOK {
				t.Errorf("status = %d (should not be 200) body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestSSE_ReturnsWhenContextCanceled(t *testing.T) {
	server, _ := newFixtureServer(t)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	context, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(context, http.MethodGet, httpServer.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}

	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, response.Body)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not exit after context cancel")
	}
}

func TestDevProxy_ForwardsRequestBodyUnchanged(t *testing.T) {
	received := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("upstream read: %v", err)
		}
		received <- string(body)
		responseWriter.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	root := t.TempDir()
	server, err := NewServer(ServerOptions{RunsDirectory: root, DevTarget: upstream.URL})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body := strings.NewReader("hello world")
	request, err := http.NewRequest(http.MethodPost, httpServer.URL+"/anything", body)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Errorf("status = %d", response.StatusCode)
	}
	select {
	case got := <-received:
		if got != "hello world" {
			t.Errorf("upstream body = %q, want %q", got, "hello world")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("upstream never received the request")
	}
}

func TestAssets_FallbackToIndexHTML(t *testing.T) {
	server, _ := newFixtureServer(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/runs/some-id-that-only-the-spa-knows", nil)
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "<div id=\"app\"></div>") && !strings.Contains(body, "<div id=\"root\"></div>") {
		t.Errorf("expected SPA shell with #app or #root, got %q", body)
	}
}

func TestAssets_API404DoesNotFallThrough(t *testing.T) {
	server, _ := newFixtureServer(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/wat", nil)
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", recorder.Code)
	}
}

func TestResolveRunsDirectory(t *testing.T) {
	root := t.TempDir()
	writeRun(t, root, "r1", trace.Meta{StartedAt: time.Now().UTC()}, nil)

	runsDirectory, deepLink, err := ResolveRunsDirectory("")
	if err != nil || runsDirectory != "./runs" || deepLink != "" {
		t.Errorf("default = (%q, %q, %v)", runsDirectory, deepLink, err)
	}
	runsDirectory, deepLink, err = ResolveRunsDirectory(root)
	if err != nil || runsDirectory != root || deepLink != "" {
		t.Errorf("multi-run dir = (%q, %q, %v)", runsDirectory, deepLink, err)
	}
	singleRun := filepath.Join(root, "r1")
	runsDirectory, deepLink, err = ResolveRunsDirectory(singleRun)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(runsDirectory) != filepath.Clean(root) || deepLink != "r1" {
		t.Errorf("single-run dir = (%q, %q)", runsDirectory, deepLink)
	}
}

func TestDevProxy_ParsesTarget(t *testing.T) {
	if _, err := newDevProxy(":://bad-url"); err == nil {
		t.Error("expected parse error for invalid URL")
	}
	parsed, err := url.Parse(DevTarget)
	if err != nil || parsed.Host != "127.0.0.1:5173" {
		t.Errorf("DevTarget parsed wrong: %v %q", err, parsed.Host)
	}
}
