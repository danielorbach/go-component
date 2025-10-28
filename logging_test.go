package component

import (
	"context"
	"log/slog"
	"testing"
)

// TestLogger tests the Logger and InjectLogger functions. It verifies that
// Logger retrieves [slog.Default] from the context when no custom logger is
// injected, and that it returns the custom logger when InjectLogger is used to
// inject a custom logger into the context.
func TestLogger(t *testing.T) {
	ctx := context.Background()
	l := Logger(ctx)

	if l != slog.Default() {
		t.Errorf("Logger from context isn't slog default logger")
	}

	l = l.With(
		slog.String("k", "v"),
	)
	ctx = InjectLogger(ctx, l)

	loggerFromCtx := Logger(ctx)
	if loggerFromCtx != l {
		t.Errorf("Logger from context isn't custom logger")
	}
}
