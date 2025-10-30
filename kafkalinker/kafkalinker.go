// Package kafkalinker provides a Kafka pubsub implementation of the
// [component.Linker] interface. It is intended to be used by loaders that link
// components in different address-spaces over a Kafka broker.
//
// By default, the linker uses the following Kafka configuration:
//   - bootstrap.servers: <required>
//   - group.id: <required>
//   - auto.offset.reset: earliest
//
// Importing the package registers flags with the loader package - always call
// [loader.ParseFlags] at the beginning of main() to parse the standard
// command-line flags for the loader package.
//
// See the variable declarations below and init() function for the list of flags
// registered by this package.
//
// The linked Kafka topics and subscriptions use [github.com/IBM/sarama] as the
// underlying Kafka driver.
//
// [New] returns a [component.Linker] that links topics and subscriptions using
// the global Config as the Sarama configuration. That global config is settable
// from command-line flags and environment variables. For example:
//
//	$ export KAFKA_CONFIG=ChannelBufferSize=100;ChannelBufferSize=2
//
//	$ go run github.com/example/my-system/cmd/demo -kafka-config 'Metadata.Full=true' -kafka-config 'Consumer.Offset.Initial=sarama.OffsetOldest'
package kafkalinker

import (
	"context"
	"fmt"
	"net/url"

	"github.com/IBM/sarama"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/kafkapubsub"

	"github.com/danielorbach/go-component"
	"github.com/danielorbach/go-component/loader"
)

var (
	// Brokers is the list of Kafka brokers to connect to.
	Brokers []string // -kafka-brokers | $KAFKA_BROKERS

	// Config is the Kafka config from type sarama.Config to be read from cmd flags
	// or env vars.
	Config = MinimalConfig() // -kafka-config | $KAFKA_CONFIG
)

func init() {
	loader.CommandLine.Var((*ConfigFlag)(Config), "kafka-config", "key value pairs of kafka configuration as in sarama.Config type")
	loader.CommandLine.Var((*BrokersFlag)(&Brokers), "kafka-brokers", "comma-separated list of Kafka brokers")
	loader.RequireCommandLine("kafka-brokers")
}

// New creates a component.Linker that links components over Kafka, using the
// global configuration variables registered by this package with the loader
// package.
//
// The provided group name essentially is a consumer-group in Kafka terminology.
// The given aspect/interest maps correlate between named targets and their
// effective topic in the Kafka broker.
func New(group string, aspects, interests map[string]string) (KafkaLinker, error) {
	// Increase the producer's message size to the maximum Kafka supports.
	//
	// This enables us to transport huge messages between components in overclocked
	// system deployments. Currently, we see no downside to increasing this value on
	// all producers unilaterally.
	if err := Config.Validate(); err != nil {
		return KafkaLinker{}, fmt.Errorf("config: %w", err)
	}

	return KafkaLinker{
		URLOpener: &kafkapubsub.URLOpener{
			Brokers: Brokers,
			Config:  Config,
		},
		InstanceId: group,
		Aspects:    aspects,
		Interests:  interests,
	}, nil
}

// KafkaLinker creates a new kafka-based component.Linker, such that all targets
// are linked using the same config provided in the given opener.
//
// The provided instance string uniquely identifies an instance of the component
// intended to use this linker such that all interests opened with the same
// instance shall cooperate consuming the subscription (i.e., it the name of the
// consumer-group).
type KafkaLinker struct {
	*kafkapubsub.URLOpener
	InstanceId string            // correlates to a Kafka consumer-group
	Aspects    map[string]string // map[aspect]topic
	Interests  map[string]string // map[interest]topic
}

func (l KafkaLinker) LinkAspect(ctx context.Context, aspect string) (target *pubsub.Topic, err error) {
	topic, ok := l.Aspects[aspect]
	if !ok {
		return nil, component.ErrTargetUnknown
	}
	return l.OpenAspect(ctx, topic)
}

// OpenAspect opens a topic to the given topic directly, as opposed to looking
// up the topic name in the preconfigured aspects map.
func (l KafkaLinker) OpenAspect(ctx context.Context, topic string) (*pubsub.Topic, error) {
	u := &url.URL{Host: topic}
	return l.OpenTopicURL(ctx, u)
}

func (l KafkaLinker) LinkInterest(ctx context.Context, interest string) (target *pubsub.Subscription, err error) {
	topic, ok := l.Interests[interest]
	if !ok {
		return nil, component.ErrTargetUnknown
	}
	return l.OpenInterest(ctx, topic)
}

// OpenInterest opens a subscription to the given topic directly, as opposed to
// looking up the topic name in the preconfigured interests map.
func (l KafkaLinker) OpenInterest(ctx context.Context, topic string) (*pubsub.Subscription, error) {
	u := &url.URL{
		Host:     l.InstanceId, // consumer-group
		RawQuery: "topic=" + topic,
	}
	return l.OpenSubscriptionURL(ctx, u)
}

// MinimalConfig returns a *sarama.Config instance with basic settings for
// connecting to a Kafka cluster. It makes the following modifications to the
// initial configuration provided by kafkapubsub.MinimalConfig:
//
//   - ClientID: Set to the program name for identification purposes.
//   - Kafka Version: Upgraded to V3.2.1.0 from the default V0.11.0.0.
//   - Consumer Offsets: Configured to start reading from the earliest available
//     messages (OffsetOldest).
//
// The underlying kafkapubsub.MinimalConfig() also applies these settings:
//
// - Headers Support: Enabled by setting Kafka version to V0.11.0.0.
// - Producer Returns: Configured to return successes for SyncProducer.
func MinimalConfig() *sarama.Config {
	c := kafkapubsub.MinimalConfig()
	c.ClientID = loader.ProgramName()
	c.Version = sarama.V3_2_1_0
	// Linkers enable asynchronous message exchanges in our distributed system. As
	// such, it makes sense for a system component to consume messages that were
	// produced before it first subscribes to the target. We make the best effort to
	// miss as little messages as possible by subscribing from the oldest available
	// offset.
	c.Consumer.Offsets.Initial = sarama.OffsetOldest
	return c
}
