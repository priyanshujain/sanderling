package testrun

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestProgressHandler_LineFormat(t *testing.T) {
	tests := []struct {
		name  string
		level slog.Level
		msg   string
		attrs []slog.Attr
		want  string
	}{
		{"info has no level prefix", slog.LevelInfo, "running", nil, "running\n"},
		{"warn prefixes lowercased level", slog.LevelWarn, "slow", nil, "warn: slow\n"},
		{"error prefixes lowercased level", slog.LevelError, "boom", nil, "error: boom\n"},
		{
			"string attr is quoted",
			slog.LevelInfo, "step", []slog.Attr{slog.String("name", "tap home")},
			"step name=\"tap home\"\n",
		},
		{
			"non-string attr renders bare",
			slog.LevelInfo, "step", []slog.Attr{slog.Int("seed", 42)},
			"step seed=42\n",
		},
		{
			"attrs render in order",
			slog.LevelInfo, "step",
			[]slog.Attr{slog.Int("n", 1), slog.String("k", "v")},
			"step n=1 k=\"v\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			h := &progressHandler{writer: &buf, level: slog.LevelInfo}
			record := slog.NewRecord(time.Time{}, tt.level, tt.msg, 0)
			record.AddAttrs(tt.attrs...)
			if err := h.Handle(context.Background(), record); err != nil {
				t.Fatal(err)
			}
			if got := buf.String(); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
