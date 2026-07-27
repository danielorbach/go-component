package component_test

import (
	"context"
	"testing"

	"github.com/danielorbach/go-component"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestLContextStartsIndependentTraces(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown tracer provider: %v", err)
		}
	})
	tracer := provider.Tracer("component.test")

	type contextKey struct{}
	parentCtx, parent := tracer.Start(
		context.WithValue(context.Background(), contextKey{}, "retained"),
		"caller",
	)

	component.RunProc(func(l *component.L) {
		if got := l.Context().Value(contextKey{}); got != "retained" {
			t.Errorf("context value = %v, want retained", got)
		}
		if sc := trace.SpanContextFromContext(l.Context()); sc.IsValid() {
			t.Errorf("lifecycle context carries span %v, want none", sc)
		}

		operationCtx, operation := tracer.Start(l.Context(), "operation")
		defer operation.End()

		_, child := tracer.Start(operationCtx, "child")
		child.End()
	}, component.WithContext(parentCtx), component.WithName("worker"))
	parent.End()

	spans := make(map[string]sdktrace.ReadOnlySpan)
	for _, span := range recorder.Ended() {
		spans[span.Name()] = span
	}

	operation, ok := spans["operation"]
	if !ok {
		t.Fatal("operation span not recorded")
	}
	if operation.Parent().IsValid() {
		t.Errorf("operation parent = %v, want root", operation.Parent())
	}

	child, ok := spans["child"]
	if !ok {
		t.Fatal("child span not recorded")
	}
	if child.Parent().TraceID() != operation.SpanContext().TraceID() ||
		child.Parent().SpanID() != operation.SpanContext().SpanID() {
		t.Errorf("child parent = %v, want %v", child.Parent(), operation.SpanContext())
	}
}
