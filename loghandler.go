package component

import (
	"context"
	"log/slog"
)

// componentLogKey is the attribute key under which a lifecycle's identity is
// stamped onto log records. It is the key [L.LogValue] documents, and the one a
// caller attaches the identity under by hand.
const componentLogKey = "component"

// logHandler wraps another [slog.Handler], stamping the identity of the
// lifecycle carried by a record's context onto that record.
type logHandler struct {
	next slog.Handler
	// grouped reports whether WithGroup opened a group. Attributes logged after
	// that render under qualified keys, so a root-level identity never collides
	// with them and Handle still stamps it.
	grouped bool
	// seenIdentity reports whether an identity attribute was baked in through
	// WithAttrs at the root, so Handle must not stamp it again.
	seenIdentity bool
}

// NewLogHandler wraps next with a handler that stamps onto every record the
// identity of the lifecycle carried by that record's context (see
// [L.LogValue]), under the "component" key. The lifecycle is read when each
// record is handled, not when the handler is built, so it can wrap any handler,
// in any package, at any time:
//
//	slog.SetDefault(slog.New(component.NewLogHandler(
//		slog.NewTextHandler(os.Stderr, nil))))
//	slog.InfoContext(ctx, "ready") // carries the component identity
//
// Only records logged through slog's Context methods carry a lifecycle;
// [slog.Logger.Info] and friends pass [context.Background], which carries none.
// Where such a call site holds the lifecycle, attach it explicitly with
// slog.Any("component", l).
//
// The stamped identity renders before the record's own attributes, mirroring
// [slog.Logger.With], so an attribute set at the call site prevails wherever
// later values win. An identity the record already carries, whether from a
// nested NewLogHandler, an explicit slog.Any("component", l) at the call site,
// or one baked in with [slog.Logger.With], is left as it is and not restated.
func NewLogHandler(next slog.Handler) slog.Handler {
	return logHandler{next: next}
}

func (h logHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h logHandler) Handle(ctx context.Context, record slog.Record) error {
	l := lifecycleFrom(ctx)
	if l == nil || h.seenIdentity || carriesIdentity(record) {
		return h.next.Handle(ctx, record)
	}
	// Stamp the identity first so an attribute set at the call site renders
	// later and prevails, as it would against an identity baked in with With.
	stamped := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	stamped.AddAttrs(slog.Any(componentLogKey, l))
	record.Attrs(func(a slog.Attr) bool {
		stamped.AddAttrs(a)
		return true
	})
	return h.next.Handle(ctx, stamped)
}

// carriesIdentity reports whether record already carries an identity attribute
// at its root, so the handler yields to it rather than stamping a second one.
func carriesIdentity(record slog.Record) bool {
	found := false
	record.Attrs(func(a slog.Attr) bool {
		found = a.Key == componentLogKey
		return !found
	})
	return found
}

// WithAttrs and WithGroup re-wrap the derived handler so that refined loggers
// keep stamping the lifecycle identity.

func (h logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := logHandler{next: h.next.WithAttrs(attrs), grouped: h.grouped, seenIdentity: h.seenIdentity}
	if !h.grouped {
		for _, a := range attrs {
			if a.Key == componentLogKey {
				next.seenIdentity = true
			}
		}
	}
	return next
}

func (h logHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return logHandler{next: h.next.WithGroup(name), grouped: true, seenIdentity: h.seenIdentity}
}
