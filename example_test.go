package component_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/danielorbach/go-component"
)

// stableOptions drops the timestamp, so an example's Output stays reproducible.
// A real program keeps the timestamp.
var stableOptions = &slog.HandlerOptions{
	ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey {
			return slog.Attr{}
		}
		return a
	},
}

// A component's log records say which component wrote them. The identity rides
// on the lifecycle context, so slog's Context-suffixed methods carry it to a
// WrapLogHandler-wrapped handler, which stamps it onto the record. The plain
// methods take no context, so where a call site cannot pass one, attach the
// lifecycle yourself under [component.LogKey] - at the call site, or once on a
// logger.
func ExampleWrapLogHandler() {
	// A real program installs this once at startup, with slog.SetDefault.
	logger := slog.New(component.WrapLogHandler(slog.NewTextHandler(os.Stdout, stableOptions)))

	component.RunProc(func(l *component.L) {
		logger.InfoContext(l.Context(), "carried by the context")
		logger.Info("no context, so no component")
		logger.Info("attached at the call site", slog.Any(component.LogKey, l))
		logger.With(slog.Any(component.LogKey, l)).Info("attached once, on a logger")
	}, component.WithName("pinger"))

	// Output:
	// level=INFO msg="carried by the context" component.name=pinger
	// level=INFO msg="no context, so no component"
	// level=INFO msg="attached at the call site" component.name=pinger
	// level=INFO msg="attached once, on a logger" component.name=pinger
}

// Attach the identity through one channel only. A logger already carrying the
// lifecycle that passes it again at the call site names the component twice.
func ExampleWrapLogHandler_avoidDoubleNaming() {
	logger := slog.New(component.WrapLogHandler(slog.NewTextHandler(os.Stdout, stableOptions)))

	component.RunProc(func(l *component.L) {
		logger.With(slog.Any(component.LogKey, l)).
			InfoContext(l.Context(), "named twice", slog.Any(component.LogKey, l))
	}, component.WithName("pinger"))

	// Output:
	// level=INFO msg="named twice" component.name=pinger component.name=pinger
}

// Wrapping a handler that is already wrapped is safe. Both layers see the
// lifecycle on the context, but the inner one finds the outer's work already on
// the record and leaves it, so the component is named once however many layers
// a program stacks up.
func ExampleWrapLogHandler_nested() {
	inner := slog.NewTextHandler(os.Stdout, stableOptions)
	logger := slog.New(component.WrapLogHandler(component.WrapLogHandler(inner)))

	component.RunProc(func(l *component.L) {
		logger.InfoContext(l.Context(), "handled message")
	}, component.WithName("pinger"))

	// Output:
	// level=INFO msg="handled message" component.name=pinger
}

// Attributes of your own sit alongside the component name; refining a logger
// with [slog.Logger.With] does not cost you the attribution.
func ExampleWrapLogHandler_moreAttributes() {
	logger := slog.New(component.WrapLogHandler(slog.NewTextHandler(os.Stdout, stableOptions)))

	component.RunProc(func(l *component.L) {
		logger.With("phase", "boot").InfoContext(l.Context(), "ready")
	}, component.WithName("pinger"))

	// Output:
	// level=INFO msg=ready phase=boot component.name=pinger
}

// Where no WrapLogHandler wraps the handler, the context alone does not name the
// component: a procedure names it only by attaching the lifecycle by hand under
// LogKey, since a lifecycle is an [slog.LogValuer] that resolves to the name.
// Omit the attribute and the record is unnamed. The lifecycle's own records name
// their component either way.
func ExampleLogKey() {
	handler := slog.NewTextHandler(os.Stdout, stableOptions) // no WrapLogHandler
	logger := slog.New(handler)

	component.RunProc(func(l *component.L) {
		logger.InfoContext(l.Context(), "attached by hand", slog.Any(component.LogKey, l))
		logger.InfoContext(l.Context(), "no attribute, so no name")
	}, component.WithName("pinger"), component.WithLogHandler(handler))

	// Output:
	// level=INFO msg="attached by hand" component.name=pinger
	// level=INFO msg="no attribute, so no name"
	// level=INFO msg="lifecycle completed" component.name=pinger
}

// A component procedure logs through slog.Default, but the lifecycle running it
// stays silent unless given a handler. WithDefaultLogHandler points that
// lifecycle at slog.Default too, so the procedure's logs and the lifecycle's
// records land together, attributed to the component. A program wires each
// lifecycle this way, and a forked child inherits the same default.
func ExampleWithDefaultLogHandler() {
	// A real program installs this once at startup.
	defer slog.SetDefault(slog.Default())
	slog.SetDefault(slog.New(component.WrapLogHandler(slog.NewTextHandler(os.Stdout, stableOptions))))

	component.RunProc(func(l *component.L) {
		slog.InfoContext(l.Context(), "ready")
	}, component.WithName("pinger"), component.WithDefaultLogHandler())

	component.RunProc(func(l *component.L) {
		slog.InfoContext(l.Context(), "ready")
		// A forked child inherits the default; it logs under the compound name
		// server/worker, its own records in the same place as its parent's.
		l.Go("worker", func(c *component.L) {
			slog.InfoContext(c.Context(), "working")
		})
	}, component.WithName("server"), component.WithDefaultLogHandler())

	// Output:
	// level=INFO msg=ready component.name=pinger
	// level=INFO msg="lifecycle completed" component.name=pinger
	// level=INFO msg=ready component.name=server
	// level=INFO msg=working component.name=server/worker
	// level=INFO msg="lifecycle completed" component.name=server/worker
	// level=INFO msg="lifecycle completed" component.name=server
}

// A lifecycle given no handler discards its own records instead of falling back
// to slog.Default. Only the framework's records go quiet: the procedure still
// logs for itself through slog.Default, so embedding a component silences the
// lifecycle, not the program.
func ExampleRunProc_silentByDefault() {
	// A real program configures slog once; route it to stdout here.
	defer slog.SetDefault(slog.Default())
	slog.SetDefault(slog.New(component.WrapLogHandler(slog.NewTextHandler(os.Stdout, stableOptions))))

	component.RunProc(func(l *component.L) {
		// This reaches slog.Default and shows below. The lifecycle's own
		// "lifecycle completed" record has nowhere to go, so it does not.
		slog.InfoContext(l.Context(), "the procedure still logs")
	}, component.WithName("pinger"))

	// Output:
	// level=INFO msg="the procedure still logs" component.name=pinger
}

// A component that loops over units of work until the program asks it to stop.
// L.Continue reports false once the stop request arrives (delivered here through
// WithStopper, which a real program wires to an interrupt signal), so a work
// loop can check it between units and exit cleanly.
func ExampleL_Continue() {
	// A real program closes this when it receives an interrupt signal.
	shutdown := make(chan struct{})
	go func() { close(shutdown) }()

	component.RunProc(func(l *component.L) {
		fmt.Println("working")
		for l.Continue() {
			// Do one unit of work here: handle a message, advance a job, poll
			// a source. Keep each unit short so the loop reacts to the stop
			// request promptly.
		}
		fmt.Println("stopped")
	}, component.WithStopper(shutdown))

	// Output:
	// working
	// stopped
}

// A leaf procedure that has no managed children depending on cancellation can
// handle an error and simply return. Returning completes the procedure; the
// lifecycle then runs cleanup normally.
func ExampleProcedure_return() {
	component.RunProc(func(l *component.L) {
		l.Cleanup(func() { fmt.Println("leaf cleanup ran") })
		if err := errors.New("leaf failed"); err != nil {
			fmt.Println("leaf handled its error")
			return
		}
	})
	fmt.Println("component completed")

	// Output:
	// leaf handled its error
	// leaf cleanup ran
	// component completed
}

// An error-returning procedure can stop managed workers by returning its
// failure through ProcE. The lifecycle uses that error as its cancellation
// cause and waits for the workers before Run returns.
func ExampleProcE() {
	component.Run(component.ProcE(func(l *component.L) error {
		l.Go("worker", func(l *component.L) {
			<-l.Context().Done()
			fmt.Println("worker stopped:", context.Cause(l.Context()))
		})
		return errors.New("startup failed")
	}))

	// Output:
	// worker stopped: startup failed
}

// A supervisor terminates its lifecycle before returning when managed children
// need cancellation in order to exit. Returning without Terminate here would
// leave the worker blocked while the lifecycle waited for it. RunProc is the
// lifecycle's join point: it returns only after the canceled worker has stopped.
func ExampleL_Terminate() {
	component.RunProc(func(l *component.L) {
		l.Go("worker", func(l *component.L) {
			<-l.Context().Done()
			fmt.Println("worker canceled:", errors.Is(context.Cause(l.Context()), component.ErrTerminated))
		})

		if err := errors.New("supervisor failed"); err != nil {
			fmt.Println("supervisor handled its error")
			l.Terminate()
			return
		}
	})
	fmt.Println("component completed")

	// Output:
	// supervisor handled its error
	// worker canceled: true
	// component completed
}

// A component that has nothing to poll and simply blocks until the program asks
// it to stop. L.Stopping returns a channel that closes on the stop request
// (delivered here through WithStopper, which a real program wires to an
// interrupt signal), so the procedure waits on it and then returns to shut down.
func ExampleL_Stopping() {
	// A real program closes this when it receives an interrupt signal.
	shutdown := make(chan struct{})
	go func() { close(shutdown) }()

	component.RunProc(func(l *component.L) {
		fmt.Println("serving")
		<-l.Stopping()
		fmt.Println("stopped")
	}, component.WithStopper(shutdown))

	// Output:
	// serving
	// stopped
}

// job is a named [component.Procedure]: a type carrying its own data whose Exec
// method L.Fork runs. L.Go and L.ForkE take a function instead and adapt it.
type job struct{ msg string }

func (j job) Exec(*component.L) {
	fmt.Println(j.msg)
}

// A lifecycle manages the goroutines it spawns: RunProc returns only after every
// child it started has finished. The three methods differ in what they accept -
// L.Go a plain [component.Proc], L.ForkE a [component.ProcE] whose returned error
// cancels its lifecycle, and L.Fork any [component.Procedure], such as job.
func Example_managedGoroutines() {
	component.RunProc(func(l *component.L) {
		l.Go("go", func(*component.L) {
			fmt.Println("Go ran a Proc")
		})
		l.ForkE("forkE", func(*component.L) error {
			fmt.Println("ForkE ran a ProcE")
			return nil
		})
		l.Fork("fork", job{msg: "Fork ran a named Procedure"})
	}, component.WithName("parent"))
	// All three lines appear because RunProc waited for every child to finish.

	// Unordered output:
	// Go ran a Proc
	// ForkE ran a ProcE
	// Fork ran a named Procedure
}
