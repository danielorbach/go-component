package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

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
					l.Log("stopping:", context.Cause(l.Context()))
					// the loop will stop because of l.Continue
				case errors.Is(err, context.DeadlineExceeded):
					l.Errorf("timeout while probing")
				case err != nil:
					l.Errorf("receive: %w", err)
				default:
					msg.Ack()
					l.Log(msg.Body)
				}
			}

			for l.Continue() {
				probe()
			}
		})

		return nil
	},
	OptionsType: reflect.TypeOf(ProbeOptions{}),
	Aspects:     nil,
	Interests:   []string{PongTarget},
}

type ProbeOptions struct {
	Timeout time.Duration
}
