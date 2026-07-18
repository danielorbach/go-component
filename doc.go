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
// it starts. The common ones are [WithName] to identify it in logs and traces,
// [WithContext] to root it in a parent context, [WithHandler] to direct its
// log records, and [WithStopper] to hand it a channel whose closing asks the
// procedure to stop. That stop request is what [L.Continue] and [L.Stopping]
// report to the running code, so a procedure can wind down on its own terms.
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
// failing) through [log/slog]. [WithHandler] directs those records at a
// handler; without one they are discarded, so a lifecycle embedded in a larger
// program stays silent until that program asks for its output. A program that
// boots through the loader inherits the process-wide default handler and need
// not pass one.
//
// Component code logs for itself through slog rather than through the
// lifecycle. Passing the lifecycle's context to slog's Context-suffixed methods
// is what ties a record to the component that wrote it:
//
//	slog.InfoContext(l.Context(), "handled message", "topic", topic)
//
// Each lifecycle seeds its identity into its context, and [NewLogHandler] wraps
// a handler so that every record whose context carries that identity is stamped
// with it. Wrap the handler the application installs at startup:
//
//	slog.SetDefault(slog.New(component.NewLogHandler(
//		slog.NewTextHandler(os.Stderr, nil))))
//
// Only the Context-suffixed methods receive a context; [slog.Info] and its
// siblings pass [context.Background], which carries no identity. Where a call
// site cannot route through a wrapped handler, [LogAttr] returns the identity
// as a single attribute to attach by hand.
//
// Attach your own attributes the slog way: derive a logger with
// [slog.Logger.With] and pass it to the code that should inherit them. The
// framework offers no facility for stashing attributes in a context to be
// logged later; slog itself declined such an API, holding that logging state
// belongs on a logger rather than travelling implicitly through a context.
//
// The lifecycle's string-formatting methods — [L.Log], [L.Logf], [L.Error] and
// [L.Errorf] — predate slog and are deprecated; each names the slog call to
// write in its place. [L.Fatal] and [L.Fatalf] remain, being control flow that
// happens to log rather than a way to format a message.
//
// # Log levels
//
// slog's levels, from [slog.LevelDebug] to [slog.LevelError], are available to
// component code directly. The minimum level is a property of the handler the
// application installs rather than of the lifecycle, and is set through
// [slog.HandlerOptions]. A program that boots through the loader gets that
// lever as its -loglevel command-line flag.
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
