package loader

import (
	"context"
	"log"
	"os"
	"os/signal"
	"runtime/pprof"
	"syscall"

	"github.com/danielorbach/go-component"
)

func EntrypointProc(main component.Proc, opts ...component.Option) {
	Entrypoint(main, opts...)
}

func Entrypoint(main component.Procedure, opts ...component.Option) {
	opts = append(opts,
		component.WithContext(context.Background()),
		component.WithLogger(log.New(os.Stderr, "", log.LstdFlags|log.LUTC|log.Lmicroseconds)),
	)
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
