package component

import (
	"log/slog"
	"testing"
)

// TestLLogValue pins the identity a lifecycle exposes as an [slog.LogValuer]:
// a group whose sole attribute today is the lifecycle's name.
func TestLLogValue(t *testing.T) {
	l := &L{name: "echo"}

	want := slog.GroupValue(slog.String("name", "echo"))
	if got := l.LogValue(); !got.Equal(want) {
		t.Errorf("L.LogValue() = %v, want %v", got, want)
	}
}
