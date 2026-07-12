package component

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
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

func TestLogHandlerStampsContextAttrs(t *testing.T) {
	ctx := withComponentLogAttr(context.Background(), "echo")

	record := logRecord(t, func(logger *slog.Logger) {
		logger.InfoContext(ctx, "ready")
	})

	if record["component"] != "echo" {
		t.Errorf("record omitted context attribute: got component=%v, want echo", record["component"])
	}
	if record["msg"] != "ready" {
		t.Errorf("record msg = %v, want ready", record["msg"])
	}
}

// TestLogHandlerIgnoresBackgroundContext documents the stdlib constraint: the
// non-Context methods pass context.Background, which carries no attributes, so
// the handler has nothing to stamp.
func TestLogHandlerIgnoresBackgroundContext(t *testing.T) {
	ctx := withComponentLogAttr(context.Background(), "echo")

	record := logRecord(t, func(logger *slog.Logger) {
		_ = ctx // Info does not take a context; the attribute cannot travel.
		logger.Info("ready")
	})

	if _, ok := record["component"]; ok {
		t.Errorf("Info without a context carried an attribute: %v", record["component"])
	}
}

// TestLogHandlerStampsThroughRefinedLogger guards the re-wrapping in WithAttrs:
// a logger refined with With must still stamp context attributes.
func TestLogHandlerStampsThroughRefinedLogger(t *testing.T) {
	ctx := withComponentLogAttr(context.Background(), "echo")

	record := logRecord(t, func(logger *slog.Logger) {
		logger.With(slog.String("phase", "boot")).InfoContext(ctx, "ready")
	})

	if record["component"] != "echo" {
		t.Errorf("refined logger dropped context attribute: got component=%v, want echo", record["component"])
	}
	if record["phase"] != "boot" {
		t.Errorf("refined logger dropped its own attribute: got phase=%v, want boot", record["phase"])
	}
}

// TestLogHandlerCallSiteWins pins the precedence model: the stamped attributes
// render before the record's own, so an explicit attribute at the call site
// comes later and prevails wherever later values win, as in slog.Logger.With.
func TestLogHandlerCallSiteWins(t *testing.T) {
	ctx := withComponentLogAttr(context.Background(), "echo")

	record := logRecord(t, func(logger *slog.Logger) {
		logger.InfoContext(ctx, "ready", "component", "explicit")
	})

	if record["component"] != "explicit" {
		t.Errorf("call-site attribute lost: got component=%v, want explicit", record["component"])
	}
}
