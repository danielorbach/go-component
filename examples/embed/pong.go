package main

import (
	"fmt"
	"log/slog"

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
					slog.ErrorContext(l.Context(), "receive", "err", err)
					continue
				}
				msg.Ack()

				echo := "ECHO " + string(msg.Body)
				err = pub.Send(l.Context(), &pubsub.Message{Body: []byte(echo)})
				if err != nil {
					slog.ErrorContext(l.Context(), "send", "err", err)
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
