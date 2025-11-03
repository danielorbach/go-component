package component

import (
	"context"
	"errors"
	"io"
	"log"

	"go.opentelemetry.io/otel"
)

// The Procedure is the primary procedure of the component lifecycle.
//
// During Exec authors should use the L parameter to interact with the current
// lifecycle:
//   - L.Go starts a new goroutine and returns immediately; the component does not
//     complete until all goroutines started by L.Go have been completed.
//   - L.Cleanup (and variants) registers a function to be called when the
//     component completes.
//   - L.Log and L.Error report logs and errors.
//   - L.Fatal terminates the component with the given error.
//   - L.Context returns the context associated with the component.
//   - L.Terminate terminates the component, causing L.Context to be canceled.
type Procedure interface {
	Exec(*L)
}

// The Proc type is an adapter to allow the use of ordinary functions as
// component Procedure.
//
// If f is a function with the appropriate signature,
// Proc(f) is a Procedure that calls f.
//
// See notes on Procedure for more details about this function.
type Proc func(*L)

func (f Proc) Exec(l *L) {
	f(l)
}

// The ProcE type is an adapter to allow the use of ordinary functions as
// component Procedure.
//
// If f is a function with the appropriate signature,
// ProcE(f) is a Procedure that calls f and then calls Fatal if it returned a
// non-nil error.
//
// See notes on Procedure for more details about this function.
type ProcE func(*L) error

func (f ProcE) Exec(l *L) {
	if err := f(l); err != nil {
		l.Fatal(err)
	}
}

var tracer = otel.Tracer("github.com/danielorbach/go-component")

// RunProc runs the provided procedure function, passing it a new lifecycle.
//
// The function blocks until the lifecycle has completed; i.e., until the main
// function has returned (or called L.Fatal), all its child lifecycles have
// completed, and all cleanup functions have been called.
func RunProc(main Proc, opts ...Option) {
	Run(main, opts...)
}

// Run runs the provided procedure, passing it a new lifecycle.
//
// The function blocks until the lifecycle has completed; i.e., until the main
// function has returned (or called L.Fatal), all its child lifecycles have
// completed, and all cleanup functions have been called.
func Run(body Procedure, opts ...Option) {
	options := lifecycleOptions{
		ctx:       context.Background(),
		logger:    log.New(io.Discard, "", log.LstdFlags),
		procedure: body,
		done:      make(chan struct{}),
	}
	for _, opt := range opts {
		opt(&options)
	}
	if err := options.validate(); err != nil {
		panic("component: invalid options: " + err.Error())
	}

	// execute the procedure in a separate goroutine (see execute for explanation)
	go execute(options.ctx, options)
	// TODO: imagine a way to communicate errors from the executed procedure and its children, and return them here
	<-options.done
}

// Option is a function that configures a new lifecycle.
type Option func(*lifecycleOptions)

// The information stored within lifecycleOptions is exposed via its exported
// method-set. That is, execute should not access the unexported fields directly,
// rather interact through exported methods.
type lifecycleOptions struct {
	name           string
	span           string // overrides 'name' if set
	ctx            context.Context
	done           chan struct{}
	stopper        <-chan struct{}
	logger         *log.Logger
	procedure      Procedure
	startedHooks   []func(name string)
	completedHooks []func(name string)
}

func (o lifecycleOptions) validate() error {
	var errs error
	if o.ctx == nil {
		errs = errors.Join(errs, errors.New("context is nil"))
	}
	if o.logger == nil {
		errs = errors.Join(errs, errors.New("logger is nil"))
	}
	return errs
}

// Name returns the name associated with the lifecycle options.
func (o lifecycleOptions) Name() string {
	return o.name
}

// SpanName returns the span name if specified; otherwise, it defaults to the
// lifecycle name.
func (o lifecycleOptions) SpanName() string {
	if o.span != "" {
		return o.span
	}
	return o.name
}

// Context retrieves the context associated with the lifecycle, panicking if it's
// nil.
func (o lifecycleOptions) Context() context.Context {
	if o.ctx == nil {
		panic("component: context is nil")
	}
	return o.ctx
}

// Done returns the channel used to signal the completion of lifecycle
// operations.
func (o lifecycleOptions) Done() chan<- struct{} {
	return o.done
}

// Stopper provides a channel through which external stop signals are received.
func (o lifecycleOptions) Stopper() <-chan struct{} {
	return o.stopper
}

// Logger returns the logger configured for the lifecycle, used for logging
// output.
func (o lifecycleOptions) Logger() *log.Logger {
	return o.logger
}

// Procedure retrieves the main procedure associated with the lifecycle to be
// executed.
func (o lifecycleOptions) Procedure() Procedure {
	return o.procedure
}

// Started executes hooks associated with the start of the lifecycle using the
// given name.
func (o lifecycleOptions) Started(name string) {
	for _, hook := range o.startedHooks {
		hook(name)
	}
}

// Completed executes hooks associated with the completion of the lifecycle
// using the given name.
func (o lifecycleOptions) Completed(name string) {
	for _, hook := range o.completedHooks {
		hook(name)
	}
}

// WithName sets the name of the new lifecycle.
// If no name is provided, the name of the program is used (i.e., os.Args[0]).
func WithName(name string) Option {
	return func(o *lifecycleOptions) {
		o.name = name
	}
}

// WithSpan overrides the name of the new lifecycle in the trace.
// If no name is provided, the name of the lifecycle is used.
func WithSpan(name string) Option {
	return func(o *lifecycleOptions) {
		o.span = name
	}
}

// WithContext sets the parent context of the new lifecycle.
// If no context is provided, a background context is used.
func WithContext(ctx context.Context) Option {
	return func(o *lifecycleOptions) {
		o.ctx = ctx
	}
}

// WithCompletion closes the given channel to signal that the new
// lifecycle has completed its execution; that is, its root procedure,
// child lifecycles and its cleanup functions.
func WithCompletion(done chan struct{}) Option {
	return func(o *lifecycleOptions) {
		o.done = done
	}
}

// WithStopper monitors the given channel to signal that the new
// lifecycle should stop by receiving a value on the channel.
// If no stopper is provided, the lifecycle will not trigger
// a stop until manually requested via a call to L.Stop().
func WithStopper(stopper <-chan struct{}) Option {
	return func(o *lifecycleOptions) {
		o.stopper = stopper
	}
}

// WithLogger sets the logger used by the new lifecycle.
// If no logger is provided, a logger that discards all messages is used.
// The lifecycle shares this logger with all its children - started via
// L.Go().
//
// TODO: using log.Llongfile causes the logger to print the wrong file because the call-site is in this package always.
func WithLogger(logger *log.Logger) Option {
	return func(o *lifecycleOptions) {
		o.logger = logger
	}
}

// OnStarted returns an Option that adds a hook to be executed when the lifecycle
// is started.
func OnStarted(hook func(name string)) Option {
	return func(o *lifecycleOptions) {
		o.startedHooks = append(o.startedHooks, hook)
	}
}

// OnComplete returns an Option that adds a hook to be executed when the
// lifecycle is completed.
func OnComplete(hook func(name string)) Option {
	return func(o *lifecycleOptions) {
		o.completedHooks = append(o.completedHooks, hook)
	}
}

// ForkOption is a function that configures a forked sub-lifecycle.
type ForkOption func(*lifecycleOptions)

// WithForkName sets the name of the new forked lifecycle. If no name is
// provided, the name of the program is used (i.e., os.Args[0]).
func WithForkName(name string) ForkOption {
	return func(o *lifecycleOptions) {
		o.name = name
	}
}

// WithForkSpanName overrides the name of the new forked lifecycle in the trace.
// If no name is provided, the name of the lifecycle is used.
func WithForkSpanName(name string) Option {
	return func(o *lifecycleOptions) {
		o.span = name
	}
}

// WithForkCompletion closes the given channel to signal that the new lifecycle
// has completed its execution; that is, its root procedure, child lifecycles and
// its cleanup functions.
func WithForkCompletion(done chan struct{}) ForkOption {
	return func(o *lifecycleOptions) {
		o.done = done
	}
}
