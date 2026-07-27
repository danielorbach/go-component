package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/danielorbach/go-component"
)

var probeTracer = otel.Tracer("github.com/danielorbach/go-component/examples/embed/probe")

var ProbeComponent = &component.Descriptor{
	Name: "probe",
	Doc:  component.MustExtractDoc(Doc, "probe"),
	Bootstrap: func(l *component.L, linker component.Linker, options any) error {
		if options.(ProbeOptions).Timeout == 0 {
			return fmt.Errorf("required option: Timeout")
		}

		sub, err := linker.LinkInterest(l.Context(), PongTarget)
		if err != nil {
			return fmt.Errorf("open interest: %w", err)
		}
		l.CleanupBackground(sub.Shutdown)

		l.Go("observer", func(l *component.L) {
			probe := func() bool {
				ctx, cancel := context.WithTimeout(l.GraceContext(), options.(ProbeOptions).Timeout)
				defer cancel()
				ctx, span := probeTracer.Start(ctx, "probe.receive", trace.WithSpanKind(trace.SpanKindConsumer))
				defer span.End()

				msg, err := sub.Receive(ctx)
				switch {
				case errors.Is(err, context.Canceled):
					slog.InfoContext(ctx, "stopping", "cause", context.Cause(l.Context()))
					return false
				case errors.Is(err, context.DeadlineExceeded):
					slog.ErrorContext(ctx, "timeout while probing")
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
				case err != nil:
					slog.ErrorContext(ctx, "receive", "err", err)
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
				default:
					msg.Ack()
					slog.InfoContext(ctx, "received", "body", string(msg.Body))
				}
				return true
			}

			for l.Continue() {
				if !probe() {
					return
				}
			}
		})

		return nil
	},
	OptionsType: reflect.TypeFor[ProbeOptions](),
	Aspects:     nil,
	Interests:   []string{PongTarget},
}

type ProbeOptions struct {
	Timeout time.Duration
}
