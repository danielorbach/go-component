package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/MakeNowJust/heredoc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gocloud.dev/pubsub"

	"github.com/danielorbach/go-component"
)

const PongAspect = "pong"

var pongTracer = otel.Tracer("github.com/danielorbach/go-component/examples/direct/pong")

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
			echo := func() bool {
				ctx, span := pongTracer.Start(l.GraceContext(), "pong.echo", trace.WithSpanKind(trace.SpanKindConsumer))
				defer span.End()

				msg, err := sub.Receive(ctx)
				switch {
				case errors.Is(err, context.Canceled):
					return false
				case err != nil:
					slog.ErrorContext(ctx, "receive ping", "err", err)
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
					return true
				}
				msg.Ack()

				body := "ECHO " + string(msg.Body)
				err = pub.Send(ctx, &pubsub.Message{Body: []byte(body)})
				if err != nil {
					slog.ErrorContext(ctx, "send pong", "err", err)
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
				}
				return true
			}

			for l.Continue() {
				if !echo() {
					return
				}
			}
		})

		return nil
	},
	OptionsType: nil,
	Aspects:     nil,
	Interests:   []string{PingAspect},
}
