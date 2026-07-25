package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/danielorbach/go-component"
)

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
			probe := func() {
				ctx, cancel := context.WithTimeout(l.GraceContext(), options.(ProbeOptions).Timeout)
				defer cancel()

				msg, err := sub.Receive(ctx)
				switch {
				case errors.Is(err, context.Canceled):
					slog.InfoContext(l.Context(), "stopping", "cause", context.Cause(l.Context()))
					// the loop will stop because of l.Continue
				case errors.Is(err, context.DeadlineExceeded):
					slog.ErrorContext(l.Context(), "timeout while probing")
					trace.SpanFromContext(l.Context()).RecordError(err)
				case err != nil:
					slog.ErrorContext(l.Context(), "receive", "err", err)
					trace.SpanFromContext(l.Context()).RecordError(err)
				default:
					msg.Ack()
					slog.InfoContext(l.Context(), "received", "body", string(msg.Body))
				}
			}

			for l.Continue() {
				probe()
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
