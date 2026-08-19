package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	readinessTimeout  = 5 * time.Second
	readinessInterval = 100 * time.Millisecond
)

// stubScriptPath is served from every implementation's own origin in place of a
// dependency that no longer answers.
const stubScriptPath = "/__corpus-sweep__/stub.js"

// deadDependency is polyfill.io, which was shut down after this corpus was
// pinned. One implementation loads it from its index.html; the request fails
// and the application works regardless, but it fails slowly, once per run, on
// every run. The corpus is served from this process, so the reference is
// rewritten to a local no-op on the way out.
var deadDependency = regexp.MustCompile(`https?://polyfill\.io/[^"'\s>]*`)

// staticServer serves the whole corpus tree on one implementation's port. The
// tree rather than the implementation's own directory, because an example that
// asks for a path above itself has to resolve the same way it would in the
// upstream repository. Only one implementation is ever visited on this port, so
// the origin still belongs to it alone.
type staticServer struct {
	implementation implementation
	listener       net.Listener
	server         *http.Server
}

func startStaticServer(
	corpusRoot string,
	target implementation,
) (*staticServer, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", target.Port))
	if err != nil {
		return nil, fmt.Errorf("serve %s: %w", target.Name, err)
	}
	server := &http.Server{Handler: corpusHandler(corpusRoot)}
	running := &staticServer{
		implementation: target,
		listener:       listener,
		server:         server,
	}
	go server.Serve(listener)
	return running, nil
}

func (s *staticServer) stop() {
	s.server.Close()
}

// waitReady confirms the served document answers before any run drives it. A
// wrong document path would otherwise reach the driver as a 404 page, and a
// sweep of 404 pages produces clean runs for every implementation.
//
// Only a transport error is retried. The listener is bound before the sweep
// starts, so a status that is not 200 is the server's answer about this
// document and waiting will not change it.
func (s *staticServer) waitReady(ctx context.Context) error {
	url := s.implementation.URL()
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(readinessTimeout)
	var lastErr error
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		response, err := client.Get(url)
		if err == nil {
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
			return fmt.Errorf(
				"%s answered %d, not the document",
				url,
				response.StatusCode,
			)
		}
		lastErr = err
		if time.Now().After(deadline) {
			return fmt.Errorf(
				"%s did not answer within %s: %w",
				url,
				readinessTimeout,
				lastErr,
			)
		}
		time.Sleep(readinessInterval)
	}
}

func corpusHandler(corpusRoot string) http.Handler {
	files := http.FileServer(http.Dir(corpusRoot))
	return http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			cleaned := path.Clean(
				"/" + strings.TrimPrefix(request.URL.Path, "/"),
			)
			if cleaned == stubScriptPath {
				writer.Header().Set("Content-Type", "application/javascript")
				io.WriteString(
					writer,
					"/* corpus-sweep: dependency removed upstream */\n",
				)
				return
			}
			if !strings.HasSuffix(cleaned, ".html") {
				files.ServeHTTP(writer, request)
				return
			}
			body, modified, err := readDocument(corpusRoot, cleaned)
			if err != nil {
				// Never the file server's fallback: it answers a document it
				// cannot read with a 200 directory listing, and a run driven at a
				// listing explores nothing and comes back clean.
				http.Error(writer, err.Error(), documentStatus(err))
				return
			}
			rewritten := deadDependency.ReplaceAll(body, []byte(stubScriptPath))
			http.ServeContent(
				writer,
				request,
				path.Base(cleaned),
				modified,
				bytes.NewReader(rewritten),
			)
		},
	)
}

func documentStatus(err error) int {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return http.StatusNotFound
	case errors.Is(err, fs.ErrPermission):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

func readDocument(corpusRoot, cleaned string) ([]byte, time.Time, error) {
	file, err := http.Dir(corpusRoot).Open(cleaned)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return nil, time.Time{}, fmt.Errorf(
			"%s is not a document",
			filepath.FromSlash(cleaned),
		)
	}
	body, err := io.ReadAll(file)
	if err != nil {
		return nil, time.Time{}, err
	}
	return body, info.ModTime(), nil
}
