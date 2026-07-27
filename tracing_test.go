package component_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/danielorbach/go-component"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// A lifecycle remains a trace boundary when an application opts into component
// span enrichment. WithContext retains ordinary context state but detaches the
// active upstream span, while the processor adds identity and correlation links
// without changing operation parentage:
//
//	upstream operation [outside component, no component.name]
//	├── link ──▶ operation [root, component.name=service]
//	│             └── child [parent=operation, component.name=service]
//	├── link ──▶ already-linked [root, component.name=service]
//	└── link ──▶ fork-operation [root, component.name=service/worker]
//
// Root means that the span has no valid OpenTelemetry parent; a SpanContext is
// a value and is never represented by nil. The already-linked span starts with
// its link supplied by the call site, so the processor must not add a duplicate.
func TestLifecycleTracing(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(component.NewSpanProcessor()),
		sdktrace.WithSpanProcessor(recorder),
	)
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown tracer provider: %v", err)
		}
	})
	tracer := provider.Tracer("component.test")

	type contextKey struct{}
	upstreamCtx, upstreamSpan := tracer.Start(context.WithValue(context.Background(), contextKey{}, "retained"), "upstream operation")

	component.RunProc(func(l *component.L) {
		if got := l.Context().Value(contextKey{}); got != "retained" {
			t.Errorf("context value = %v, want retained", got)
		}
		if sc := trace.SpanContextFromContext(l.Context()); sc.IsValid() {
			t.Errorf("lifecycle context carries span %v, want none", sc)
		}

		operationCtx, operation := tracer.Start(l.Context(), "operation")
		_, child := tracer.Start(operationCtx, "child")
		child.End()
		operation.End()

		// Supplying the upstream operation link explicitly must be idempotent
		// with the processor's automatic link.
		_, linked := tracer.Start(l.Context(), "already-linked", trace.WithLinks(trace.Link{SpanContext: upstreamSpan.SpanContext()}))
		linked.End()

		l.Go("worker", func(l *component.L) {
			_, forkOperation := tracer.Start(l.Context(), "fork-operation")
			forkOperation.End()
		})
	}, component.WithContext(upstreamCtx), component.WithName("service"))
	upstreamSpan.End()

	spans := make(map[string]sdktrace.ReadOnlySpan)
	for _, span := range recorder.Ended() {
		spans[span.Name()] = span
	}

	// Starting a lifecycle from an upstream operation must not retroactively
	// attribute that operation to the component.
	upstream := spans["upstream operation"]
	if upstream == nil {
		t.Fatal("upstream operation span not recorded")
	}
	if attrs := upstream.Attributes(); len(attrs) != 0 {
		t.Errorf("upstream operation attributes = %v, want none", attrs)
	}

	// The first bounded operation starts a new trace while retaining both the
	// lifecycle identity and a navigable relationship to the upstream operation.
	operation := spans["operation"]
	assertRoot(t, operation)
	assertComponentName(t, operation, "service")
	assertLink(t, operation, upstream.SpanContext())

	// Once the operation establishes a trace, ordinary descendants retain its
	// parentage and do not repeat the component-entry link.
	child := spans["child"]
	assertComponentName(t, child, "service")
	if got, want := child.Parent(), operation.SpanContext(); !got.Equal(want) {
		t.Errorf("child parent = %v, want %v", got, want)
	}
	if links := child.Links(); len(links) != 0 {
		t.Errorf("child links = %v, want none", links)
	}

	// A link supplied at the call site remains authoritative; the processor may
	// enrich the span but must not duplicate that correlation.
	alreadyLinked := spans["already-linked"]
	assertRoot(t, alreadyLinked)
	assertComponentName(t, alreadyLinked, "service")
	assertLink(t, alreadyLinked, upstream.SpanContext())

	// A managed child lifecycle is another component entry point: it starts a
	// root linked to the same upstream operation and carries its compound name.
	forkOperation := spans["fork-operation"]
	assertRoot(t, forkOperation)
	assertComponentName(t, forkOperation, "service/worker")
	assertLink(t, forkOperation, upstream.SpanContext())
}

// An application registers the processor alongside its exporter. Spans started
// from a lifecycle context receive its component name unless the call site
// supplies one when starting the span.
func ExampleNewSpanProcessor() {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(component.NewSpanProcessor()),
		// Synchronous export keeps this example deterministic. Applications
		// normally use WithBatcher with their telemetry exporter.
		sdktrace.WithSyncer(exporter),
	)
	tracer := provider.Tracer("example")

	component.RunProc(func(l *component.L) {
		_, fallback := tracer.Start(l.Context(), "processor fallback")
		fallback.End()

		callSiteName := component.TraceKey.String("call-site-value")
		_, override := tracer.Start(l.Context(), "call site takes precedence", trace.WithAttributes(callSiteName))
		override.End()
	}, component.WithName("lifecycle-value"))

	spans := exporter.GetSpans()
	if err := provider.Shutdown(context.Background()); err != nil {
		panic(err)
	}
	for _, span := range spans {
		fmt.Printf("%s:", span.Name)
		for _, attr := range span.Attributes {
			fmt.Printf(" %s=%s", attr.Key, attr.Value.String())
		}
		fmt.Println()
	}

	// Output:
	// processor fallback: component.name=lifecycle-value
	// call site takes precedence: component.name=call-site-value
}

// The initial, explicitly linked, and forked component operations must be roots;
// links preserve upstream correlation without joining the upstream trace.
func assertRoot(t *testing.T, span sdktrace.ReadOnlySpan) {
	t.Helper()
	if span == nil {
		t.Fatal("span not recorded")
	}
	if span.Parent().IsValid() {
		t.Errorf("span parent = %v, want root", span.Parent())
	}
}

func assertComponentName(t *testing.T, span sdktrace.ReadOnlySpan, want string) {
	t.Helper()
	if span == nil {
		t.Fatal("span not recorded")
	}
	for _, attr := range span.Attributes() {
		if attr.Key == component.TraceKey {
			if got := attr.Value.AsString(); got != want {
				t.Errorf("component.name = %q, want %q", got, want)
			}
			return
		}
	}
	t.Error("component.name attribute not recorded")
}

// Exactly one link must be present, and it must point to want; this catches a
// processor that duplicates correlation already supplied at the call site.
func assertLink(t *testing.T, span sdktrace.ReadOnlySpan, want trace.SpanContext) {
	t.Helper()
	if span == nil {
		t.Fatal("span not recorded")
	}
	links := span.Links()
	if len(links) != 1 {
		t.Fatalf("span links = %v, want exactly one", links)
	}
	got := links[0].SpanContext
	if !got.Equal(want) {
		t.Errorf("span link = %v, want %v", got, want)
	}
}
