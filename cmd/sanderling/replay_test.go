package main

import (
	"bytes"
	"io"
	"net"
	"strings"
	"testing"
)

func TestParseReplayArgs_Defaults(t *testing.T) {
	options, err := parseReplayArgs(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.port != 0 || options.noOpen || options.dev || options.directory != "" {
		t.Errorf("unexpected defaults: %+v", options)
	}
}

func TestParseReplayArgs_AllFlags(t *testing.T) {
	options, err := parseReplayArgs([]string{"--port", "9090", "--no-open", "--dev", "/tmp/runs"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.port != 9090 || !options.noOpen || !options.dev || options.directory != "/tmp/runs" {
		t.Errorf("unexpected options: %+v", options)
	}
}

func TestParseReplayArgs_RejectsTooManyPositional(t *testing.T) {
	_, err := parseReplayArgs([]string{"a", "b"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "at most one") {
		t.Fatalf("expected too-many-args error, got %v", err)
	}
}

func TestBuildBrowseURL(t *testing.T) {
	address := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}
	cases := []struct {
		name       string
		deepLinkID string
		want       string
	}{
		{"root", "", "http://127.0.0.1:8080/"},
		{"deep link", "20240101-120000", "http://127.0.0.1:8080/runs/20240101-120000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := buildBrowseURL(address, c.deepLinkID); got != c.want {
				t.Errorf("buildBrowseURL(%q) = %q, want %q", c.deepLinkID, got, c.want)
			}
		})
	}
}

func TestRun_HelpListsReplayCommand(t *testing.T) {
	var stdout bytes.Buffer
	if err := run([]string{"sanderling"}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "replay") {
		t.Errorf("usage missing replay command: %q", stdout.String())
	}
}
