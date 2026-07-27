package main

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"github.com/MakeNowJust/heredoc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gocloud.dev/pubsub"

	"github.com/danielorbach/go-component"
)

const PingAspect = "ping"

var pingTracer = otel.Tracer("github.com/danielorbach/go-component/examples/direct/ping")

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
			slog.InfoContext(l.Context(), "pinging", "interval", PingInterval)

			for i := 0; l.Continue(); i++ {
				select {
				case <-t.C:
					text := fmt.Sprintf("%s (seq=%d)", options.Data, i)
					ctx, span := pingTracer.Start(l.Context(), "ping.send", trace.WithSpanKind(trace.SpanKindProducer))
					err := pub.Send(ctx, &pubsub.Message{Body: []byte(text)})
					if err != nil {
						slog.ErrorContext(ctx, "send ping", "err", err)
						span.RecordError(err)
						span.SetStatus(codes.Error, err.Error())
					}
					span.End()
				case <-l.Stopping():
					slog.InfoContext(l.Context(), "graceful stop")
				case <-l.Context().Done():
					slog.InfoContext(l.Context(), "abrupt stop", "cause", context.Cause(l.Context()))
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
