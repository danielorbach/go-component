package component_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"log/slog"
	"strings"
	"testing"

	"github.com/danielorbach/go-component"
)

// TestWithLogHandlerIdentifiesComponent runs a lifecycle against a plain JSON
// handler and verifies every record the lifecycle emits carries the component
// identity by value: no context-aware wrapping is involved.
func TestWithLogHandlerIdentifiesComponent(t *testing.T) {
	var buf bytes.Buffer
	component.RunProc(func(l *component.L) {
		l.Log("hello")
	}, component.WithName("logging-test"), component.WithLogHandler(slog.NewJSONHandler(&buf, nil)))

	var sawHello bool
	for line := range strings.Lines(strings.TrimSpace(buf.String())) {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decoding record %q: %v", line, err)
		}
		group, _ := record["component"].(map[string]any)
		if group["name"] != "logging-test" {
			t.Errorf("record %q misses component.name=logging-test", strings.TrimSpace(line))
		}
		if record["msg"] == "hello" {
			sawHello = true
		}
	}
	if !sawHello {
		t.Error("the procedure's hello record never reached the configured handler")
	}
}

// TestWithLoggerWarns pins the deprecation escape hatch: the option ignores
// its logger, and the warning surfaces on the default logger so the drop is
// not silent.
func TestWithLoggerWarns(t *testing.T) {
	var buf bytes.Buffer
	defer slog.SetDefault(slog.Default())
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))

	component.RunProc(func(*component.L) {}, component.WithLogger(log.New(io.Discard, "", 0)))

	if line := buf.String(); !strings.Contains(line, "WithLogger") {
		t.Errorf("applying WithLogger logged %q, want a deprecation warning", line)
	}
}

// TestWithDefaultLogHandlerJoinsDefault verifies the shortcut routes the
// lifecycle's records to whatever handler slog.Default carries when the option
// is applied.
func TestWithDefaultLogHandlerJoinsDefault(t *testing.T) {
	var buf bytes.Buffer
	defer slog.SetDefault(slog.Default())
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))

	component.RunProc(func(l *component.L) {
		l.Log("hello")
	}, component.WithName("default-test"), component.WithDefaultLogHandler())

	if line := buf.String(); !strings.Contains(line, `"name":"default-test"`) {
		t.Errorf("WithDefaultLogHandler did not route records to slog.Default(); got %q", line)
	}
}
