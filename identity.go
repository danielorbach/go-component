package component

import "context"

// lifecycleKey is the context key under which a lifecycle carries itself, so a
// [NewLogHandler]-wrapped handler can stamp the lifecycle's identity onto
// records logged with that context. The lifecycle is never handed back out
// through the context: only the framework reads this key.
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
