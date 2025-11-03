package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/MakeNowJust/heredoc"
	"gocloud.dev/pubsub"

	"github.com/danielorbach/go-component"
)

const PongAspect = "pong"

var PongComponent = &component.Descriptor{
	Name: "pong",
	Doc: heredoc.Doc(`
	continually subscribes to simple message from the PingAspect interest
	
	The pong component receives simple messages from the pong component and echoes
	them onto its aspect (PongAspect).
	`),
	Bootstrap: func(l *component.L, linker component.Linker, options any) error {
		pub, err := linker.LinkAspect(l.Context(), PongAspect)
		if err != nil {
			return fmt.Errorf("open aspect: %w", err)
		}
		l.CleanupBackground(pub.Shutdown)

		sub, err := linker.LinkInterest(l.Context(), PingAspect)
		if err != nil {
			return fmt.Errorf("open interest: %w", err)
		}
		l.CleanupBackground(sub.Shutdown)

		l.Go("sub", func(l *component.L) {
			for l.Continue() {
				msg, err := sub.Receive(l.GraceContext())
				switch {
				case errors.Is(err, context.Canceled):
					return
				case err != nil:
					l.Error(fmt.Errorf("receive ping: %w", err))
					continue
				}
				msg.Ack()

				echo := "ECHO " + string(msg.Body)
				err = pub.Send(l.Context(), &pubsub.Message{Body: []byte(echo)})
				if err != nil {
					l.Error(fmt.Errorf("send pong: %w", err))
				}
			}
		})

		return nil
	},
	OptionsType: nil,
	Aspects:     nil,
	Interests:   []string{PingAspect},
}
