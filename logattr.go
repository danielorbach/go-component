package component

import (
	"context"
	"log/slog"
)

// logAttrKey is the context key under which the framework carries the group
// of logging attributes that identify a component's work. The framework is
// the only writer: callers attach their own attributes by deriving a logger
// with [slog.Logger.With], not through the context.
type logAttrKey struct{}

// LogAttr returns the logging attributes the framework carries on ctx, as a
// single group attribute with an empty key, or the zero Attr when ctx carries
// none. Handlers inline an empty-keyed group, rendering its attributes at the
// record's top level, so the identity attaches wherever one attribute fits:
// baked into a derived logger with [slog.Logger.With], or raw at the call
// site:
//
//	logger.LogAttrs(ctx, slog.LevelInfo, "started", component.LogAttr(ctx))
func LogAttr(ctx context.Context) slog.Attr {
	group, ok := ctx.Value(logAttrKey{}).(slog.Value)
	if !ok {
		return slog.Attr{}
	}
	return slog.Attr{Value: group}
}

// logAttrComponent is the key under which a lifecycle stamps its own name, so
// that all logs from a component's goroutines identify the component.
const logAttrComponent = "component"

// withComponentLogAttr returns a copy of ctx whose logging attributes carry
// name under [logAttrComponent]. Each seed stores a fresh group, so a forked
// sub-lifecycle's identity replaces the one inherited from its parent.
func withComponentLogAttr(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, logAttrKey{}, slog.GroupValue(slog.String(logAttrComponent, name)))
}
