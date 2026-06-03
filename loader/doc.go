// Package loader runs the components described by a footprint as one program.
//
// A [Footprint] is a static description of the components to start and the
// linkages between them. Each [Claim] in it names a [component.Descriptor], the
// options to bootstrap that component with, and the [component.Linker] that
// connects it to its peers. [Load] spawns one child lifecycle per claim and
// blocks until every component has bootstrapped or one of them fails.
//
// [Entrypoint] is the program's main entry point: it supplies a root context and
// a logger and installs a SIGINT/SIGTERM handler before running the procedure,
// so an operator's Ctrl-C or an orchestrator's shutdown signal becomes the
// request that stops the lifecycle. Call [ParseFlags] first to register and parse
// the command-line flags that the component descriptors declare.
//
// The package ships [component.Linker] implementations that wire components
// together: [MuxLinker] routes links through gocloud's pub-sub URL multiplexer,
// and [SharedMemLinker] connects components within a single process over shared
// memory.
package loader
