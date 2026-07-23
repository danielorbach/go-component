package component_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/danielorbach/go-component"
)

func noop(*component.L) {}

// A lifecycle with no handler stays silent rather than leaking its records to
// slog.Default, so embedding a component in a larger program adds no noise.
func TestUnconfiguredLifecycleStaysSilent(t *testing.T) {
	var buf bytes.Buffer
	defer slog.SetDefault(slog.Default())
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))

	component.RunProc(noop, component.WithName("silent"))

	if buf.Len() != 0 {
		t.Errorf("a lifecycle with no handler wrote to slog.Default:\n%s", strings.TrimSpace(buf.String()))
	}
}
