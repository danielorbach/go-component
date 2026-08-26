package main

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gocloud.dev/pubsub"

	"github.com/danielorbach/go-component"
)

const PingTarget = "ping"

var pingTracer = otel.Tracer("github.com/danielorbach/go-component/examples/embed/ping")

// PingInterval flag sets the interval between pings. The purpose of flags, as
// opposed to options, is to allow the user to configure the component at
// deployment time - usually with deployment-specific values. For this example,
// there is no need to configure the interval at deployment time, but it is still
// useful to demonstrate how to use flags.
var (
	PingInterval time.Duration
)

// The zero value of the type flag.FlagSet is a valid flag set, so we can use it
// directly. The loader package will automatically add any flags set forth to the
// command-line flags and parse them. Components can then access the values of
// these flags just like any other global variable.
func init() {
	PingComponent.Flags.DurationVar(&PingInterval, "interval", 2*time.Second, "interval between pings")
}

var PingComponent = &component.Descriptor{
	Name: "ping",
	Doc:  component.MustExtractDoc(Doc, "ping"),
	Bootstrap: func(l *component.L, linker component.Linker, options any) error {
		pub, err := linker.LinkAspect(l.Context(), PingTarget)
		if err != nil {
			return fmt.Errorf("open aspect: %w", err)
		}
		l.CleanupBackground(pub.Shutdown)

		l.Go("timer", func(l *component.L) {
			options := options.(PingOptions)
			t := time.NewTicker(PingInterval)
			defer t.Stop()
			slog.InfoContext(l.Context(), "pinging", "interval", PingInterval)

			for l.Continue() {
				select {
				case <-t.C:
					ctx, span := pingTracer.Start(l.Context(), "ping.send", trace.WithSpanKind(trace.SpanKindProducer))
					err := pub.Send(ctx, &pubsub.Message{Body: []byte(options.Data)})
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
					return
				}
			}
		})

		return nil
	},
	OptionsType: reflect.TypeFor[PingOptions](),
	Aspects:     []string{PingTarget},
	Interests:   nil,
}

type PingOptions struct {
	Data string // data to send on ping
}
