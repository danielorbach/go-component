package kafkaloader

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/kafkapubsub"

	"github.com/danielorbach/go-component"
	"github.com/danielorbach/go-component/fileloader"
	"github.com/danielorbach/go-component/kafkalinker"
	"github.com/danielorbach/go-component/loader"
)

var tracer = otel.Tracer("github.com/danielorbach/go-component/kafkaloader")

var (
	// BusBrokers provides an alternative to kafkalinker.Brokers, to allow for a
	// different set of brokers for footprint messages.
	BusBrokers []string // -footprint-brokers | $FOOTPRINT_BROKERS

	// ControlBus names the Kafka topic for broadcast footprints
	ControlBus string // -footprint-control | $FOOTPRINT_CONTROL

	// StatusBus names the Kafka topic for broadcast footprint statuses like
	// acknowledgements and errors
	StatusBus string // -footprint-status | $FOOTPRINT_STATUS
)

func init() {
	loader.CommandLine.Var((*kafkalinker.BrokersFlag)(&BusBrokers), "footprint-brokers", "comma-separated list of Kafka brokers of the footprint bus (implied from kafkalinker if not set)")
	loader.CommandLine.StringVar(&ControlBus, "footprint-control", "footprints.control", "Kafka topic for footprint messages")
	loader.CommandLine.StringVar(&StatusBus, "footprint-status", "footprints.status", "Kafka topic for footprint acknowledgements")
}

func Main(group string) {
	if !loader.CommandLine.Parsed() {
		panic("component/kafkaloader: loader.ParseFlags must be called before Main")
	}

	if len(BusBrokers) == 0 {
		log.Printf("No alternative footprint bus brokers specified, using %v", kafkalinker.Brokers)
	}

	// multiple instances of the same executable cooperatively respect the same
	// footprint bus, so that only one of them will load a given footprint.
	loader.Entrypoint(Loader{ReplicaSet: group}, component.WithSpan("loader"))
}

type Loader struct {
	ReplicaSet string // consumer group for footprint bus
}

func (x Loader) Exec(l *component.L) {
	control, err := OpenControlSubscription(x.ReplicaSet)
	if err != nil {
		l.Fatalf("open footprint-control subscription: %w", err)
	}
	l.CleanupBackground(control.Shutdown)

	for l.Continue() {
		m, err := control.Receive(l.GraceContext())
		if err != nil {
			if errors.Is(err, context.Canceled) && errors.Is(context.Cause(l.GraceContext()), component.ErrStopped) {
				break
			}
			l.Errorf("receive footprint: %w", err)
			continue
		}
		m.Ack()

		var msg *sarama.ConsumerMessage
		if !m.As(&msg) {
			l.Fatalf("unexpected message type: %T", m)
		}
		handleFootprint(l, msg)
	}
}

func handleFootprint(l *component.L, msg *sarama.ConsumerMessage) {
	// l.Context() detaches from the lifecycle span, so this is the root of an
	// independent per-message trace — its size is bounded by the work done for
	// one footprint, not by loader uptime. The span link points back to the
	// lifecycle span for navigation in the trace backend.
	//
	// TODO: link to producer span
	_, span := tracer.Start(l.Context(), "handleFootprint",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithLinks(trace.Link{SpanContext: l.Span().SpanContext()}),
	)
	defer span.End()

	fp, err := fileloader.UnmarshalFootprintJSON(msg.Value)
	if err != nil {
		l.Errorf("unmarshal footprint: %w", err)
		span.SetStatus(codes.Error, err.Error()) // TODO: how does this look in Jaeger?
		return
	}

	span.SetAttributes(
		attribute.String("footprint.name", fp.Name),
		attribute.String("footprint.solution", fp.Solution),
		attribute.StringSlice("footprint.locations", fp.Locations),
		attribute.String("footprint.description", fp.Metadata),
		attribute.Stringer("footprint.identifier", fp.Identifier),
		attribute.Int("footprint.revision", fp.Revision),
	)

	// load the relevant components from the footprint
	loader.Load(fp)
}

func OpenControlTopic() (*pubsub.Topic, error) {
	if !loader.CommandLine.Parsed() {
		panic("component/kafkaloader: loader.ParseFlags must be called before OpenControlTopic")
	}

	config := BusConfig()
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if len(BusBrokers) != 0 {
		return kafkapubsub.OpenTopic(BusBrokers, config, ControlBus, nil)
	} else {
		return kafkapubsub.OpenTopic(kafkalinker.Brokers, config, ControlBus, nil)
	}
}

func OpenControlSubscription(group string) (*pubsub.Subscription, error) {
	if !loader.CommandLine.Parsed() {
		panic("component/kafkaloader: loader.ParseFlags must be called before OpenControlSubscription")
	}

	config := BusConfig()
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if len(BusBrokers) != 0 {
		return kafkapubsub.OpenSubscription(BusBrokers, config, group, []string{ControlBus}, nil)
	} else {
		return kafkapubsub.OpenSubscription(kafkalinker.Brokers, config, group, []string{ControlBus}, nil)
	}
}

// BusConfig returns a Kafka configuration for footprint bus consumers and
// producers. It shares kafkalinker.Config.ClientID and kafkalinker.Config.Version.
func BusConfig() *sarama.Config {
	config := kafkalinker.MinimalConfig()
	config.ClientID = kafkalinker.Config.ClientID
	config.Version = kafkalinker.Config.Version
	config.Consumer.Offsets.Initial = sarama.OffsetNewest // skip older footprints
	return config
}
