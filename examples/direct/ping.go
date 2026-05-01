package main

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/MakeNowJust/heredoc"
	"gocloud.dev/pubsub"

	"github.com/danielorbach/go-component"
)

const PingAspect = "ping"

var (
	PingInterval time.Duration
)

func init() {
	PingComponent.Flags.DurationVar(&PingInterval, "interval", 2*time.Second, "interval between pings")
}

var PingComponent = &component.Descriptor{
	Name: "ping",
	Doc: heredoc.Doc(`
	continually publishes a simple message to the PingAspect aspect
	
	The ping component sends simple messages every PingInterval duration on
	its aspect (PingAspect).
	`),
	Bootstrap: func(l *component.L, linker component.Linker, options any) error {
		pub, err := linker.LinkAspect(l.Context(), PingAspect)
		if err != nil {
			return fmt.Errorf("open aspect: %w", err)
		}
		l.CleanupBackground(pub.Shutdown)

		l.Go("pub", func(l *component.L) {
			options := options.(PingOptions)
			t := time.NewTicker(PingInterval)
			defer t.Stop()
			l.Logf("pinging every %v", PingInterval)

			for i := 0; l.Continue(); i++ {
				select {
				case <-t.C:
					text := fmt.Sprintf("%s (seq=%d)", options.Data, i)
					err := pub.Send(l.Context(), &pubsub.Message{Body: []byte(text)})
					if err != nil {
						l.Error(fmt.Errorf("send ping: %w", err))
					}
				case <-l.Stopping():
					l.Log("graceful stop")
				case <-l.Context().Done():
					l.Log("abrupt stop:", context.Cause(l.Context()))
				}
			}
		})

		return nil
	},
	OptionsType: reflect.TypeFor[PingOptions](),
	Aspects:     nil,
	Interests:   nil,
}

type PingOptions struct {
	Data []byte // data to send on ping
}
