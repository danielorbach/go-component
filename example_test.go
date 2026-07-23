package component_test

import (
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

// A lifecycle is an slog.LogValuer, so a call site holding one attaches the
// component's identity by logging the lifecycle itself under component.LogKey.
// The record then says which component wrote it.
func ExampleL_LogValue() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, stableOptions))

	component.RunProc(func(l *component.L) {
		logger.LogAttrs(l.Context(), slog.LevelInfo, "ready", slog.Any(component.LogKey, l))
	}, component.WithName("pinger"))

	// Output:
	// level=INFO msg=ready component.name=pinger
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
// terminates the component, and L.Fork any [component.Procedure], such as job.
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
