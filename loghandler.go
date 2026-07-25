package component

import (
	"context"
	"log/slog"
	"slices"
)

// lifecycleKey is the context key under which a lifecycle carries itself, so a
// WrapLogHandler-wrapped handler can stamp the lifecycle's identity onto records
// logged with that context. The lifecycle is never handed back out through the
// context: only the framework reads this key.
type lifecycleKey struct{}

// withLifecycle returns a copy of ctx carrying l. A forked sub-lifecycle stores
// itself afresh, so its identity replaces the one inherited from its parent.
func withLifecycle(ctx context.Context, l *L) context.Context {
	return context.WithValue(ctx, lifecycleKey{}, l)
}

// lifecycleFrom returns the lifecycle carried by ctx, or nil when ctx carries
// none.
func lifecycleFrom(ctx context.Context) *L {
	l, _ := ctx.Value(lifecycleKey{}).(*L)
	return l
}

// logHandler wraps another slog.Handler, stamping the identity of the lifecycle
// carried by a record's context onto that record.
type logHandler struct {
	next slog.Handler
	// withIdentity reports whether an identity was baked into this handler by
	// WithAttrs, as slog.Logger.With does. Such an attribute never reaches
	// Handle on the record, because it is held by the handler rather than
	// carried by the record, so Handle cannot find it by inspecting the record
	// and would stamp a second identity beside it. The two checks cover
	// different places an identity can already live: this field covers the
	// handler, carriesIdentity covers the record.
	//
	// Neither check can see the other's place, so an identity supplied both
	// ways at once is left alone and renders twice. Reconciling them
	// would mean reaching into next to retract what WithAttrs already baked
	// there, which a handler cannot do. Collapsing that case later would only
	// remove a repetition, so it stays open rather than being solved here.
	withIdentity bool
}

// WrapLogHandler wraps next with a handler that stamps onto every record the
// identity of the lifecycle carried by that record's context (see [L.LogValue]),
// under [LogKey]. The lifecycle is read when each record is handled, not when
// the handler is built, so it can wrap any handler, in any package, at any time:
//
//	slog.SetDefault(slog.New(component.WrapLogHandler(slog.NewTextHandler(os.Stderr, nil))))
//	// ... later, from inside a component:
//	slog.InfoContext(ctx, "ready") // carries the component identity
//
// Only records logged through slog's Context methods carry a lifecycle;
// [slog.Logger.Info] and friends pass [context.Background], which carries none.
// Where such a call site holds the lifecycle, attach it explicitly with
// slog.Any(LogKey, l).
//
// The stamped identity renders before the record's own attributes, mirroring
// [slog.Logger.With], so an attribute set at the call site prevails wherever
// later values win. An identity that is already present is left as it is and
// not restated, whether it comes from a nested WrapLogHandler, an explicit
// slog.Any(LogKey, l) at the call site, or one baked into a logger with
// [slog.Logger.With].
//
// Supply the identity one way, not two: a logger built with slog.Any(LogKey, l)
// that also passes it at the call site names the component twice.
func WrapLogHandler(next slog.Handler) slog.Handler {
	if next == nil {
		panic("component: WrapLogHandler: nil next handler")
	}
	return logHandler{next: next}
}

func (h logHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h logHandler) Handle(ctx context.Context, record slog.Record) error {
	l := lifecycleFrom(ctx)
	if l == nil || h.withIdentity || carriesIdentity(record) {
		return h.next.Handle(ctx, record)
	}
	// Stamp the identity first so an attribute set at the call site renders
	// later and prevails, as it would against an identity baked in with With.
	stamped := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	stamped.AddAttrs(slog.Any(LogKey, l))
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
		found = a.Key == LogKey
		return !found
	})
	return found
}

// WithAttrs and WithGroup re-wrap the derived handler so that refined loggers
// keep stamping the lifecycle identity.

func (h logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// An identity baked in here renders wherever this handler's records render,
	// which is exactly where Handle's stamp would land, so treat it as seen even
	// under an open group.
	seen := h.withIdentity || slices.ContainsFunc(attrs, func(a slog.Attr) bool {
		return a.Key == LogKey
	})
	return logHandler{next: h.next.WithAttrs(attrs), withIdentity: seen}
}

func (h logHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return logHandler{next: h.next.WithGroup(name), withIdentity: h.withIdentity}
}
