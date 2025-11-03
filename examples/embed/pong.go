package main

import (
	"fmt"

	"gocloud.dev/pubsub"

	"github.com/danielorbach/go-component"
)

const PongTarget = "pong"

var PongComponent = &component.Descriptor{
	Name: "pong",
	Doc:  component.MustExtractDoc(Doc, "pong"),
	Bootstrap: func(l *component.L, linker component.Linker, options any) error {
		pub, err := linker.LinkAspect(l.Context(), PongTarget)
		if err != nil {
			return fmt.Errorf("open aspect: %w", err)
		}
		l.CleanupBackground(pub.Shutdown)

		sub, err := linker.LinkInterest(l.Context(), PingTarget)
		if err != nil {
			return fmt.Errorf("open interest: %w", err)
		}
		l.CleanupBackground(sub.Shutdown)

		l.Go("echo", func(l *component.L) {
			for l.Continue() {
				msg, err := sub.Receive(l.GraceContext())
				if err != nil {
					l.Error(fmt.Errorf("receive: %w", err))
					continue
				}
				msg.Ack()

				echo := "ECHO " + string(msg.Body)
				err = pub.Send(l.Context(), &pubsub.Message{Body: []byte(echo)})
				if err != nil {
					l.Error(fmt.Errorf("send: %w", err))
				}
				// we do not log this echo, run ProbeComponent to inspect the messages
			}
		})

		return nil
	},
	OptionsType: nil,
	Aspects:     []string{PongTarget},
	Interests:   []string{PingTarget},
}
