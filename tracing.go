package component

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// SpanComponentNameKey is the OpenTelemetry attribute key that identifies the
// component responsible for a span or span event.
const SpanComponentNameKey attribute.Key = "component.name"

// detachedSpanKey is the context key under which a lifecycle privately carries
// the valid SpanContext that was active when the lifecycle began. New component
// roots link to that span rather than inherit it as their parent.
type detachedSpanKey struct{}

// detachSpan removes a valid current span from lifecycle parentage while
// retaining its SpanContext for links from new component roots. If ctx has no
// valid current span, it is already clean and is returned unchanged; an already
// detached context therefore keeps carrying its retained SpanContext.
func detachSpan(ctx context.Context) context.Context {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ctx
	}
	ctx = context.WithValue(ctx, detachedSpanKey{}, sc)
	return trace.ContextWithSpanContext(ctx, trace.SpanContext{})
}

// detachedSpanFrom returns the SpanContext retained for links from new
// component roots, or an invalid zero SpanContext when no active span was
// detached.
func detachedSpanFrom(ctx context.Context) trace.SpanContext {
	sc, ok := ctx.Value(detachedSpanKey{}).(trace.SpanContext)
	if !ok {
		return trace.SpanContext{}
	}
	return sc
}

// NewSpanProcessor returns an OpenTelemetry span processor that attributes
// spans started from [L.Context] to their component.
//
// Register it with the application's TracerProvider:
//
//	provider := sdktrace.NewTracerProvider(
//		sdktrace.WithSpanProcessor(component.NewSpanProcessor()),
//		sdktrace.WithBatcher(exporter),
//	)
//
// The processor supplies [SpanComponentNameKey] when the span's start
// attributes do not already contain it, so a value set at the call site takes
// precedence. If [WithContext] received a context with an active span, that
// span is detached from the lifecycle and linked to each parentless operation
// span; ordinary child spans retain their parent and do not repeat the link.
//
// OnStart runs after the SDK has made its sampling decision. The attribute and
// link are therefore available to exporters and downstream Collectors, but not
// to an in-process head sampler.
func NewSpanProcessor() sdktrace.SpanProcessor {
	return spanProcessor{}
}

type spanProcessor struct{}

func (spanProcessor) OnStart(parent context.Context, span sdktrace.ReadWriteSpan) {
	l := lifecycleFrom(parent)
	if l == nil {
		return
	}
	if !hasAttribute(span.Attributes(), SpanComponentNameKey) {
		span.SetAttributes(SpanComponentNameKey.String(l.Name()))
	}

	if span.Parent().IsValid() {
		// This is an ordinary descendant of a bounded operation. Repeating the
		// detached upstream link would misrepresent it as another component root.
		return
	}
	detached := detachedSpanFrom(parent)
	if !detached.IsValid() || hasLink(span.Links(), detached) {
		return
	}
	span.AddLink(trace.Link{SpanContext: detached})
}

func (spanProcessor) OnEnd(sdktrace.ReadOnlySpan) {}

func (spanProcessor) Shutdown(context.Context) error {
	return nil
}

func (spanProcessor) ForceFlush(context.Context) error {
	return nil
}

func hasAttribute(attrs []attribute.KeyValue, key attribute.Key) bool {
	for _, attr := range attrs {
		if attr.Key == key {
			return true
		}
	}
	return false
}

func hasLink(links []sdktrace.Link, target trace.SpanContext) bool {
	for _, link := range links {
		if link.SpanContext.TraceID() == target.TraceID() &&
			link.SpanContext.SpanID() == target.SpanID() {
			return true
		}
	}
	return false
}
