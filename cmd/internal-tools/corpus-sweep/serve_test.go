package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(body)
}

func TestStaticServer_ServesEachImplementationsDocumentFromItsOwnPort(
	t *testing.T,
) {
	root := wholeCorpus(t)
	planned, err := planImplementations(
		root,
		[]string{"angular-dart", "duel", "react"},
		freePortRange(t, 3),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range planned {
		server, err := startStaticServer(root, target)
		if err != nil {
			t.Fatal(err)
		}
		defer server.stop()
		if err := server.waitReady(context.Background()); err != nil {
			t.Fatalf("%s: %v", target.Name, err)
		}
		status, body := get(t, target.URL())
		if status != http.StatusOK ||
			!strings.Contains(body, "<title>"+target.Name+"</title>") {
			t.Errorf(
				"%s at %s: got %d %q",
				target.Name,
				target.URL(),
				status,
				body,
			)
		}
	}
}

func TestStaticServer_ReplacesTheDependencyThatNoLongerAnswers(t *testing.T) {
	root := wholeCorpus(t)
	document := filepath.Join(root, filepath.FromSlash(documentFor("aurelia")))
	body := `<!doctype html><script src="https://polyfill.io/v3/polyfill.min.js?features=Promise"></script>`
	if err := os.WriteFile(document, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	planned, err := planImplementations(
		root,
		[]string{"aurelia"},
		freePortRange(t, 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	server, err := startStaticServer(root, planned[0])
	if err != nil {
		t.Fatal(err)
	}
	defer server.stop()

	status, served := get(t, planned[0].URL())
	if status != http.StatusOK {
		t.Fatalf("document answered %d", status)
	}
	if strings.Contains(served, "polyfill.io") {
		t.Errorf(
			"a request to a host that no longer answers reaches the browser once per run: %q",
			served,
		)
	}
	if !strings.Contains(served, `src="`+stubScriptPath+`"`) {
		t.Errorf(
			"the reference was removed rather than pointed at a local no-op: %q",
			served,
		)
	}

	stubStatus, stub := get(t, planned[0].Origin()+stubScriptPath)
	if stubStatus != http.StatusOK || stub == "" {
		t.Errorf("stub script: got %d %q", stubStatus, stub)
	}
}

// An example that asks for a path above its own directory has to resolve the
// same way it does upstream, which is why the whole tree is served rather than
// the one directory.
func TestStaticServer_ServesPathsAboveTheImplementationsOwnDirectory(
	t *testing.T,
) {
	root := wholeCorpus(t)
	shared := filepath.Join(root, "node_modules", "todomvc-common")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "base.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	planned, err := planImplementations(
		root,
		[]string{"react"},
		freePortRange(t, 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	server, err := startStaticServer(root, planned[0])
	if err != nil {
		t.Fatal(err)
	}
	defer server.stop()

	status, body := get(
		t,
		planned[0].Origin()+"/node_modules/todomvc-common/base.css",
	)
	if status != http.StatusOK || body != "body{}" {
		t.Errorf("shared asset: got %d %q", status, body)
	}
}

func TestStaticServerWaitReady_ReportsADocumentThatDoesNotAnswer(t *testing.T) {
	root := wholeCorpus(t)
	document := filepath.Join(root, filepath.FromSlash(documentFor("dojo")))
	if err := os.Chmod(document, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(document, 0o644) })
	planned, err := planImplementations(
		root,
		[]string{"dojo"},
		freePortRange(t, 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	server, err := startStaticServer(root, planned[0])
	if err != nil {
		t.Fatal(err)
	}
	defer server.stop()

	started := time.Now()
	err = server.waitReady(context.Background())
	if err == nil {
		t.Fatal(
			"a document that does not answer would be swept as an error page and come back clean",
		)
	}
	if elapsed := time.Since(started); elapsed > readinessTimeout {
		t.Errorf(
			"waited %s for a status that was never going to change",
			elapsed,
		)
	}
}

func TestStaticServerStop_ReleasesThePort(t *testing.T) {
	root := wholeCorpus(t)
	planned, err := planImplementations(
		root,
		[]string{"vue"},
		freePortRange(t, 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	server, err := startStaticServer(root, planned[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := server.waitReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	server.stop()

	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(planned[0].URL())
		if err != nil {
			return
		}
		response.Body.Close()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf(
		"port %d is still served after stop(), so the next sweep cannot bind it",
		planned[0].Port,
	)
}
