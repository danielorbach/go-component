package component_test

import (
	"fmt"

	"github.com/danielorbach/go-component"
)

// Example_gracefulShutdown shows how a component does its work and stops cleanly
// when the surrounding program asks it to. WithStopper feeds in the stop request;
// closing that channel, which a real program does on an interrupt signal, makes
// l.Continue() report that it is time to wind down, so the procedure returns and
// lets the lifecycle complete its shutdown.
func Example_gracefulShutdown() {
	// A real program closes this when it receives an interrupt signal.
	shutdown := make(chan struct{})
	go func() { close(shutdown) }()

	component.RunProc(func(l *component.L) {
		fmt.Println("working")

		// Keep working until the component is asked to stop. l.Continue()
		// returns false once the stop request arrives, which ends the loop.
		for l.Continue() {
			// Do one unit of work here: handle a message, advance a job, poll
			// a source. Keep each unit short so the loop checks l.Continue()
			// often and reacts to the stop request promptly.
		}

		fmt.Println("stopped gracefully")
	}, component.WithStopper(shutdown))

	// Output:
	// working
	// stopped gracefully
}
