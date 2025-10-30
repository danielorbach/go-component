package kafkaloader

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/google/uuid"
	"gocloud.dev/pubsub"

	"github.com/danielorbach/go-component"
	"github.com/danielorbach/go-component/fileloader"
	"github.com/danielorbach/go-component/kafkalinker"
)

// TODO: define better use-cases for Builder

type Builder struct {
	msg       fileloader.FootprintJSON
	broadcast atomic.Bool // defensive: Broadcast at most once
}

func NewBuilder(id uuid.UUID, revision int) *Builder {
	return &Builder{msg: fileloader.FootprintJSON{
		Identifier: id,
		Revision:   revision,
		Components: make(map[string]fileloader.ComponentJSON),
	}}
}

// guard prevents modifications to the footprint after it has been broadcast.
func (b *Builder) guard() {
	if b.broadcast.Load() {
		panic("component/kafkaloader: footprint must not be modified after it had been broadcast")
	}
}

func (b *Builder) Name(name string) {
	b.guard()
	b.msg.Name = name
}

func (b *Builder) Metadata(metadata string) {
	b.guard()
	b.msg.Metadata = metadata
}

func (b *Builder) Location(location ...string) {
	b.guard()
	b.msg.Locations = append(b.msg.Locations, location...)
}

func (b *Builder) Claim(d *component.Descriptor, options any) error {
	b.guard()

	if reflect.TypeOf(options) != d.OptionsType {
		return fmt.Errorf("unexpected options: %T", options)
	}

	o, err := json.Marshal(options)
	if err != nil {
		return fmt.Errorf("marshal options: %w", err)
	}

	c := fileloader.ComponentJSON{
		Location:  "", // ignored
		Options:   o,
		Aspects:   make(map[string]string, len(d.Aspects)),
		Interests: make(map[string]string, len(d.Interests)),
	}

	for _, target := range d.Aspects {
		c.Aspects[target] = composeTopic(b.msg.Identifier.String(), target)
	}
	for _, target := range d.Interests {
		c.Interests[target] = composeTopic(b.msg.Identifier.String(), target)
	}
	b.msg.Components[d.Name] = c
	return nil
}

func (b *Builder) Broadcast(ctx context.Context, topic *pubsub.Topic) error {
	// at this moment, a single footprint can only be broadcast at most once
	if swapped := b.broadcast.CompareAndSwap(false, true); !swapped {
		panic("component/kafkaloader: footprint had already been broadcast")
	}

	data, err := json.Marshal(b.msg)
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}

	if err := topic.Send(ctx, &pubsub.Message{Body: data}); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	return nil
}

// Observe subscribes the author of this footprint to the target given by the
// provided aspect of the provided descriptor. It panics if that descriptor is
// not part of the footprint, or the aspect is not defined in its footprint.
func (b *Builder) Observe(ctx context.Context, d *component.Descriptor, aspect string) (target *pubsub.Subscription, err error) {
	c, ok := b.msg.Components[d.Name]
	if !ok {
		panic("component/kafkaloader: footprint must contain " + strconv.Quote(d.Name) + " component descriptor")
	}
	topic, ok := c.Aspects[aspect]
	if !ok {
		panic("component/kafkaloader: component descriptor must target " + strconv.Quote(aspect) + " aspect")
	}

	linker, err := kafkalinker.New(b.msg.Identifier.String()+"/"+d.Name+"/monitor", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("kafka linker: %w", err)
	}
	return linker.OpenInterest(ctx, topic)
}

func composeTopic(segments ...string) string {
	// valid Kafka topic regex is "[A-Za-z0-9._]"
	return strings.Join(segments, ".")
}
