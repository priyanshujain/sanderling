package inspect

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ServerOptions configures a new Server.
type ServerOptions struct {
	RunsDirectory string
	DevTarget     string
}

// Server holds the HTTP handlers for `sanderling inspect`.
type Server struct {
	options ServerOptions
	cache   *Cache
	watcher *Watcher
	assets  http.Handler
	dev     http.Handler
}

// NewServer constructs a Server. When options.DevTarget is non-empty the
// server reverse-proxies non-API GETs to it; otherwise it serves embedded
// assets from the dist FS.
func NewServer(options ServerOptions) (*Server, error) {
	server := &Server{
		options: options,
		cache:   NewCache(options.RunsDirectory),
		watcher: NewWatcher(options.RunsDirectory),
		assets:  spaHandler(Assets()),
	}
	if options.DevTarget != "" {
		proxy, err := newDevProxy(options.DevTarget)
		if err != nil {
			return nil, fmt.Errorf("dev proxy: %w", err)
		}
		server.dev = proxy
	}
	return server, nil
}

// Watcher exposes the runs-directory watcher so callers can run it under their
// own context.
func (s *Server) Watcher() *Watcher { return s.watcher }

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/runs", s.handleRunsList)
	mux.HandleFunc("/api/runs/", s.handleRunsTree)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/", s.handleAssets)
	return mux
}

func (s *Server) handleRunsList(responseWriter http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(responseWriter, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	summaries, err := Scan(s.options.RunsDirectory)
	if err != nil {
		http.Error(responseWriter, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(responseWriter, http.StatusOK, summaries)
}

var stepPathPattern = regexp.MustCompile(`^([a-zA-Z0-9._-]+)/steps/([^/]+)$`)
var screenshotPathPattern = regexp.MustCompile(`^([a-zA-Z0-9._-]+)/screenshots/([a-zA-Z0-9._-]+\.png)$`)
var runDetailPathPattern = regexp.MustCompile(`^([a-zA-Z0-9._-]+)/?$`)

func (s *Server) handleRunsTree(responseWriter http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(responseWriter, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rest := strings.TrimPrefix(request.URL.Path, "/api/runs/")
	if rest == "" {
		s.handleRunsList(responseWriter, request)
		return
	}
	if match := stepPathPattern.FindStringSubmatch(rest); match != nil {
		s.serveStep(responseWriter, match[1], match[2])
		return
	}
	if match := screenshotPathPattern.FindStringSubmatch(rest); match != nil {
		s.serveScreenshot(responseWriter, request, match[1], match[2])
		return
	}
	if match := runDetailPathPattern.FindStringSubmatch(rest); match != nil {
		s.serveDetail(responseWriter, match[1])
		return
	}
	http.NotFound(responseWriter, request)
}

func (s *Server) serveDetail(responseWriter http.ResponseWriter, id string) {
	detail, err := s.cache.Detail(id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(responseWriter, "run not found", http.StatusNotFound)
			return
		}
		http.Error(responseWriter, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(responseWriter, http.StatusOK, detail)
}

func (s *Server) serveStep(responseWriter http.ResponseWriter, id, indexText string) {
	index, err := strconv.Atoi(indexText)
	if err != nil {
		http.Error(responseWriter, "step index must be numeric", http.StatusBadRequest)
		return
	}
	run, err := s.cache.Open(id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(responseWriter, "run not found", http.StatusNotFound)
			return
		}
		http.Error(responseWriter, err.Error(), http.StatusInternalServerError)
		return
	}
	step, err := s.cache.Step(run, index)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(responseWriter, "step not found", http.StatusNotFound)
			return
		}
		http.Error(responseWriter, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(responseWriter, http.StatusOK, step)
}

func (s *Server) serveScreenshot(responseWriter http.ResponseWriter, request *http.Request, id, name string) {
	if !validRunID(id) {
		http.Error(responseWriter, "run not found", http.StatusNotFound)
		return
	}
	full := filepath.Join(s.options.RunsDirectory, id, "screenshots", name)
	http.ServeFile(responseWriter, request, full)
}

func (s *Server) handleEvents(responseWriter http.ResponseWriter, request *http.Request) {
	flusher, ok := responseWriter.(http.Flusher)
	if !ok {
		http.Error(responseWriter, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	responseWriter.Header().Set("Content-Type", "text/event-stream")
	responseWriter.Header().Set("Cache-Control", "no-cache")
	responseWriter.Header().Set("Connection", "keep-alive")
	responseWriter.WriteHeader(http.StatusOK)
	flusher.Flush()

	subscription := s.watcher.Subscribe()
	defer s.watcher.Unsubscribe(subscription)
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-request.Context().Done():
			return
		case _, ok := <-subscription:
			if !ok {
				return
			}
			fmt.Fprint(responseWriter, "event: runs.changed\ndata: {\"type\":\"runs.changed\"}\n\n")
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprint(responseWriter, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) handleAssets(responseWriter http.ResponseWriter, request *http.Request) {
	if strings.HasPrefix(request.URL.Path, "/api/") {
		http.NotFound(responseWriter, request)
		return
	}
	if s.dev != nil {
		s.dev.ServeHTTP(responseWriter, request)
		return
	}
	s.assets.ServeHTTP(responseWriter, request)
}

// spaHandler serves files from assets, falling back to index.html for
// unknown paths so the SPA router can take over.
func spaHandler(assets fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		clean := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		if clean == "" {
			serveIndex(responseWriter, request, assets)
			return
		}
		file, err := assets.Open(clean)
		if err != nil {
			serveIndex(responseWriter, request, assets)
			return
		}
		file.Close()
		fileServer.ServeHTTP(responseWriter, request)
	})
}

func serveIndex(responseWriter http.ResponseWriter, request *http.Request, assets fs.FS) {
	file, err := assets.Open("index.html")
	if err != nil {
		http.Error(responseWriter, "index.html missing from embedded assets", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	body, err := readAll(file)
	if err != nil {
		http.Error(responseWriter, err.Error(), http.StatusInternalServerError)
		return
	}
	responseWriter.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = responseWriter.Write(body)
}

func readAll(file fs.File) ([]byte, error) {
	const initialCapacity = 4 * 1024
	buffer := make([]byte, 0, initialCapacity)
	chunk := make([]byte, 4*1024)
	for {
		read, err := file.Read(chunk)
		if read > 0 {
			buffer = append(buffer, chunk[:read]...)
		}
		if err != nil {
			if errors.Is(err, fs.ErrInvalid) {
				return nil, err
			}
			break
		}
	}
	return buffer, nil
}

func writeJSON(responseWriter http.ResponseWriter, status int, payload any) {
	responseWriter.Header().Set("Content-Type", "application/json")
	responseWriter.WriteHeader(status)
	encoder := json.NewEncoder(responseWriter)
	_ = encoder.Encode(payload)
}

// ResolveRunsDirectory takes the optional positional argument and returns
// (runsDirectory, deepLinkID, error). When argument is "" it falls back
// to ./runs. When argument is a single run directory (has meta.json), the
// parent becomes runsDirectory and the basename becomes the deep-link id.
func ResolveRunsDirectory(argument string) (string, string, error) {
	if argument == "" {
		return "./runs", "", nil
	}
	if IsRunDirectory(argument) {
		cleaned := filepath.Clean(argument)
		parent := filepath.Dir(cleaned)
		base := filepath.Base(cleaned)
		return parent, base, nil
	}
	return argument, "", nil
}

