// Package component provides types and functions for building enterprise
// message-driven component systems. Components are deployable units that
// observe and react to events. They are described by a [Descriptor] (identity,
// flags, bootstrap logic, pub/sub topology) and run inside an [L] (lifecycle)
// that manages concurrent execution, logging, cleanup, and graceful shutdown.
//
// # Tracing contract
//
// The framework distinguishes two scopes of OpenTelemetry observability:
//
//   - The lifecycle scope. Each lifecycle owns one span — accessible via
//     [L.Span] — that opens when the lifecycle starts and closes when it
//     completes. Lifecycle spans nest: a child lifecycle's span is a child of
//     its parent's. Use the lifecycle span to record attributes, events, and
//     errors that pertain to the lifecycle event itself (started, completed,
//     bootstrap status). [L.Error] and [L.Fatal] do this automatically.
//
//   - The handler scope. Spans started from [L.Context] are roots of
//     independent traces, intentionally detached from the lifecycle span.
//     This is the framework's mitigation against unbounded traces: a
//     subscription consumer that handles thousands of messages over hours of
//     uptime would, if its handler spans inherited the lifecycle span as
//     parent, accumulate spans into a single trace whose size grows with
//     uptime and saturates downstream backends. By severing this parent
//     relationship at l.Context(), each handler invocation produces its own
//     bounded trace.
//
// Per-handler instrumentation should link back to the lifecycle for
// navigation in the trace backend:
//
//	ctx, span := tracer.Start(l.Context(), "consume",
//		trace.WithSpanKind(trace.SpanKindConsumer),
//		trace.WithLinks(trace.Link{SpanContext: l.Span().SpanContext()}),
//	)
//	defer span.End()
//
// Authors integrating a new transport (e.g., a non-Kafka subscription
// consumer) should follow the same shape: l.Context() for the handler's
// parent context, a link back to l.Span() for navigability. Anything
// nested under l.Context() will be part of the same per-handler trace, and
// is bounded by the work the handler does.
//
// One-shot setup work that genuinely belongs under the lifecycle span (for
// example, the loader's per-component bootstrap tracing) can opt into
// nesting by attaching the lifecycle span explicitly:
//
//	ctx := trace.ContextWithSpan(l.Context(), l.Span())
//	_, span := tracer.Start(ctx, "Bootstrap.Step")
//	defer span.End()
//
// This is appropriate only when the span count contributed by such code is
// bounded by the lifecycle's own bootstrap, not by external traffic.
package component
