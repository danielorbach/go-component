package component_test

import (
	"fmt"
	"time"

	"github.com/danielorbach/go-component"
)

// Example_gracefulShutdown shows a component that does periodic work and stops
// cleanly when the surrounding program asks it to. Closing the stopper channel,
// which in production is fed by an interrupt signal, unblocks L.Stopping so the
// procedure can return and let the lifecycle run its cleanup.
func Example_gracefulShutdown() {
	// Closing this channel is the request to wind down; a real program feeds it
	// from an interrupt signal.
	shutdown := make(chan struct{})
	go func() {
		// Stand in for a signal that arrives a short while after startup.
		time.Sleep(10 * time.Millisecond)
		close(shutdown)
	}()

	component.RunProc(func(l *component.L) {
		fmt.Println("working")
		for l.Continue() {
			select {
			case <-time.After(time.Millisecond):
				// Do one unit of periodic work here.
			case <-l.Stopping():
				// The request arrived. Returning runs the lifecycle's cleanup
				// instead of abandoning work midway.
				fmt.Println("stopped gracefully")
				return
			}
		}
	}, component.WithStopper(shutdown))

	// Output:
	// working
	// stopped gracefully
}
