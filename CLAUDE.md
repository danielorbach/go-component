# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

This is a Go library providing types and functions for building enterprise message-driven component systems. Components are deployable units that observe and react to events, with **Aspects** (what they publish) and **Interests** (what they subscribe to) linked via a pub/sub system.

## Commands

### Testing
```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run a specific test
go test -run TestName ./...

# Run tests in a specific package
go test ./loader/...

# Run tests with race detector
go test -race ./...
```

### Building
```bash
# Build the module
go build ./...

# Build an example
go build ./examples/direct
go build ./examples/embed

# Run an example
go run ./examples/direct
```

### Linting & Formatting
```bash
# Format code
go fmt ./...

# Run go vet
go vet ./...

# Run staticcheck (if installed)
staticcheck ./...
```

### Module Management
```bash
# Tidy dependencies
go mod tidy

# Verify dependencies
go mod verify

# Download dependencies
go mod download
```

## Architecture

### Core Concepts

**Component Lifecycle (`lifecycle.go`, `procedure.go`)**
- **L (Lifecycle)**: Manages concurrent execution, logging, cleanup, and graceful shutdown
  - `L.Context()`: Main context, cancelled on termination
  - `L.GraceContext()`: Cancelled when stopping gracefully (before Context)
  - `L.Stopping()`: Channel closed when graceful stop begins
  - `L.Continue()`: Helper to check if lifecycle should continue
  - `L.Go(name, proc)` / `L.Fork(name, procedure)`: Spawn child lifecycles
  - `L.Cleanup()` / `L.CleanupError()`: Register LIFO cleanup functions
  - `L.Fatal(err)`: Terminate lifecycle with error (only call from main goroutine)
  - `L.Stop(timeout)`: Request graceful shutdown
- **Procedure**: Interface with `Exec(*L)` method for lifecycle body
- **Proc / ProcE**: Function adapters for Procedure interface
- **Run(procedure, opts...)**: Entry point to execute a lifecycle

**Component Descriptor (`descriptor.go`)**
- Describes a component's identity, flags, bootstrap logic, and pub/sub topology
- **Aspects**: Topics the component publishes to
- **Interests**: Topics the component subscribes from
- **Linker**: Interface to resolve aspects/interests to pub/sub targets (Topics/Subscriptions)
- **Bootstrap function**: `func(l *L, target Linker, options any) error`

**Loader System (`loader/`)**
- **Footprint**: Describes deployment topology with Allocations (component instances)
- **Claim**: Resource allocation for a single component instance
  - Links Component descriptor + Options + Linker binding
  - Provides `Ready()` channel signaling successful bootstrap
- **Load(footprint, opts...)**: Main entry point to initialize and run components
- **Linkers** (`loader/linkers.go`):
  - `SharedMemLinker`: In-memory pub/sub for single-process testing
  - `MuxLinker`: Multi-scheme support (kafka://, mem://, etc.)

### Package Structure

- **Root package** (`component`): Core lifecycle, procedure, descriptor types
- **`loader/`**: Component loading, footprints, claims, linkers, flag parsing
  - **`loaderflags/`**: Flag parsing utilities (enabled, required, help)
- **`fileloader/`**: Load configurations from JSON/YAML files
- **`kafkalinker/`**: Kafka-specific linker implementation
- **`kafkaloader/`**: Kafka topic/subscription builders
- **`healthprobe/`**: HTTP/gRPC health check servers
- **`examples/`**: Demo applications showing usage patterns
  - `direct/`: Manual footprint construction
  - `embed/`: Embedded component pattern

### Key Patterns

**Lifecycle Management**
- Lifecycles are hierarchical: child lifecycles inherit parent context and stopping signal
- Use `L.Fork()` to create child lifecycles that run concurrently
- Cleanup functions run in LIFO order, even if lifecycle terminates abruptly
- `L.Fatal()` terminates the current lifecycle via `runtime.Goexit()` - only call from the main lifecycle goroutine

**Graceful Shutdown**
```go
for l.Continue() {
    // work loop
}
// or
select {
case <-l.Stopping():
    // graceful stop
case <-l.Context().Done():
    // forced termination
}
```

**Component Linking**
- Aspects and Interests must be declared in Descriptor
- Linker resolves them to concrete pub/sub Topics/Subscriptions
- Use `SharedMemLinker` for in-process testing, Kafka linkers for production

**Tracing Contract**
- `l.Context()` is detached from the lifecycle span: spans started from it are roots of independent traces. This is what subscription handlers and other per-iteration work should use, so trace size is bounded per invocation rather than by component uptime.
- `l.Span()` is the lifecycle span. It opens at lifecycle start, closes at lifecycle completion, and is intended for lifecycle-scope observability (attributes, events, errors). `L.Error` and `L.Fatal` record onto it automatically.
- Per-handler instrumentation should link back to the lifecycle for backend navigability: `trace.WithLinks(trace.Link{SpanContext: l.Span().SpanContext()})`.
- One-shot work that genuinely belongs under the lifecycle span (e.g., loader bootstrap stages) opts into nesting by attaching the span explicitly: `trace.ContextWithSpan(l.Context(), l.Span())`.
- See package doc and `examples/direct/pong.go` for the canonical handler shape.

**Testing**
- Tests use table-driven patterns with subtests
- `SyncTimeout` constant (1 second) for synchronization events
- Use `L.Stop(timeout)` to cleanly terminate test lifecycles

## Common Workflows

### Adding a New Component
1. Define a `component.Descriptor` with Name, Doc, Aspects, Interests
2. Implement Bootstrap function: `func(l *L, linker Linker, options any) error`
3. Register component in `loader.Descriptors` if using loader package
4. Link aspects via `linker.LinkAspect(ctx, aspect)` → `*pubsub.Topic`
5. Link interests via `linker.LinkInterest(ctx, interest)` → `*pubsub.Subscription`

### Running Tests for a Component
```bash
# Test the component package
go test -v -run TestComponentName ./path/to/package
```

### Debugging with Profiling
```bash
# Run with CPU profile
go run ./examples/direct -cpuprofile=cpu.prof

# Run with memory profile
go run ./examples/direct -memprofile=mem.prof

# Run with trace
go run ./examples/direct -trace=trace.out
```

## Module Information

- **Module**: `github.com/danielorbach/go-component`
- **Go Version**: 1.24.0
- **Key Dependencies**:
  - `gocloud.dev/pubsub`: Portable pub/sub abstraction
  - `github.com/IBM/sarama`: Kafka client
  - `go.opentelemetry.io/otel`: OpenTelemetry tracing
  - `github.com/peterbourgon/ff/v3`: Flag parsing

## Notes

- This library uses OpenTelemetry for distributed tracing (see `tracer` variables)
- Pprof labels are applied to goroutines for better profiling (`component.name`, `component.depth`)
- Health probes track lifecycle states (started/completed) via hooks
- The loader enforces single-load semantics (Load() can only be called once)
