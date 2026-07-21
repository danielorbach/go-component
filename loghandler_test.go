package component

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// logRecord logs one record through a NewLogHandler-wrapped JSON handler and
// returns it decoded into a map. Where JSON carries duplicate keys, decoding
// keeps the last value, matching how attribute-reading backends resolve them.
func logRecord(t *testing.T, log func(logger *slog.Logger)) map[string]any {
	t.Helper()

	var buf bytes.Buffer
	logger := slog.New(NewLogHandler(slog.NewJSONHandler(&buf, nil)))
	log(logger)

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("decoding emitted record %q: %v", buf.String(), err)
	}
	return record
}

// componentName extracts the lifecycle name from a decoded record's nested
// component group, reporting whether the group was present.
func componentName(record map[string]any) (string, bool) {
	group, ok := record[LogKey].(map[string]any)
	if !ok {
		return "", false
	}
	name, ok := group["name"].(string)
	return name, ok
}

func TestLogHandlerStampsContextIdentity(t *testing.T) {
	ctx := withLifecycle(context.Background(), &L{name: "echo"})

	record := logRecord(t, func(logger *slog.Logger) {
		logger.InfoContext(ctx, "ready")
	})

	if name, ok := componentName(record); !ok || name != "echo" {
		t.Errorf("record omitted the component identity: got %v, want name=echo", record[LogKey])
	}
	if record["msg"] != "ready" {
		t.Errorf("record msg = %v, want ready", record["msg"])
	}
}

// TestLogHandlerIgnoresBackgroundContext documents the stdlib constraint: the
// non-Context methods pass context.Background, which carries no lifecycle, so
// the handler has nothing to stamp.
func TestLogHandlerIgnoresBackgroundContext(t *testing.T) {
	ctx := withLifecycle(context.Background(), &L{name: "echo"})

	record := logRecord(t, func(logger *slog.Logger) {
		_ = ctx // Info does not take a context; the identity cannot travel.
		logger.Info("ready")
	})

	if _, ok := record[LogKey]; ok {
		t.Errorf("Info without a context carried an identity: %v", record[LogKey])
	}
}

// TestLogHandlerStampsThroughRefinedLogger guards the re-wrapping in WithAttrs:
// a logger refined with With must still stamp the context identity.
func TestLogHandlerStampsThroughRefinedLogger(t *testing.T) {
	ctx := withLifecycle(context.Background(), &L{name: "echo"})

	record := logRecord(t, func(logger *slog.Logger) {
		logger.With(slog.String("phase", "boot")).InfoContext(ctx, "ready")
	})

	if name, ok := componentName(record); !ok || name != "echo" {
		t.Errorf("refined logger dropped the identity: got %v, want name=echo", record[LogKey])
	}
	if record["phase"] != "boot" {
		t.Errorf("refined logger dropped its own attribute: got phase=%v, want boot", record["phase"])
	}
}

// TestLogHandlerCallSiteWins pins the precedence model: the handler yields to an
// identity the call site set for itself, so an explicit attribute under the
// component key prevails over the framework's stamp.
func TestLogHandlerCallSiteWins(t *testing.T) {
	ctx := withLifecycle(context.Background(), &L{name: "echo"})

	record := logRecord(t, func(logger *slog.Logger) {
		logger.InfoContext(ctx, "ready", LogKey, "explicit")
	})

	if record[LogKey] != "explicit" {
		t.Errorf("call-site attribute lost: got %v=%v, want explicit", LogKey, record[LogKey])
	}
}

// logLine logs one record through the logger built over the given handler and
// returns the rendered text line, for asserting how often an attribute appears.
func logLine(t *testing.T, handler func(slog.Handler) slog.Handler, log func(logger *slog.Logger)) string {
	t.Helper()

	var buf bytes.Buffer
	log(slog.New(handler(slog.NewTextHandler(&buf, nil))))
	return strings.TrimSpace(buf.String())
}

// TestLogHandlerStampsOnceWhenNested runs two NewLogHandler layers in one
// chain: the outer stamps the identity onto the record, and the inner finds it
// already there, so the line mentions the component exactly once.
func TestLogHandlerStampsOnceWhenNested(t *testing.T) {
	ctx := withLifecycle(context.Background(), &L{name: "echo"})

	line := logLine(t, func(base slog.Handler) slog.Handler {
		return NewLogHandler(NewLogHandler(base))
	}, func(logger *slog.Logger) {
		logger.InfoContext(ctx, "ready")
	})

	if got := strings.Count(line, "component.name=echo"); got != 1 {
		t.Errorf("nested handlers mentioned the component %d times in %q, want once", got, line)
	}
}

// TestLogHandlerHonoursExplicitIdentity covers callers who attach the lifecycle
// themselves, as a raw attribute at the call site: the handler finds the
// identity key already present and does not restate it.
func TestLogHandlerHonoursExplicitIdentity(t *testing.T) {
	lc := &L{name: "echo"}
	ctx := withLifecycle(context.Background(), lc)

	line := logLine(t, NewLogHandler, func(logger *slog.Logger) {
		logger.LogAttrs(ctx, slog.LevelInfo, "ready", slog.Any(LogKey, lc))
	})

	if got := strings.Count(line, "component.name=echo"); got != 1 {
		t.Errorf("explicitly attached identity rendered %d times in %q, want once", got, line)
	}
}

// TestLogHandlerHonoursIdentityBakedWithWith covers callers who bake the
// lifecycle into a derived logger: the handler observed it through WithAttrs
// and does not restate it from the context.
func TestLogHandlerHonoursIdentityBakedWithWith(t *testing.T) {
	lc := &L{name: "echo"}
	ctx := withLifecycle(context.Background(), lc)

	line := logLine(t, NewLogHandler, func(logger *slog.Logger) {
		logger.With(slog.Any(LogKey, lc)).InfoContext(ctx, "ready")
	})

	if got := strings.Count(line, "component.name=echo"); got != 1 {
		t.Errorf("baked identity rendered %d times in %q, want once", got, line)
	}
}

// TestLogHandlerHonoursIdentityBakedBeforeGroup opens a group after baking the
// identity: the baked attribute still renders at the root of every record, so
// the handler must not stamp it again.
func TestLogHandlerHonoursIdentityBakedBeforeGroup(t *testing.T) {
	lc := &L{name: "echo"}
	ctx := withLifecycle(context.Background(), lc)

	line := logLine(t, NewLogHandler, func(logger *slog.Logger) {
		logger.With(slog.Any(LogKey, lc)).WithGroup("req").InfoContext(ctx, "ready", "id", 7)
	})

	if got := strings.Count(line, "component.name=echo"); got != 1 {
		t.Errorf("identity baked before a group rendered %d times in %q, want once", got, line)
	}
}
