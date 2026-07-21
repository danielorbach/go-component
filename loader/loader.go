package loader

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"os"
	"runtime"
	"runtime/pprof"
	gotrace "runtime/trace"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gocloud.dev/pubsub"

	"github.com/danielorbach/go-component"
	"github.com/danielorbach/go-component/healthprobe"
)

var tracer = otel.Tracer("github.com/danielorbach/go-component/loader")

var (
	// Debug is a set of single-letter flags:
	//
	//	f	show [f]acts as they are created
	// 	p	disable [p]arallel execution of analyzers
	//	s	do additional [s]anity checks on fact types and serialization
	//	t	show [t]iming info (NB: use 'p' flag to avoid GC/scheduler noise)
	//	v	show [v]erbose logging
	//
	Debug = ""

	// Log files for optional performance tracing.
	CPUProfile, MemProfile, Trace string

	// LogLevel is the minimal level at which log records will be emitted. Any log
	// records below this level will be omitted.
	LogLevel slog.Level

	// Descriptors are the enabled components of the service
	Descriptors []*component.Descriptor
)

func init() {
	// Any log records with a level lower than the specified LogLevel will be
	// omitted. The component.WrapLogHandler wrapping stamps the component
	// identity onto every record logged with a lifecycle context, so logs from
	// all loaded components arrive attributed without per-component wiring.
	slog.SetDefault(slog.New(component.WrapLogHandler(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: &LogLevel}))))
}

func init() {
	CommandLine.StringVar(&CPUProfile, "cpuprofile", "", "Set to write CPU profile to this file")
	CommandLine.StringVar(&MemProfile, "memprofile", "", "Set to write memory profile to this file")
	CommandLine.StringVar(&Trace, "trace", "", "Set to write trace to this file")
	CommandLine.TextVar(&LogLevel, "loglevel", slog.LevelInfo, "Ignore all log records below this log level")
	CommandLine.StringVar(&HealthAddress, "health", "", "Set to enable health server on the specified local address")
}

// loaded ensures that the Load function is executed only once. It acts as a
// guard to prevent multiple consecutive loads, which for now is forbidden. Read
// Load doc for more information.
var loaded atomic.Bool

// Load initializes the components and shared resources for the given footprint.
// This function guarantees that the initialization process is carried out only
// once, preventing re-initialization and potential conflicts. It configures the
// environment, applies the provided options, and starts the lifecycle of the
// components. Currently, only a single footprint is supported due to this
// decision.
//
// In the future, the API of this package will be expanded to support multiple
// footprints. This change is probably backwards incompatible because it affects
// some decisions that make sense when loading only a single footprint. For
// example, the loader lifecycle stages that are exposed via Health.
func Load(footprint Footprint, opts ...component.Option) {
	// TODO(@danielorbach): We think that Load can become a method of Footprint, when
	//  we are refactor this code please consider that.

	// If Load has already been executed, prevent subsequent runs by panicking. This
	// ensures only a single successful call to load the footprint.
	if loaded.Swap(true) {
		panic("cannot load a footprint more than once")
	}

	// We register started/completed hooks to notify the global health probe.
	opts = append(opts,
		component.OnStarted(Health.ProcStarted),
		component.OnComplete(Health.ProcCompleted),
	)

	EntrypointProc(func(l *component.L) {
		loadSharedResources(l)
		loadFootprintComponents(l, footprint)
	}, opts...)
}

func loadSharedResources(l *component.L) {
	_, span := tracer.Start(l.Context(), "Load.SharedResources")
	defer span.End()

	if CPUProfile != "" {
		f, err := os.Create(CPUProfile)
		if err != nil {
			log.Fatalf("Failed to create CPU profile: %v", err)
		}
		l.Cleanup(func() {
			if err := f.Close(); err != nil {
				log.Printf("Failed to close CPU profile: %v", err)
			}
		})

		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatalf("Failed to start CPU profile: %v", err)
		}
		l.Cleanup(pprof.StopCPUProfile)
	}

	if Trace != "" {
		f, err := os.Create(Trace)
		if err != nil {
			log.Fatalf("Failed to create trace: %v", err)
		}
		l.Cleanup(func() {
			if err := f.Close(); err != nil {
				log.Printf("Failed to close trace: %v", err)
			}
		})

		if err := gotrace.Start(f); err != nil {
			log.Fatalf("Failed to start trace: %v", err)
		}
		l.Cleanup(func() {
			gotrace.Stop()
			log.Printf("To view the trace, run:\n$ go tool trace view %s", Trace)
		})
	}

	if MemProfile != "" {
		f, err := os.Create(MemProfile)
		if err != nil {
			log.Fatalf("Failed to create memory profile: %v", err)
		}
		l.Cleanup(func() {
			if err := f.Close(); err != nil {
				log.Printf("Failed to close memory profile: %v", err)
			}
		})

		l.Cleanup(func() {
			runtime.GC() // get up-to-date statistics
			if err := pprof.WriteHeapProfile(f); err != nil {
				log.Fatalf("Writing memory profile: %v", err)
			}
		})
	}

	if HealthAddress != "" {
		lis, err := net.Listen("tcp", HealthAddress)
		if err != nil {
			log.Fatalf("Failed to listen on health endpoint: %v", err)
		}
		l.Cleanup(func() {
			if err := lis.Close(); err != nil {
				if !errors.Is(err, net.ErrClosed) {
					log.Printf("Failed to close health listener: %v", err)
				}
			}
		})
		go pprof.Do(l.Context(), pprof.Labels("health-server", "grpc"), func(ctx context.Context) {
			healthprobe.StartGRPC(l, lis, &Health)
		})
	}
}

func loadFootprintComponents(l *component.L, footprint Footprint) {
	_, span := tracer.Start(l.Context(), "Load.FootprintComponents")
	defer span.End()

	// TODO: skip loading based on Location
	var g forkGroup
	// Load the relevant components from the footprint.
	for i := range footprint.Allocations {
		claim := footprint.Allocations[i]
		// Does footprint contain only enabled components?
		if !IsEnabled(claim.Component) {
			slog.InfoContext(l.Context(), "skipping disabled component", "name", claim.Component.Name)
			span.AddEvent("loader.skip", trace.WithAttributes(
				attribute.String("component.name", claim.Component.Name),
				attribute.Stringer("footprint.identifier", footprint.Identifier),
				attribute.Int("footprint.revision", footprint.Revision),
			))
			continue
		}

		span.AddEvent("loader.load", trace.WithAttributes(
			attribute.String("component.name", claim.Component.Name),
			attribute.Stringer("footprint.identifier", footprint.Identifier),
			attribute.Int("footprint.revision", footprint.Revision),
		))
		// TODO(@danielorbach): when supporting concurrent footprints, perhaps Fork based on fp.Identifier (to improve logging).
		g.Fork(l, claim)
	}

	slog.Info("Waiting for all components to load...")
	// We wait until all loaded components are ready (or terminated before that), or
	// the process is signalled to shut down gracefully.
	readyClaims := g.Wait(l.GraceContext())
	if readyClaims == len(footprint.Allocations) {
		Health.LoadingCompleted()
		slog.Info("All components are ready")
	} else {
		unreadyClaims := len(footprint.Allocations) - readyClaims
		slog.Info("Some components bootstrap was not ready on time", "ready", readyClaims, "unready", unreadyClaims)
	}
}

// TODO(@danielorbach): We wish to enable a Claim to signal it's done (perhaps
//  include Claim.Done()), which can be used instead of the forkGroup.

// A forkGroup is used by Load to keep track of allocated components.
//
// The group can Wait until all claims it had previously Forked are ready or
// terminate prematurely.
type forkGroup struct {
	mu          sync.Mutex
	allocated   []*Claim
	completions []<-chan struct{}
}

func (f *forkGroup) Fork(l *component.L, claim *Claim) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Store the signals of the forked claim before actually forking it.
	f.allocated = append(f.allocated, claim)
	done := make(chan struct{}, 1)
	f.completions = append(f.completions, done)
	l.Fork(claim.Component.Name, claim, component.WithForkCompletion(done))
}

// Wait for the ready or premature termination signals of the allocated claims,
// within the duration of the given context. All claims are allocated by prior
// calls to Fork during [Load].
func (f *forkGroup) Wait(ctx context.Context) (ready int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.allocated) != len(f.completions) {
		panic(fmt.Sprintf("mismatch allocated claims (%d) to completion channels (%d)", len(f.allocated), len(f.completions)))
	}

	var n atomic.Int32
	var wg sync.WaitGroup
	for i, claim := range f.allocated {
		wg.Add(1)
		go func(claim *Claim, completion <-chan struct{}) {
			defer wg.Done()
			select {
			case <-claim.Ready():
				n.Add(1)
				slog.Debug("Loaded component is ready", "name", claim.Component.Name)
			case <-completion:
				slog.Error("Loaded component terminated before ready", "name", claim.Component.Name)
			case <-ctx.Done():
				slog.Warn("Loaded component was not ready in time", "name", claim.Component.Name)
			}
		}(claim, f.completions[i])
	}
	wg.Wait()
	return int(n.Load())
}

func IsEnabled(d *component.Descriptor) bool {
	return slices.Contains(Descriptors, d)
}

type Footprint struct {
	Name       string // human-readable name (does not need to be unique)
	Metadata   string // human-readable description/summary/notes/comments
	Identifier uuid.UUID
	Revision   int // linear (scalar) sequence number ordering different versions in time
	Locations  []string
	Solution   string
	// An Allocation is a concrete resource claim that defines how a given Instance
	// should be bootstrapped (allocated) from components (i.e. descriptors)
	// available in given Locations, and their interconnections (i.e. Linkages -
	// Aspect and Interest).
	Allocations []*Claim
}

type InstanceId struct {
	Footprint uuid.UUID // footprint-identifier
	Component string    // component-descriptor
	Name      string    // human-assigned
}

// A Claim specifies the resources required to bootstrap and run an instance of a
// component. A single claim can be executed at most once.
type Claim struct {
	//InstanceId InstanceId
	Component *component.Descriptor
	Options   any // component-specific configuration options
	Binding   component.Linker

	// The channel is closed by Exec, to signal that the Claim bootstrap process has
	// completed successfully, and the Component is ready to serve its purpose.
	ready     chan struct{} // DO NOT access this channel directly, even internally; see readyChan doc.
	initReady sync.Once     // Used to initialise a meaningful zero-value; see readyChan doc.
	// A claim must never execute more than once; this flag is set to true once Exec
	// has started.
	executed atomic.Bool
}

// Exec panics when called more than once for the same claim. It may panic while
// the first call to Exec has yet to return.
func (c *Claim) Exec(l *component.L) {
	if reused := c.executed.Swap(true); reused {
		panic("loaded claim can be executed only once")
	}

	span := trace.SpanFromContext(l.Context())
	span.SetAttributes(
		attribute.String("component.name", c.Component.Name),
		attribute.String("component.binding", fmt.Sprintf("%+v", c.Binding)),
		attribute.String("component.options", fmt.Sprintf("%+v", c.Options)),
	)

	linker := protectLinker(c.Binding, c.Component)
	//if c.Component.Bootstrap == nil {
	//	c.Component.Run(l, linker, c.Options, superClaim{}.ready)
	//}
	err := c.Component.Bootstrap(l, linker, c.Options)
	if err != nil {
		l.Fatalf("bootstrap: %w", err)
	}
	// Mark the claim as ready after successfully bootstrapping the component.
	// Placing this here ensures that the claim is only marked ready once we confirm
	// that the component initialization is complete without errors.
	c.setReady()
}

func (c *Claim) setReady() {
	close(c.readyChan())
}

// Users of this package create Claim values by setting the exported fields.
// Users may call Ready to wait until the claim has completed its bootstrap and
// all procedures have been run successfully. As such, users cannot set the
// unexported ready channel.
//
// Only ever access the ready channel through the value this function returns. It
// ensures the channel is initialised, otherwise we'd not be able to signal on
// the nil channel.
func (c *Claim) readyChan() chan struct{} {
	c.initReady.Do(func() {
		c.ready = make(chan struct{})
	})
	return c.ready
}

// Ready returns a channel that's closed when the Component loaded by this Claim
// is running and ready to serve its purpose.
//
// The Component is ready when either [component.Descriptor.Bootstrap] returned
// successfully.
func (c *Claim) Ready() <-chan struct{} {
	return c.readyChan()
}

// panicUnexpectedLinkage wraps a component.Linker to detect and alert usage of
// aspects and interests that are not defined in the component's descriptor.
//
// lookup is O(1) because the aspects/interests are stored in a map, otherwise a
// binary search would have been necessary. Go guarantees that the memory cost of
// a map is double the size of the data stored in it, so it's not a big deal.
type panicUnexpectedLinkage struct {
	base      component.Linker
	aspects   map[string]struct{}
	interests map[string]struct{}
}

func protectLinker(base component.Linker, d *component.Descriptor) panicUnexpectedLinkage {
	// although we could've
	aspects := make(map[string]struct{}, len(d.Aspects))
	for _, a := range d.Aspects {
		aspects[a] = struct{}{}
	}
	interests := make(map[string]struct{}, len(d.Interests))
	for _, i := range d.Interests {
		interests[i] = struct{}{}
	}

	return panicUnexpectedLinkage{
		base:      base,
		aspects:   aspects,
		interests: interests,
	}
}

func (l panicUnexpectedLinkage) LinkAspect(ctx context.Context, aspect string) (*pubsub.Topic, error) {
	if _, ok := l.aspects[aspect]; !ok {
		panic("component/loader: an aspect must be defined in the component's descriptor")
	}
	return l.base.LinkAspect(ctx, aspect)
}

func (l panicUnexpectedLinkage) LinkInterest(ctx context.Context, interest string) (*pubsub.Subscription, error) {
	if _, ok := l.interests[interest]; !ok {
		panic("component/loader: an interest must be defined in the component's descriptor")
	}
	return l.base.LinkInterest(ctx, interest)
}

type Allocated struct {
	Request   Claim
	Footprint Footprint
	Error     error // nil if successfully allocated
}

type Reclaimed struct {
	Request   Claim
	Footprint Footprint
	Error     error // non-nil if reclaimed due to an internal error
}
