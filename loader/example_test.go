package loader_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"gocloud.dev/pubsub"

	"github.com/danielorbach/go-component"
	"github.com/danielorbach/go-component/loader"
)

// Example_loader demonstrates how to use the loader package to load a footprint
// into a component lifecycle.
//
// The footprint is a static description of the components to be loaded, and the
// bindings (i.e. linkages) between them.
func Example_loader() {
	// call loader.ParseFlags at the beginning of main() to parse the standard
	// command-line flags for the loader package. it respects flags defined by the
	// individual component descriptors.
	loader.ParseFlags(SourceComponent, SinkComponent)

	// call component.EntrypointProc to pass control of the main goroutine to the
	// component package. this function will not return until its procedure and all
	// its subcomponents have exited.
	loader.EntrypointProc(func(l *component.L) {
		// use the in-memory linker for this example
		var bindings loader.SharedMemLinker

		// call loader.Load with the current lifecycle to spawn a new subcomponent for
		// each allocation claim in the footprint. the loader package will call the
		// bootstrap function for each component, passing it the child lifecycle for the
		// subcomponent.
		loader.Load(loader.Footprint{
			Name:     "Example of a trivial source-sink footprint",
			Metadata: "This footprint demonstrates how to use the loader package",
			// each allocation claim describes an instance of a component to be loaded
			Allocations: []*loader.Claim{
				{
					Component: SourceComponent,
					// options are component-specific
					Options: SourceOptions{Message: []byte("Hello Stewart")},
					// bindings may be claim-specific, or shared between claims
					// (as in this example) to link components together
					Binding: &bindings,
				},
				{
					Component: SinkComponent,
					// some components have no options
					Options: nil,
					Binding: &bindings,
				},
			},
		})
	})
}

// Linkage is both the name of the aspect used by the source component, and
// the name of the interest used by the sink component.
const Linkage = "target-linkage"

// The SourceComponent is a source component that sends messages to the
// Linkage aspect.
var SourceComponent = &component.Descriptor{
	// the name of the component is used to identify it in the footprint, and in
	// command-line flags - it must be unique, a valid Go identifier.
	Name: "source",
	// the documentation of the component is used to generate the help text for the
	// component's command-line flags - see the field's comment for more details on
	// the format.
	Doc: `
	# Service source
	
	source: continually publishes messages to the agreed-upon Linkage (aspect)
	
	The source component sends messages every Interval duration on its aspect.
	`,
	// the loader package will call the bootstrap function for each instance of this
	// component. this function is responsible for setting up any subcomponents
	// (i.e., goroutines) and resources (i.e., files, sockets, etc.) that the
	// component needs to operate. it returns when the component is running and ready
	// - without waiting for it to stop.
	Bootstrap: func(l *component.L, linker component.Linker, options any) error {
		pub, err := linker.LinkAspect(l.Context(), Linkage)
		if err != nil {
			return fmt.Errorf("open aspect: %w", err)
		}
		l.CleanupBackground(pub.Shutdown)

		// bootstrapping a component is a blocking operation; hence, any long-running
		// operations must be performed in a subcomponent (i.e., a managed goroutine).
		l.Go("pub", func(l *component.L) {
			// options must be type-asserted to the type declared by this descriptor.
			// it is safe to assume that the type is correct, otherwise a panic is
			// appropriate.
			options := options.(SourceOptions)

			// standard Go lifecycle patterns (i.e., defer statements) are encouraged
			// over lifecycle functions (i.e., l.CleanupBackground). the latter is
			// more subject to change and is provided to support a new style of
			// concurrent resource patterns.
			t := time.NewTicker(Interval)
			defer t.Stop()

			// a long-running component usually has a select statement inside an infinite
			// loop, as it spends most of its time waiting for event notifications until it
			// is signaled to stop - either by the Stopping channel or the Context. lifecycle
			// also provides GraceContext (see SourceComponent below) as a convenience for more
			// complex use cases.
			for {
				select {
				case <-t.C:
					err := pub.Send(l.Context(), &pubsub.Message{Body: options.Message})
					if err != nil {
						l.Errorf("send: %w", err)
					}
				case <-l.Stopping():
					// the earliest signal to stop a long-running component is the
					// Stopping() channel. we must respect this signal and return
					// as soon as possible.
					return
				case <-l.Context().Done():
					// the latest signal to stop a long-running component is the
					// Context().Done() channel - which is usually passed down to
					// blocking functions (e.g., pub.Send) to signal them to abort.
					return
				}
			}
		})

		return nil
	},
	// some loaders use the options-type to during construction of footprints from
	// text to unmarshal the appropriate options for each component. the loader
	// package does not use this field.
	OptionsType: reflect.TypeOf(SourceOptions{}),
	// a component declares the aspects it will publish. at this time, this field is
	// not used by the loader package and provides for expressiveness.
	Aspects: []string{Linkage},
	// this component is not interested in other components, it is a pure source.
	Interests: nil,
}

// it is common for components to share static configuration that is set
// separately from the component's options. this is usually done by defining a
// package-level variable and registering it as a command-line flag in the init
// function, as shown below.
var (
	Interval time.Duration
)

func init() {
	// the component's command-line flags are another way to configure the component.
	// as opposed to its options, these flags are not part of the footprint and are
	// constant for all instances of the component.
	// the loader package will automatically add the flags to its CommandLine flags
	// and parse them during ParseFlags.
	// it is recommended to define the flags in an init function to avoid the need
	// to create a new flag.FlagSet.
	SourceComponent.Flags.DurationVar(&Interval, "interval", time.Second, "interval between echo notifications")
}

type SourceOptions struct {
	Message []byte // data to send on the Linkage aspect
}

// The SinkComponent is a sink component that receives messages from the
// Linkage interest.
var SinkComponent = &component.Descriptor{
	Name: "sink",
	Doc: `
	# Service sink
	
	sink: continually subscribes to messages from the agreed-upon Linkage (interest)
	
	The sink component receives simple messages from the source component and echoes
	them to the lifecycle logger.
	`,
	Bootstrap: func(l *component.L, linker component.Linker, options any) error {
		// establishing a link to another component is part of the initialization
		// of the component. hence, it is done in the bootstrap function and its
		// failure will cause the component to fail to start.
		sub, err := linker.LinkInterest(l.Context(), Linkage)
		if err != nil {
			return fmt.Errorf("open interest: %w", err)
		}
		// the component must clean up any resources it has allocated during
		// bootstrap. this is done by registering a cleanup function with the
		// lifecycle. the cleanup function will be called after all the spawned
		// subcomponents have been completed.
		l.CleanupBackground(sub.Shutdown)

		l.Go("sub", func(l *component.L) {
			// lifecycle provides a convenient way to iterate forever, until
			// the component is stopping. this is a common pattern for
			// components that are interested in other components.
			//
			// the alternative is to use a naked for-loop, and check return
			// abruptly when a Receive(l.Context()) call returns an error.
			for l.Continue() {
				// note the use of GraceContext() to ensure that the component
				// gets a change to shut down in a timely manner.
				msg, err := sub.Receive(l.GraceContext())
				if err != nil {
					// context.Cause will return ErrStopped if this context was
					// canceled due to the component stopping.
					if !errors.Is(context.Cause(l.GraceContext()), component.ErrStopped) {
						l.Errorf("receive: %w", err)
					}
					continue
				}
				// ack messages received from interests as soon as possible,
				// otherwise they may be received again.
				msg.Ack()
				// the lifecycle logger is always a uniform way to log messages from components
				l.Log("ECHO", string(msg.Body))
			}
		})

		return nil
	},
	// a component may have no options, in which case this field is nil.
	OptionsType: nil,
	// this component does interest in other components, it is a pure sink.
	Aspects: nil,
	// a component declares the interests it will subscribe to. at this time, this
	// field is not used by the loader package and provides for expressiveness.
	Interests: []string{Linkage},
}
