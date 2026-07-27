package component

import (
	"context"
	"errors"
	"log"
	"log/slog"
)

// The Procedure is the primary procedure of the component lifecycle.
//
// During Exec authors should use the L parameter to interact with the current
// lifecycle:
//   - L.Go starts a new goroutine and returns immediately; the component does not
//     complete until all goroutines started by L.Go have been completed.
//   - L.Cleanup (and variants) registers a function to be called when the
//     component completes.
//   - L.Context returns the context associated with the component; pass it
//     to slog's Context methods so log records identify the component.
//   - L.Terminate cancels the component context when managed children must be
//     told to exit before the procedure returns.
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
		handler:   slog.DiscardHandler,
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
	ctx            context.Context
	done           chan struct{}
	stopper        <-chan struct{}
	handler        slog.Handler
	procedure      Procedure
	startedHooks   []func(name string)
	completedHooks []func(name string)
}

func (o lifecycleOptions) validate() error {
	var errs error
	if o.ctx == nil {
		errs = errors.Join(errs, errors.New("context is nil"))
	}
	if o.handler == nil {
		errs = errors.Join(errs, errors.New("handler is nil"))
	}
	return errs
}

// Name returns the name associated with the lifecycle options.
func (o lifecycleOptions) Name() string {
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

// Handler returns the slog.Handler configured for the lifecycle, which
// receives the lifecycle's own log records.
func (o lifecycleOptions) Handler() slog.Handler {
	return o.handler
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

// WithSpan used to override the name of the span that component held open for
// the lifetime of a lifecycle. Component no longer creates that span, so the
// option has no effect. It remains as a no-op for v1 source compatibility.
//
// Deprecated: instead of configuring a lifecycle span, start bounded operation
// spans from [L.Context] and name them at their call sites.
func WithSpan(string) Option {
	return func(*lifecycleOptions) {}
}

// WithContext sets the context from which the new lifecycle derives
// cancellation, deadlines, and values. If no context is provided, a background
// context is used.
//
// A span carried by ctx is not inherited as the parent of spans started from
// [L.Context]. A component procedure is a trace boundary: its operations start
// independent traces rather than accumulating under a lifecycle-long parent.
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

// WithLogHandler directs the lifecycle's own log records, and those of its
// children started via L.Go and L.Fork, to the given handler. Omitting the
// option discards the records; a nil handler is rejected, not treated as
// discard.
func WithLogHandler(handler slog.Handler) Option {
	return func(o *lifecycleOptions) {
		o.handler = handler
	}
}

// WithDefaultLogHandler directs the lifecycle's own log records to the handler
// of [slog.Default], captured when the option is applied. It is the common
// wiring for a program that has already installed its process-wide logger and
// wants the lifecycle's records to join it, and is shorthand for
// WithLogHandler(slog.Default().Handler()).
func WithDefaultLogHandler() Option {
	return func(o *lifecycleOptions) {
		o.handler = slog.Default().Handler()
	}
}

// WithLogger directs the lifecycle's own log records to the given logger by
// writing them, as slog text, to that logger's output.
//
// Build the handler directly and pass it to [WithLogHandler] instead:
//
//	component.WithLogHandler(slog.NewTextHandler(w, nil))
//
// Routing through a [*log.Logger] only borrows its output writer: the logger's
// prefix and flags are ignored, and it carries neither levels nor attributes.
//
// Deprecated: use [WithLogHandler], as shown above. Removal is reserved for a
// future major version.
func WithLogger(logger *log.Logger) Option {
	if logger == nil {
		return WithLogHandler(slog.DiscardHandler)
	}
	slog.Warn("component.WithLogger is deprecated; use component.WithLogHandler(slog.NewTextHandler(w, nil)) instead")
	return WithLogHandler(slog.NewTextHandler(logger.Writer(), nil))
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

// WithForkSpanName used to override the name of the span that component held
// open for the lifetime of a forked lifecycle. Component no longer creates that
// span, so the option has no effect. It remains as a no-op for v1 source
// compatibility.
//
// Deprecated: start bounded operation spans from [L.Context] and name them at
// their call sites.
func WithForkSpanName(string) Option {
	return func(*lifecycleOptions) {}
}

// WithForkCompletion closes the given channel to signal that the new lifecycle
// has completed its execution; that is, its root procedure, child lifecycles and
// its cleanup functions.
func WithForkCompletion(done chan struct{}) ForkOption {
	return func(o *lifecycleOptions) {
		o.done = done
	}
}
