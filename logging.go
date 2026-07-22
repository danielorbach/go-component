package component

import (
	"context"
	"log/slog"
)

// loggerContextKey is a unique key used to inject a logger into a context. This
// key is defined as an unexported type to prevent assignment from outside the
// package. The associated value stored in the context will be of type
// [*slog.Logger].
type loggerContextKey struct{}

// InjectLogger returns a new context based on the provided parent context, with
// the provided [*slog.Logger] associated with it.
//
// Deprecated: do not carry a logger in a context. Pass the [*slog.Logger]
// explicitly to the code that needs it, or install one process-wide with
// [slog.SetDefault]. A logger hidden in a context is an invisible dependency,
// which is why slog declined the pattern (golang/go#58243). Removal is reserved
// for a future major version.
func InjectLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerContextKey{}, logger)
}

// Logger returns the [*slog.Logger] associated with the provided context. If
// none, it returns [slog.Default].
//
// Deprecated: obtain a logger from [slog.Default] or one passed explicitly, and
// log through slog's Context-suffixed methods with the lifecycle context so a
// [WrapLogHandler]-wrapped handler attributes the record to its component (see
// [L.LogValue]). Retrieving a logger smuggled through a context hides the
// dependency, which is why slog declined the pattern (golang/go#58243). Removal
// is reserved for a future major version.
func Logger(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerContextKey{}).(*slog.Logger); ok {
		return logger
	}

	return slog.Default()
}
