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
func InjectLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerContextKey{}, logger)
}

// Logger returns the [*slog.Logger] associated with the provided context. If
// none, it returns [slog.Default].
func Logger(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerContextKey{}).(*slog.Logger); ok {
		return logger
	}

	return slog.Default()
}
