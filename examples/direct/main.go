// This 'direct' example demonstrates how to use loader package to manually
// construct a loader.Footprint with resource allocations that link two
// components together - ping & pong.
//
// These components communicate over an in-memory pub-sub channel just for
// the purposes of this example. In production code, event notifications
// over Kafka is the most prevalent alternative.
package main

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/danielorbach/go-component"
	"github.com/danielorbach/go-component/loader"
)

func main() {
	loader.ParseFlags(PingComponent, PongComponent)

	var binding loader.SharedMemLinker

	fp := loader.Footprint{
		Name:       "Static ping-pong-probe",
		Metadata:   "This is a static footprint loading three components: ping, pong, and probe.",
		Identifier: uuid.Nil, // fake identifier for a fake footprint
		Revision:   0,        // revision 0 is the only revision for this footprint
		Locations:  nil,      // locations are not supported yet
		Allocations: []*loader.Claim{
			{
				Component: PingComponent,
				Options:   PingOptions{Data: []byte("Hello Stewart")},
				Binding:   &binding,
			},
			{
				Component: PongComponent,
				Options:   nil,
				Binding:   &binding,
			},
		},
	}

	loader.EntrypointProc(func(l *component.L) {
		// first, start a probe subcomponent to output the ping-ponged messages
		l.Go("prober", func(l *component.L) {
			sub, err := binding.LinkInterest(l.Context(), PongAspect)
			if err != nil {
				l.Fatal(err)
			}
			l.CleanupBackground(sub.Shutdown)

			for l.Continue() {
				msg, err := sub.Receive(l.GraceContext())
				if err != nil {
					l.Error(fmt.Errorf("receive: %w", err))
					continue
				}
				msg.Ack()
				l.Log(string(msg.Body))
			}
		})

		// then, load the footprint to start the ping-pong components
		loader.Load(fp)
	})
}
