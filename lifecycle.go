package component

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime"
	"runtime/pprof"
	"strconv"
	"sync"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// TODO(@danielorbach): We think we can move Run() to lifecycle, let's consider
//  it while refactoring.

// ErrTerminated is the cause of L.Context() and L.GraceContext() cancellation
// due to a call to L.Terminate().
var ErrTerminated = errors.New("component terminated")

// ErrStopped is the cause of L.GraceContext() cancellation due to a call to
// L.Stop(). It indicates that the component is in the process of stopping
// gracefully.
var ErrStopped = errors.New("component gracefully stopped")

// execute is the only function that initiates a new lifecycle. it is unexported
// because L is not intended to be created directly by users, but rather by
// providing a Procedure to Run or by calling a method of an existing L.
//
// Call this function in its own goroutine, because down its call-stack there are
// potential calls to runtime.Goexit.
func execute(ctx context.Context, options lifecycleOptions) {
	// Set labels on the context to ensure that all goroutines started by the
	// lifecycle are labelled with the lifecycle name. Note, all related goroutines
	// should be started with pprof.Do to ensure that they are labelled correctly.
	ctx = pprofIncrementLabel(ctx, "component.depth")
	pprof.Do(ctx, pprof.Labels("component.name", options.Name()), func(ctx context.Context) {
		done := make(chan struct{}) // make ahead of &L for better readability

		// The lifecycle span captures observability for the lifecycle event itself
		// (started, completed, errors). It is intentionally NOT propagated through
		// l.Context(): handlers and other per-iteration work that derive spans from
		// l.Context() should produce traces bounded by the work done per invocation,
		// not by component uptime. The span is held on L for direct interaction
		// (L.Error, L.Fatal, L.Span); child lifecycle spans nest under it via Fork,
		// which re-attaches it to the context handed to the child execute.
		//
		// TODO: attach caller-defined attributes to the span
		_, span := tracer.Start(ctx, options.SpanName())
		// the span ends when the lifecycle completes - this must be done asynchronously
		go pprof.Do(ctx, pprof.Labels("component.reaper", "span"), func(context.Context) {
			<-done
			span.End()
		})

		// Detach any inherited lifecycle span from the user-facing ctx so spans
		// started from l.Context() are roots of bounded traces.
		ctx = trace.ContextWithSpan(ctx, noop.Span{})
		ctx, cancel := context.WithCancelCause(ctx)
		// do not leak a context.Context
		go pprof.Do(ctx, pprof.Labels("component.reaper", "context"), func(context.Context) {
			<-done
			cancel(nil)
		})

		stopping := make(chan struct{})
		graceCtx, cancelGraceCtx := context.WithCancelCause(ctx)
		go pprof.Do(ctx, pprof.Labels("component.reaper", "graceful-context"), func(context.Context) {
			select {
			case <-stopping:
				cancelGraceCtx(ErrStopped)
			case <-done:
				// by the time done is closed, the graceful context is already cancelled because
				// it is a child of the lifecycle context
			}
		})

		l := &L{
			name:     options.Name(),
			ctx:      ctx,
			cancel:   cancel,
			graceCtx: graceCtx,
			span:     span,
			done:     done,
			common: common{
				logger:         options.Logger(),
				stopping:       stopping,
				statedHooks:    options.startedHooks,
				completedHooks: options.completedHooks,
			},
		}

		// propagate external stop signal to the new lifecycle
		if options.Stopper() != nil {
			go pprof.Do(ctx, pprof.Labels("component.reaper", "stopping"), func(context.Context) {
				l.propagateStop(options.Stopper())
			})
		}

		// broadcast to external receivers (if any) that the lifecycle is done
		if options.Done() != nil {
			go pprof.Do(ctx, pprof.Labels("component.reaper", "completion"), func(ctx context.Context) {
				l.broadcastDone(options.Done())
			})
		}

		// We are using the registered lifecycle options, started and completed hooks, in
		// order to signal the global health probe. We are immediately signalling that
		// the lifecycle has started, and deferring it to signal the completion of one.
		defer options.Completed(options.Name())
		options.Started(options.Name())

		// Execute the lifecycle procedure in the current goroutine.
		l.exec(options.Procedure())
	})
}

// pprofIncrementLabel returns a sub-context of ctx with the value of the given
// label incremented by 1 if the label exists, or set to 0 otherwise.
func pprofIncrementLabel(ctx context.Context, label string) context.Context {
	v, ok := pprof.Label(ctx, label)
	if !ok {
		return pprof.WithLabels(ctx, pprof.Labels(label, "0"))
	}

	d, err := strconv.Atoi(v)
	if err != nil {
		panic(fmt.Sprintf("component: non-integer %s pprof label = %q", label, v))
	}
	return pprof.WithLabels(ctx, pprof.Labels(label, strconv.Itoa(d+1)))
}

// L manages concurrent execution lifecycle and supports formatted logs.
//
// A lifecycle ends when its Procedure returns or calls Fatal. This is the
// only way to exit a lifecycle. When called from another goroutine, Fatal will
// not be able to exit the lifecycle.
//
// The other reporting methods, such as the variations of Log and Error,
// may be called simultaneously from multiple goroutines.
type L struct {
	ctx      context.Context
	cancel   context.CancelCauseFunc
	graceCtx context.Context // cancelled when stopping is closed

	// span is the lifecycle span. It is not propagated through ctx; spans
	// started from l.Context() are roots, bounded to per-invocation work.
	// See Span() for how to interact with it (set attributes, link from
	// per-handler traces) and the package documentation for the framework's
	// tracing contract.
	span trace.Span

	done chan struct{} // closed when all lifecycle goroutines and cleanup functions have finished
	wg   sync.WaitGroup

	stopOnce sync.Once

	name string

	cleanups funcStack

	common common
}

// common holds shared resources and behaviors for managing the execution and
// termination of lifecycles within the application. It provides common
// structures and hooks that facilitate smooth operation and graceful shutdown
// processes.
type common struct {
	logger         *log.Logger
	stopping       chan struct{} // closed when the lifecycle is shutting down gracefully
	statedHooks    []func(name string)
	completedHooks []func(name string)
}

func (l *L) Name() string {
	return l.name
}

func (l *L) Context() context.Context {
	return l.ctx
}

func (l *L) GraceContext() context.Context {
	return l.graceCtx
}

func (l *L) Done() <-chan struct{} {
	return l.done
}

// Span returns the lifecycle span. Its lifetime spans the entire component
// uptime, so it must NOT be used as a parent for per-handler or per-iteration
// work — that would produce traces that grow without bound and saturate trace
// backends. Use it to record lifecycle observability (attributes, events,
// errors) and to link short-lived handler traces back to the lifecycle for
// navigation in trace backends:
//
//	_, handler := tracer.Start(l.Context(), "consume",
//		trace.WithLinks(trace.Link{SpanContext: l.Span().SpanContext()}))
//
// Spans started from l.Context() are roots of independent traces — that is the
// per-handler scope. The lifecycle span is the lifecycle scope.
func (l *L) Span() trace.Span {
	return l.span
}

// exec should be called in its own goroutine and only once.
//
// note it is perfectly valid for a lifecycle to run its course from beginning to
// completion successfully while it has been signalled to stop - it may ignore
// the stopping signal.
func (l *L) exec(logic Procedure) {
	defer func() {
		// We want to log the reason for the lifecycle termination. Since we are working
		// with two different contexts, where one is the parent of the other, it is
		// sufficient to only check the cancellation cause of the child context.
		//
		// If the parent context was canceled first, its cause propagates to the child
		// context. In which case, we get the cause from the child context.
		//
		// If the child context was canceled first, we also get the cause from the child
		// context. However, if the parent context is also canceled after the child, we
		// won't be able to determine which one of them the lifecycle has reacted to and
		// the log will contain the child's cause.
		//
		// We use defer() because l.Exec() may panic, directly or as a result of
		// l.Fatal().
		//
		// TODO: In component v2, we need to consider error management for both the main lifecycle
		//		 and sub-lifecycles. For example, l.Fatal() sets the context cancellation cause
		// 		 based on the error from the calling procedure
		if ctxCause := context.Cause(l.graceCtx); ctxCause != nil {
			l.Logf("Lifecycle completed: %s", ctxCause)
		} else {
			l.Log("Lifecycle completed")
		}
	}()
	// close done channel after all child goroutines have finished and all cleanup
	// funcs have been called.
	defer close(l.done)
	// defer cleanup funcs to run despite runtime.Goexit() - which is called by
	// l.Fatal().
	defer l.runCleanup()
	// wait for goroutines started within the lifecycle procedure to finish before
	// running cleanup funcs.
	defer l.wg.Wait()
	// calling the provided procedure in the same goroutine is crucial for l.Fatal().
	logic.Exec(l)
}

// Go derives a new lifecycle from the current lifecycle and executes the given
// function in a new goroutine, passing this new lifecycle as its only argument.
//
// This function returns without waiting for the new lifecycle completion and is
// safe for concurrent use.
//
// The new lifecycle completes when the provided Proc returns (and all the
// cleanup functions registered during its execution have returned as well).
//
// The new lifecycle Context() is a child of the current lifecycle Context().
//
// The new lifecycle Stopping() channel is closed when the current lifecycle
// Stopping() channel is closed.
//
// Terminating the current lifecycle will terminate the new lifecycle as well.
//
// This function panics if the current lifecycle is already stopping or if it has
// already completed.
func (l *L) Go(name string, proc Proc) {
	l.Fork(name, proc)
}

// Fork derives a new lifecycle from the current lifecycle and executes the given
// Procedure in a new goroutine, passing this new lifecycle as its only argument.
//
// This function returns without waiting for the new lifecycle completion and is
// safe for concurrent use.
//
// The new lifecycle completes when the provided Procedure returns (and all the
// cleanup functions registered during its execution have returned as well).
//
// The new lifecycle Context() is a child of the current lifecycle Context().
//
// The new lifecycle Stopping() channel is closed when the current lifecycle
// Stopping() channel is closed.
//
// Terminating the current lifecycle will terminate the new lifecycle as well.
//
// This function panics if the current lifecycle is already stopping or if it has
// already completed.
func (l *L) Fork(name string, procedure Procedure, opts ...ForkOption) {
	select {
	case <-l.common.stopping:
		panic("component: cannot Go after component has started stopping")
	case <-l.done:
		panic("component: cannot Go after component has terminated")
	default:
	}

	l.wg.Add(1)
	// Re-attach the lifecycle span so the child's lifecycle span nests under
	// it. Lifecycle spans are the only spans the framework guarantees nest
	// (parent-component to child-component); per-handler spans started from
	// l.Context() are roots by design. See package documentation.
	forkCtx := trace.ContextWithSpan(l.ctx, l.span)
	go pprof.Do(forkCtx, pprof.Labels("parent-component", l.name), func(ctx context.Context) {
		defer l.wg.Done()
		fullName := l.name + "/" + name // the child's name is appended to that of the parent (also used for tracing)
		options := lifecycleOptions{
			name:           fullName,
			span:           fullName,
			ctx:            ctx,
			done:           nil,
			stopper:        l.common.stopping,
			logger:         l.common.logger,
			procedure:      procedure,
			startedHooks:   l.common.statedHooks,
			completedHooks: l.common.completedHooks,
		}
		for _, opt := range opts {
			opt(&options)
		}
		execute(ctx, options)
	})
}

// ForkE is like Go except it calls Fatal if the function returns an error.
func (l *L) ForkE(name string, proc ProcE) {
	l.Fork(name, proc)
}

// propagateStop monitors the current lifecycle and the parent channel until
// either:
//
//  1. the parent signal is received -> propagates it to the child (current)
//     lifecycle by closing the stopping channel.
//
//  2. the lifecycle is done, in which case it ignores the parent signal, and
//     nothing else happens.
//
//  3. the lifecycle context expires, in which case it ignores the parent signal
//     meticulously to encourage the code pattern described in Stopping().
func (l *L) propagateStop(parent <-chan struct{}) {
	select {
	case <-parent:
		l.stopOnce.Do(func() {
			close(l.common.stopping)
		})
	case <-l.done:
	case <-l.ctx.Done():
	}
}

// function broadcastDone waits for the current lifecycle to complete, then it
// closes the provided broadcast channel.
//
// the function must not select on any other signals to terminate to prevent a
// leak because a goroutine is considered leaked only if it persists far beyond
// the lifecycle has completed.
func (l *L) broadcastDone(broadcast chan<- struct{}) {
	<-l.done
	close(broadcast)
}

// Stopping returns a channel that is closed to signal the lifecycle procedure to
// stop gracefully; it is not closed when the lifecycle context expires.
//
// The following pattern is recommended:
//
//	select {
//	case <-l.Stopping():
//		// gracefully stop
//	case <-l.Context().Done():
//		// handle timeout/abortion
//	}
func (l *L) Stopping() <-chan struct{} {
	return l.common.stopping
}

// Continue returns false if the lifecycle has been signalled to stop, otherwise
// it returns true indicating that the lifecycle should continue.
//
// The following pattern is recommended:
//
//	for l.Continue() {
//		// do something, commonly with l.Context()
//	}
func (l *L) Continue() bool {
	select {
	case <-l.common.stopping:
		return false
	default:
		return true
	}
}

// Stop returns true if the lifecycle has stopped gracefully
// within the timeout; otherwise, it returns false.
// This function is safe for concurrent use.
func (l *L) Stop(timeout time.Duration) (stopped bool) {
	l.stopOnce.Do(func() {
		close(l.common.stopping)
		runtime.Gosched() // optimisation: give a chance to the goroutines to stop
	})

	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-l.done:
		return true
	case <-t.C:
		return false
	}
}

func (l *L) Terminate() {
	l.cancel(ErrTerminated)
}

func (l *L) log(s string) {
	l.common.logger.Print(l.name + "$ " + s)
}

func (l *L) Logf(format string, args ...any) {
	// TODO: mimic testing.common.decorate for pretty output
	l.log(fmt.Sprintf(format, args...))
}

func (l *L) Log(args ...any) {
	l.log(fmt.Sprint(args...))
}

func (l *L) Error(err error) {
	l.Logf("error: %v", err)
	l.span.RecordError(err)
}

func (l *L) Errorf(format string, a ...any) {
	l.Error(fmt.Errorf(format, a...))
}

// Fatal behaves like Error except it terminates the lifecycle.
//
// When called from goroutines other than the primary lifecycle goroutine,
// it can't terminate the lifecycle goroutine. Nonetheless, it will
// cancel the lifecycle context in order to signal to all goroutines
// that the lifecycle is terminating.
//
// DO NOT call Fatal from goroutines other than those which started the lifecycle
// (i.e. the goroutine spawned by Run/L.Go/L.Fork/L.ForkE).
func (l *L) Fatal(err error) {
	select {
	case <-l.done:
		// when called from a goroutine, we can't terminate the lifecycle
		// goroutine, so we have to panic.
		panic("component: Fatal in goroutine after component has completed")
	default:
	}

	l.Logf("fatal error: %v", err)
	// marking the span as errored is a good practice
	l.span.RecordError(err)
	l.span.SetStatus(codes.Error, err.Error())
	// cancel the lifecycle context because a fatal error is unrecoverable, hence
	// there is no point in continuing.
	l.cancel(fmt.Errorf("fatality: %w", err))
	// when called from within the main lifecycle goroutine, we can
	// terminate it, which will run all deferred cleanup functions.
	runtime.Goexit()
}

func (l *L) Fatalf(format string, a ...any) {
	l.Fatal(fmt.Errorf(format, a...))
}

// A CleanupFunc is a function that is called just before the lifecycle
// completes - either by returning from a Proc or by calling L.Fatal.
//
// CleanupFuncs are called even if the lifecycle is terminated by calling
// Terminate or if the context is cancelled before the lifecycle completes - for
// this reason, cleanup functions must not rely on the context being valid.
//
// See L.Cleanup, L.CleanupError and L.CleanupContext for more details.
type CleanupFunc func()

// funcStack stores a bunch of cleanup functions in LIFO order.
type funcStack struct {
	q  []CleanupFunc
	mu sync.Mutex
}

func (q *funcStack) Push(fn func()) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.q = append(q.q, fn)
}

// Pop returns the last cleanup function, or nil if there are no more.
func (q *funcStack) Pop() (fn func()) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.q) == 0 {
		return nil
	}
	last := len(q.q) - 1
	fn, q.q = q.q[last], q.q[:last]
	return fn
}

// runCleanup is called at the end of the lifecycle to invoke its cleanup funcs
// in LIFO (stack) order.
//
// once called, a cleanup function is removed from the list - it will not be
// called again (e.g., if it panics).
func (l *L) runCleanup() {
	var done bool // whether we've finished running all cleanup funcs
	defer func() {
		// if we were interrupted during cleanup, we need to
		// continue running the cleanup functions.
		// this is a bit of a hack, but it's the only way to
		// ensure that cleanup functions are called.
		if !done {
			// we can only get here if we were interrupted by a panic
			// or a call to runtime.Goexit().
			reason := recover()
			if reason == nil {
				// we were interrupted by a call to runtime.Goexit(),
				// most likely from a call to l.Fatal().
				l.runCleanup()
			} else {
				// we need to re-panic the original panic value
				// so that the program exits and no more cleanup
				// functions are called.
				panic(reason)
			}
		}
	}()

	for {
		fn := l.cleanups.Pop()
		if fn == nil {
			done = true
			return
		}
		fn()
	}
}

// Cleanup registers the given function to be called after the lifecycle has
// completed, in LIFO (stack) order.
//
// Cleanup functions are called even if the lifecycle is terminated; however,
// the lifecycle context is cancelled at the time of termination, so cleanup
// should be aware of this.
//
// Cleanup functions are called in the same goroutine as the lifecycle body.
func (l *L) Cleanup(fn func()) {
	select {
	case <-l.done:
		panic("component: cannot register a cleanup function after component has terminated")
	default:
	}
	l.cleanups.Push(fn)
}

// CleanupError registers the given function to be called after the lifecycle
// has completed, like Cleanup; However, if the function returns an error, it
// is logged using the Error() function.
//
// This helper is useful for calling cleanup functions that return errors,
// such as io.Closer.Close().
func (l *L) CleanupError(fn func() error) {
	l.Cleanup(func() {
		if err := fn(); err != nil {
			// TODO: consider a more predictable way to contextually log errors
			l.Error(fmt.Errorf("during cleanup of %s: %w", l.Name(), err))
		}
	})
}

// CleanupContext registers the given function to be called after the lifecycle
// has completed, like CleanupError; However, the function is passed the context
// of the lifecycle.
//
// This helper is useful for calling cleanup functions that require a context,
// like http.Server.Shutdown(). However, the context passed to the function may
// be cancelled by the time the function is called. Callers should be aware of
// this, or use CleanupBackground instead.
func (l *L) CleanupContext(fn func(context.Context) error) {
	l.CleanupError(func() error { return fn(l.ctx) })
}

// CleanupBackground registers the given function to be called after the
// lifecycle has completed, like CleanupContext; However, a background context is
// passed to the function.
//
// This helper is useful for calling cleanup functions that require a context,
// like http.Server.Shutdown(), but should not be cancelled when the lifecycle is
// terminated.
func (l *L) CleanupBackground(fn func(context.Context) error) {
	l.CleanupError(func() error { return fn(context.Background()) })
}
