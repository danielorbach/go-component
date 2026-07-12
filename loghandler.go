package component

import (
	"context"
	"log/slog"
	"slices"
)

// logHandler wraps another [slog.Handler], stamping the logging attributes
// carried by a record's context onto that record.
type logHandler struct {
	next slog.Handler
	// seen holds root-level attributes added through WithAttrs; the wrapped
	// handler renders them on every record, so Handle must not stamp the
	// identity again when it is among them.
	seen []slog.Attr
	// grouped reports whether WithGroup opened a group; attributes added
	// after that render under qualified keys, never matching the root-level
	// identity, so seen stops growing.
	grouped bool
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
// later values win. An identity the record already carries, whether from a
// nested NewLogHandler, [LogAttr] at the call site, or [slog.Logger.With], is
// not stamped again.
func NewLogHandler(next slog.Handler) slog.Handler {
	return logHandler{next: next}
}

func (h logHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h logHandler) Handle(ctx context.Context, record slog.Record) error {
	attr := LogAttr(ctx)
	if attr.Equal(slog.Attr{}) || h.carries(record, attr) {
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

// carries reports whether the record already carries attr, among its own
// attributes or baked in through WithAttrs.
func (h logHandler) carries(record slog.Record, attr slog.Attr) bool {
	if slices.ContainsFunc(h.seen, attr.Equal) {
		return true
	}
	carried := false
	record.Attrs(func(a slog.Attr) bool {
		carried = a.Equal(attr)
		return !carried
	})
	return carried
}

// WithAttrs and WithGroup re-wrap the derived handler so that refined loggers
// keep stamping context attributes.

func (h logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := logHandler{next: h.next.WithAttrs(attrs), seen: h.seen, grouped: h.grouped}
	if !h.grouped {
		// Concat copies, so siblings derived from the same receiver never
		// share seen's backing array.
		next.seen = slices.Concat(h.seen, attrs)
	}
	return next
}

func (h logHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return logHandler{next: h.next.WithGroup(name), seen: h.seen, grouped: true}
}
