package component

import (
	"context"
	"log/slog"
)

// logHandler wraps another [slog.Handler], stamping the logging attributes
// carried by a record's context onto that record.
type logHandler struct {
	next slog.Handler
}

// NewLogHandler wraps next with a handler that stamps onto every record the
// identity carried by that record's context: the group of attributes the
// framework seeds onto each lifecycle (see [LogAttr]). The identity is read
// when each record is handled, not when the handler is built, so it can wrap
// any handler, in any package, at any time:
//
//	slog.SetDefault(slog.New(component.NewLogHandler(
//		slog.NewTextHandler(os.Stderr, nil))))
//	slog.InfoContext(ctx, "ready") // carries the component identity
//
// Only records logged through slog's Context methods carry the identity;
// [slog.Logger.Info] and friends pass [context.Background], which carries
// none. Stamp those call sites explicitly with [LogAttr] instead.
//
// The stamped identity renders before the record's own attributes, mirroring
// [slog.Logger.With], so an attribute set at the call site prevails wherever
// later values win.
func NewLogHandler(next slog.Handler) slog.Handler {
	return logHandler{next: next}
}

func (h logHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h logHandler) Handle(ctx context.Context, record slog.Record) error {
	attr := LogAttr(ctx)
	if attr.Equal(slog.Attr{}) {
		return h.next.Handle(ctx, record)
	}
	// Rebuild the record with the identity first, mirroring attributes baked
	// into a logger with With.
	stamped := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	stamped.AddAttrs(attr)
	record.Attrs(func(a slog.Attr) bool {
		stamped.AddAttrs(a)
		return true
	})
	return h.next.Handle(ctx, stamped)
}

// WithAttrs and WithGroup re-wrap the derived handler so that refined loggers
// keep stamping context attributes.

func (h logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return logHandler{next: h.next.WithAttrs(attrs)}
}

func (h logHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return logHandler{next: h.next.WithGroup(name)}
}
