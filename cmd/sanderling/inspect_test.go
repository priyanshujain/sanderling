package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestParseInspectArgs_Defaults(t *testing.T) {
	options, err := parseInspectArgs(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.port != 0 || options.noOpen || options.dev || options.directory != "" {
		t.Errorf("unexpected defaults: %+v", options)
	}
}

func TestParseInspectArgs_AllFlags(t *testing.T) {
	options, err := parseInspectArgs([]string{"--port", "9090", "--no-open", "--dev", "/tmp/runs"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.port != 9090 || !options.noOpen || !options.dev || options.directory != "/tmp/runs" {
		t.Errorf("unexpected options: %+v", options)
	}
}

func TestParseInspectArgs_RejectsTooManyPositional(t *testing.T) {
	_, err := parseInspectArgs([]string{"a", "b"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "at most one") {
		t.Fatalf("expected too-many-args error, got %v", err)
	}
}

func TestRun_HelpListsInspectCommand(t *testing.T) {
	var stdout bytes.Buffer
	if err := run([]string{"sanderling"}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "inspect") {
		t.Errorf("usage missing inspect command: %q", stdout.String())
	}
}
