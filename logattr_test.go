package component

import (
	"context"
	"log/slog"
	"testing"
)

func TestLogAttrEmpty(t *testing.T) {
	if got := LogAttr(context.Background()); !got.Equal(slog.Attr{}) {
		t.Errorf("LogAttr of a bare context = %v, want the zero Attr", got)
	}
}

// TestWithComponentLogAttrReplaces guards the fork behaviour: re-seeding must
// leave exactly one component attribute, carrying the most specific name, so a
// sub-lifecycle reports itself rather than its parent.
func TestWithComponentLogAttrReplaces(t *testing.T) {
	ctx := withComponentLogAttr(context.Background(), "parent")
	ctx = withComponentLogAttr(ctx, "parent/child")

	want := slog.Group("", slog.String(logAttrComponent, "parent/child"))
	if got := LogAttr(ctx); !got.Equal(want) {
		t.Errorf("LogAttr after re-seeding = %v, want %v", got, want)
	}
}

// TestLifecycleSeedsComponentAttr verifies the seed end to end: a procedure
// finds its component's identity on its own context.
func TestLifecycleSeedsComponentAttr(t *testing.T) {
	var attr slog.Attr
	RunProc(func(l *L) {
		attr = LogAttr(l.Context())
	}, WithName("seed-test"))

	want := slog.Group("", slog.String(logAttrComponent, "seed-test"))
	if !attr.Equal(want) {
		t.Errorf("LogAttr(l.Context()) = %v, want %v", attr, want)
	}
}
