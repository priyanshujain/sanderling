package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// newProgressLogger wires a slog.Logger that prints user-facing progress
// lines to the CLI's stdout stream. Info messages render as
// "msg key=value ..." to match the prose style of other CLI prints;
// warnings and errors get a "warn:" / "error:" prefix so they stand
// out in the same stream.
func newProgressLogger(writer io.Writer) *slog.Logger {
	return slog.New(&progressHandler{writer: writer, level: slog.LevelInfo})
}

type progressHandler struct {
	writer io.Writer
	level  slog.Level
}

func (h *progressHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *progressHandler) Handle(_ context.Context, record slog.Record) error {
	var builder strings.Builder
	if record.Level >= slog.LevelWarn {
		fmt.Fprintf(&builder, "%s: ", strings.ToLower(record.Level.String()))
	}
	builder.WriteString(record.Message)
	record.Attrs(func(attr slog.Attr) bool {
		fmt.Fprintf(&builder, " %s=%s", attr.Key, formatAttrValue(attr.Value))
		return true
	})
	builder.WriteByte('\n')
	_, err := io.WriteString(h.writer, builder.String())
	return err
}

func (h *progressHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *progressHandler) WithGroup(_ string) slog.Handler      { return h }

func formatAttrValue(value slog.Value) string {
	if value.Kind() == slog.KindString {
		return fmt.Sprintf("%q", value.String())
	}
	return value.String()
}
