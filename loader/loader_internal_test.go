package loader

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/danielorbach/go-component"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Once bootstrap has started managed work, a failure must cancel that work
// before the lifecycle waits, leave the claim unready, and record the failed
// operation against the component that was being loaded.
func TestFailedBootstrapStopsComponent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		recorder := tracetest.NewSpanRecorder()
		provider := sdktrace.NewTracerProvider(
			sdktrace.WithSpanProcessor(component.NewSpanProcessor()),
			sdktrace.WithSpanProcessor(recorder),
		)
		t.Cleanup(func() {
			if err := provider.Shutdown(context.Background()); err != nil {
				t.Errorf("shutdown tracer provider: %v", err)
			}
		})

		previousTracer := tracer
		tracer = provider.Tracer("loader.test")
		t.Cleanup(func() { tracer = previousTracer })

		failure := errors.New("bootstrap failed")
		claim := &Claim{
			Component: &component.Descriptor{
				Name: "worker",
				Bootstrap: func(l *component.L, _ component.Linker, _ any) error {
					l.Go("child", func(l *component.L) {
						<-l.Context().Done()
						if cause := context.Cause(l.Context()); !errors.Is(cause, component.ErrTerminated) {
							t.Errorf("child cause = %v, want %v", cause, component.ErrTerminated)
						}
					})
					return failure
				},
			},
		}

		component.Run(claim, component.WithName("loader/worker"))

		select {
		case <-claim.Ready():
			t.Error("failed claim reported ready")
		default:
		}

		var bootstrap sdktrace.ReadOnlySpan
		for _, span := range recorder.Ended() {
			if span.Name() == "component.bootstrap" {
				bootstrap = span
				break
			}
		}
		if bootstrap == nil {
			t.Fatal("bootstrap span not recorded")
		}
		if bootstrap.Status().Code != codes.Error {
			t.Errorf("bootstrap status = %v, want Error", bootstrap.Status().Code)
		}
		attrs := attribute.NewSet(bootstrap.Attributes()...)
		name, ok := attrs.Value(component.TraceKey)
		if !ok {
			t.Error("component.name attribute not recorded")
		} else if got := name.AsString(); got != "worker" {
			t.Errorf("component.name = %q, want worker", got)
		}
	})
}
