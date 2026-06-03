package component_test

import (
	"fmt"

	"github.com/danielorbach/go-component"
)

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
