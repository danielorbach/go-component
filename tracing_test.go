package component

import (
	"slices"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// installRecorder swaps in an in-memory tracer provider for the duration of t,
// restoring the previous global at cleanup. It returns the recorder so tests
// can inspect started/ended spans.
//
// OpenTelemetry's global package binds delegating tracers to the first
// TracerProvider registered after package init (sync.Once on the delegate),
// so installRecorder must be called once at the top-level test rather than
// once per subtest. See https://pkg.go.dev/go.opentelemetry.io/otel/internal/global.
func installRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return sr
}

// findSpan looks up an ended span by its name. Returns nil if not found.
func findSpan(spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	if i := slices.IndexFunc(spans, func(s sdktrace.ReadOnlySpan) bool { return s.Name() == name }); i >= 0 {
		return spans[i]
	}
	return nil
}

// waitForSpan polls sr.Ended() until a span with the given name appears. The
// lifecycle ends its span asynchronously via a reaper goroutine, so the span
// may not be present when Run() returns.
func waitForSpan(t *testing.T, sr *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	deadline := time.Now().Add(SyncTimeout)
	for {
		if s := findSpan(sr.Ended(), name); s != nil {
			return s
		}
		if time.Now().After(deadline) {
			started := make([]string, 0, len(sr.Started()))
			for _, s := range sr.Started() {
				started = append(started, s.Name())
			}
			ended := make([]string, 0, len(sr.Ended()))
			for _, s := range sr.Ended() {
				ended = append(ended, s.Name())
			}
			t.Fatalf("timed out waiting for span %q (started=%v, ended=%v)", name, started, ended)
			return nil
		}
		time.Sleep(time.Millisecond)
	}
}

func TestTracingContract(t *testing.T) {
	sr := installRecorder(t)
	handlerTracer := otel.Tracer("test/handler")

	t.Run("HandlerSpansAreRoots", func(t *testing.T) {
		RunProc(func(l *L) {
			_, span := handlerTracer.Start(l.Context(), "tc.handler.work")
			span.End()
		}, WithName("tc.solo"))

		handler := waitForSpan(t, sr, "tc.handler.work")
		// A span started from l.Context() must be the root of its own trace.
		// If it inherited the lifecycle span as parent, downstream backends
		// would see traces that grow with component uptime — see Issue #56.
		if handler.Parent().IsValid() {
			t.Errorf("handler span has parent %v, want root", handler.Parent())
		}
	})

	t.Run("ChildLifecycleNestsUnderParent", func(t *testing.T) {
		RunProc(func(l *L) {
			done := make(chan struct{})
			l.Fork("child", Proc(func(*L) { close(done) }))
			<-done
		}, WithName("tc.parent"))

		parent := waitForSpan(t, sr, "tc.parent")
		child := waitForSpan(t, sr, "tc.parent/child")
		// Child lifecycle spans nest under parent lifecycle spans — this is
		// the only nesting the framework guarantees.
		if child.Parent().SpanID() != parent.SpanContext().SpanID() {
			t.Errorf("child lifecycle parent %v, want %v", child.Parent().SpanID(), parent.SpanContext().SpanID())
		}
	})

	t.Run("HandlerSpanInChildIsRoot", func(t *testing.T) {
		// In a forked lifecycle, l.Context() is still detached from any
		// lifecycle span, so per-handler spans within the child also start
		// new traces.
		RunProc(func(l *L) {
			done := make(chan struct{})
			l.Fork("child", Proc(func(l *L) {
				_, span := handlerTracer.Start(l.Context(), "tc.child.handler")
				span.End()
				close(done)
			}))
			<-done
		}, WithName("tc.parent2"))

		handler := waitForSpan(t, sr, "tc.child.handler")
		if handler.Parent().IsValid() {
			t.Errorf("child handler span has parent %v, want root", handler.Parent())
		}
	})

	t.Run("LinkToLifecycleSpan", func(t *testing.T) {
		// Per-handler instrumentation should link back to the lifecycle for
		// navigability in the trace backend. Verify that the link mechanism
		// works end-to-end through L.Span().
		var lifecycleSC trace.SpanContext
		RunProc(func(l *L) {
			lifecycleSC = l.Span().SpanContext()
			_, span := handlerTracer.Start(l.Context(), "tc.handler.linked",
				trace.WithLinks(trace.Link{SpanContext: lifecycleSC}),
			)
			span.End()
		}, WithName("tc.linked"))

		handler := waitForSpan(t, sr, "tc.handler.linked")
		links := handler.Links()
		if len(links) != 1 {
			t.Fatalf("got %d links, want 1", len(links))
		}
		if links[0].SpanContext.SpanID() != lifecycleSC.SpanID() {
			t.Errorf("link SpanID %v, want %v", links[0].SpanContext.SpanID(), lifecycleSC.SpanID())
		}
	})

	t.Run("LifecycleSpanIsValid", func(t *testing.T) {
		// L.Span() must return a usable span that records into the configured
		// tracer provider — Error and Fatal call RecordError on it.
		var sc trace.SpanContext
		RunProc(func(l *L) {
			sc = l.Span().SpanContext()
		}, WithName("tc.valid"))
		if !sc.IsValid() {
			t.Errorf("L.Span() SpanContext is invalid, want a recording span")
		}
	})
}
