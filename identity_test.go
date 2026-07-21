package component

import (
	"context"
	"testing"
)

func TestLifecycleFromEmpty(t *testing.T) {
	if got := lifecycleFrom(context.Background()); got != nil {
		t.Errorf("lifecycleFrom of a bare context = %v, want nil", got)
	}
}

// TestWithLifecycleReplaces guards the fork behaviour: re-seeding must leave the
// most specific lifecycle on the context, so a sub-lifecycle reports itself
// rather than its parent.
func TestWithLifecycleReplaces(t *testing.T) {
	parent := &L{name: "parent"}
	child := &L{name: "parent/child"}

	ctx := withLifecycle(context.Background(), parent)
	ctx = withLifecycle(ctx, child)

	if got := lifecycleFrom(ctx); got != child {
		t.Errorf("lifecycleFrom after re-seeding = %v, want the child", got)
	}
}

// TestLifecycleSeedsItselfInContext verifies the seed end to end: a procedure
// finds its own lifecycle on its context.
func TestLifecycleSeedsItselfInContext(t *testing.T) {
	var seeded, running *L
	RunProc(func(l *L) {
		running = l
		seeded = lifecycleFrom(l.Context())
	}, WithName("seed-test"))

	if seeded != running {
		t.Errorf("lifecycleFrom(l.Context()) = %v, want the running lifecycle %v", seeded, running)
	}
}
