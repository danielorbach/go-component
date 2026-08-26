package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gocloud.dev/pubsub"

	"github.com/danielorbach/go-component"
)

const PongTarget = "pong"

var pongTracer = otel.Tracer("github.com/danielorbach/go-component/examples/embed/pong")

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
			echo := func() bool {
				ctx, span := pongTracer.Start(l.GraceContext(), "pong.echo", trace.WithSpanKind(trace.SpanKindConsumer))
				defer span.End()

				msg, err := sub.Receive(ctx)
				switch {
				case errors.Is(err, context.Canceled):
					return false
				case err != nil:
					slog.ErrorContext(ctx, "receive", "err", err)
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
					return true
				}
				msg.Ack()

				body := "ECHO " + string(msg.Body)
				err = pub.Send(ctx, &pubsub.Message{Body: []byte(body)})
				if err != nil {
					slog.ErrorContext(ctx, "send", "err", err)
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
				}
				return true
			}

			for l.Continue() {
				if !echo() {
					return
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
