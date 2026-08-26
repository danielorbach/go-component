// Package component provides building blocks for message-driven services whose
// startup, shutdown, and cleanup must happen in a controlled order.
//
// A component runs as a lifecycle, represented by [L]: a procedure with its own
// context that can spawn child lifecycles, register cleanup work, and react to a
// request to stop. [RunProc] starts one and blocks until it, its children, and
// their cleanup have all finished.
//
// # Testing with synctest
//
// The lifecycle's concurrency is exercised under [testing/synctest], which gives
// a test deterministic control over the goroutines a lifecycle starts. The L
// tests in lifecycle_test.go are the worked examples to follow when testing
// lifecycle behaviour.
//
// # Configuring a lifecycle
//
// [RunProc] and [Run] accept [Option] values that configure the lifecycle before
// it starts. The common ones are [WithName] to identify it in logs and profiles,
// [WithContext] to derive its cancellation, deadlines, and values,
// [WithLogHandler] to direct its log records, and [WithStopper] to hand it a
// channel whose closing asks the procedure to stop. That stop request is what
// [L.Continue] and [L.Stopping] report to the running code, so a procedure can
// wind down on its own terms.
//
// # Running a program
//
// A standalone program rarely calls [RunProc] itself. The
// [github.com/danielorbach/go-component/loader] subpackage provides Entrypoint,
// which installs a context, log routing, and an interrupt handler before running
// the procedure, so that pressing Ctrl-C or receiving a SIGTERM (as Kubernetes
// sends on shutdown) becomes the stop request the lifecycle observes. Reach for
// [RunProc] or [Run] when embedding a lifecycle inside a larger program or a
// test.
//
// # Logging
//
// A lifecycle logs its own records (a component starting, completing, or
// failing) through [log/slog]. [WithLogHandler] directs those records at a
// handler; without one they are discarded, so a lifecycle embedded in a larger
// program stays silent until that program asks for its output. A program that
// boots through the loader has [WithDefaultLogHandler] applied for it and need
// not pass a handler.
//
// Embedding a lifecycle adds no logging of its own: it writes only to the
// handler you give it and never sets or reads the process-wide [slog.Default].
// Sending records to that global is the surrounding program's decision, not the
// lifecycle's.
//
// A procedure logs for itself through slog rather than through the lifecycle.
// Passing the lifecycle's context to slog's Context-suffixed methods is what
// ties a record to the component that wrote it:
//
//	slog.InfoContext(l.Context(), "handled message", "topic", topic)
//
// Each lifecycle carries itself in its context, and [WrapLogHandler] wraps a
// handler so that every record whose context carries a lifecycle is stamped
// with that lifecycle's identity, under [LogKey]. Wrap the handler the
// application installs at startup:
//
//	slog.SetDefault(slog.New(component.WrapLogHandler(slog.NewTextHandler(os.Stderr, nil))))
//
// Only the Context-suffixed methods receive a context; [slog.Info] and its
// siblings pass [context.Background], which carries no lifecycle. Where a call
// site holds the lifecycle but cannot route through a wrapped handler, attach
// it by hand under [LogKey]: a lifecycle is an [slog.LogValuer] (see
// [L.LogValue]), so slog.Any(LogKey, l) stamps the same identity.
//
// Attach your own attributes the slog way: derive a logger with
// [slog.Logger.With] and pass it to the code that should inherit them. The
// framework offers no facility for stashing attributes in a context to be
// logged later; slog itself declined such an API, holding that logging state
// belongs on a logger rather than travelling implicitly through a context.
//
// Such a logger can carry the component identity too, not only your own
// attributes: bake in slog.Any(LogKey, l) and a [WrapLogHandler]-wrapped
// handler recognizes it rather than stamping a second one. That is one way to
// attach the identity; the call site is another. Use one, not both, or the
// component is named twice, as the examples on [WrapLogHandler] show.
//
// The lifecycle's string-formatting methods — [L.Log], [L.Logf], [L.Error] and
// [L.Errorf] — predate slog and are deprecated; each names the slog call to
// write in its place. [L.Fatal] and [L.Fatalf] are deprecated as well: new code
// handles logging and tracing at the call site, calls [L.Terminate] only when
// managed children need cancellation, and returns explicitly.
//
// # Log levels
//
// slog's levels, from [slog.LevelDebug] to [slog.LevelError], are available to
// a procedure directly. The minimum level is a property of the handler the
// application installs rather than of the lifecycle, and is set through
// [slog.HandlerOptions]. A program that boots through the loader gets that
// lever as its -loglevel command-line flag.
//
// # Completing and terminating procedures
//
// Returning completes the current procedure. The lifecycle then waits for its
// managed children and runs cleanup; it does not cancel their contexts merely
// because the procedure returned. A leaf procedure with no dependent managed
// work can therefore handle an error and simply return:
//
//	if err != nil {
//		slog.ErrorContext(ctx, "operation failed", "err", err)
//		span.RecordError(err)
//		span.SetStatus(codes.Error, err.Error())
//		return
//	}
//
// Call [L.Terminate] before returning when children or other managed work must
// observe cancellation in order to exit:
//
//	if err != nil {
//		slog.ErrorContext(ctx, "supervisor failed", "err", err)
//		span.RecordError(err)
//		span.SetStatus(codes.Error, err.Error())
//		l.Terminate()
//		return
//	}
//
// Terminate only cancels the lifecycle contexts; it does not abort the caller's
// goroutine. [ProcE] applies the supervising form automatically when its
// function returns an error, using that error as the cancellation cause, and
// then returns normally.
//
// # Tracing
//
// A lifecycle is a control-flow boundary, not a trace operation. Component does
// not hold a span open while a procedure runs: a long-lived component can
// process many unrelated operations, and making all of them children of one
// lifecycle span would create one unbounded trace.
//
// [L.Context] therefore carries no active span inherited through [WithContext].
// It still derives cancellation, deadlines, values, and baggage from that
// context. Start a bounded span where an operation begins and pass the returned
// context through that operation:
//
//	ctx, span := tracer.Start(l.Context(), "handle")
//	defer span.End()
//	slog.InfoContext(ctx, "handled message")
//
// A span started directly from [L.Context] is the root of an independent trace.
// Child spans started from the returned ctx retain the ordinary OpenTelemetry
// parent-child relationship.
//
// Register [NewSpanProcessor] with the application's OpenTelemetry
// TracerProvider to set [TraceKey] on spans started from a lifecycle context
// when the call site has not supplied it. When [WithContext] receives a context
// with an active span, the lifecycle detaches it rather than making it a parent;
// the processor links it to parentless operation spans so the relationship
// remains navigable without merging their traces. Child spans have an explicit
// parent and do not repeat the link.
//
// The processor runs after the OpenTelemetry SDK sampler. The component name
// and link it supplies are visible to exporters and downstream Collectors, but
// not to in-process head sampling. Applications that delegate
// component-sensitive sampling to a Collector must still configure the SDK to
// export the spans the Collector should consider.
//
// Process identity is separate from component identity. Attributes such as
// service.instance.id belong to the immutable OpenTelemetry Resource configured
// by the application, shared by its traces, metrics, and logs; this package
// neither generates nor mutates them.
//
// # Correlating logs with traces
//
// A component that handles messages for hours emits records throughout, and an
// operator reading one of them often needs the trace for the work that wrote
// it. A handler sees the context of each call site, so it can read the active
// span from it: install an OpenTelemetry slog bridge as the handler, and every
// record carries the identifiers of the span it was written under. As with the
// component identity, this reaches only the records written through slog's
// Context-suffixed methods.
package component
