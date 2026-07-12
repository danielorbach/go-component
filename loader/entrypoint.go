package loader

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"runtime/pprof"
	"syscall"

	"github.com/danielorbach/go-component"
)

// EntrypointProc is [Entrypoint] for a procedure expressed as a [component.Proc].
func EntrypointProc(main component.Proc, opts ...component.Option) {
	Entrypoint(main, opts...)
}

// Entrypoint runs main as the program's root lifecycle and blocks until it, its
// child lifecycles, and their cleanup have all finished.
//
// It supplies a background context and routes the lifecycle's own log records
// to the handler of the process-wide default slog logger, captured when
// Entrypoint is called, so configure slog before calling it. Then it installs
// a signal handler: the first SIGINT or SIGTERM asks the lifecycle to stop
// gracefully, and a second SIGINT terminates it abruptly. The supplied options
// are applied after these defaults, so a caller may override them.
func Entrypoint(main component.Procedure, opts ...component.Option) {
	opts = append([]component.Option{
		component.WithContext(context.Background()),
		component.WithHandler(slog.Default().Handler()),
	}, opts...)
	component.Run(signalMiddleware{base: main}, opts...)
}

// A Procedure middleware that listens for SIGINT/SIGTERM signals in order to
// trigger a soft and hard stop of the executed procedure.
type signalMiddleware struct {
	base component.Procedure
}

func (m signalMiddleware) Exec(l *component.L) {
	l.Log("Press Ctrl-C to stop gracefully; press again to terminate abruptly.")
	go pprof.Do(l.Context(), pprof.Labels("stop-signal", "soft"), func(context.Context) { m.softStop(l) })
	go pprof.Do(l.Context(), pprof.Labels("stop-signal", "hard"), func(context.Context) { m.hardStop(l) })
	m.base.Exec(l)
}

// a soft-stop initiates a shutdown of the provided lifecycle after a signal
// (SIGINT/SIGTERM) has been received, without waiting for it to complete.
//
// it is important to handle both SIGINT and SIGTERM since:
//
//  1. when running from the commandline, the user will press Ctrl-C,
//     which generates SIGINT, however
//  2. when running in Kubernetes, the process will be sent SIGTERM
//     to initiate a controlled/graceful shutdown.
func (m signalMiddleware) softStop(l *component.L) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)

	select {
	case <-sig:
		signal.Stop(sig) // unregister the signal handler early
		l.Log("Received soft-stop signal; stopping...")
		l.Stop(0)
	case <-l.Done():
	}
}

// a hard-stop terminates the provided lifecycle after a signal (SIGINT)
// has been received twice; the first signal is handled concurrently by softStop.
//
// when running from the commandline, it is helpful to let the second Ctrl-C do a
// forced stop, but when running in a managed setting, it is risky to let a
// second SIGTERM trigger a forced stop - a forced stop should be done via the
// standard SIGKILL.
func (m signalMiddleware) hardStop(l *component.L) {
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	for range 2 {
		select {
		case <-sig:
		case <-l.Done():
			return
		}
	}
	l.Log("Received hard-stop signal; terminating...")
	l.Terminate()
}
