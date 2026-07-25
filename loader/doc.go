// Package loader runs the components described by a footprint as one program.
//
// A [Footprint] is a static description of the components to start and the
// linkages between them. Each [Claim] in it names a [component.Descriptor], the
// options to bootstrap that component with, and the [component.Linker] that
// connects it to its peers. [Load] spawns one child lifecycle per claim and
// blocks until every component has bootstrapped or one of them fails.
//
// [Entrypoint] is the program's main entry point: it supplies a root context,
// routes the lifecycle's log records to the process-wide slog default, and
// installs a SIGINT/SIGTERM handler before running the procedure, so an
// operator's Ctrl-C or an orchestrator's shutdown signal becomes the request
// that stops the lifecycle. Call [ParseFlags] first to register and parse the
// command-line flags that the component descriptors declare.
//
// As the entry point, the package owns process-wide logging where a library
// must not: its init calls [slog.SetDefault] with a handler that wraps a stdout
// text handler in [component.WrapLogHandler], so every loaded component's
// records carry its identity and honor the -loglevel flag. A program that needs
// a different sink or format calls [slog.SetDefault] itself afterward.
//
// The package ships [component.Linker] implementations that wire components
// together: [MuxLinker] routes links through gocloud's pub-sub URL multiplexer,
// and [SharedMemLinker] connects components within a single process over shared
// memory.
package loader
