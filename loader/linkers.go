package loader

import (
	"context"
	"net/url"

	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/mempubsub"

	"github.com/danielorbach/go-component"
)

// MuxLinker implements component.Linker over gocloud's pub-sub URLMux to support
// targets with multiple schemes (e.g., kafka://, mem://, etc.).
type MuxLinker struct {
	pubsub.URLMux
	Aspects   map[string]string // map[aspect]topic
	Interests map[string]string // map[interest]topic
}

func (l MuxLinker) LinkAspect(ctx context.Context, aspect string) (*pubsub.Topic, error) {
	topic, ok := l.Aspects[aspect]
	if !ok {
		return nil, component.ErrTargetUnknown
	}
	return l.OpenTopic(ctx, topic)
}

func (l MuxLinker) LinkInterest(ctx context.Context, interest string) (*pubsub.Subscription, error) {
	topic, ok := l.Aspects[interest]
	if !ok {
		return nil, component.ErrTargetUnknown
	}
	return l.OpenSubscription(ctx, topic)
}

// SharedMemLinker implements component.Linker over shared-memory, in order to
// link components in the same address-space.
//
// All topics are opened using the same URLOpener - in other words, use the same
// SharedMemLinker for all components that should share the same topics. The zero
// value is usable.
//
// Establish a link between two components by using the same value as both the
// aspect and the interest - this linker does not support targeting the same data
// exchange (i.e. topic) with different keys, nor does it support targeting
// different topics for different components using the same aspect/interest.
type SharedMemLinker mempubsub.URLOpener

// a target in the same address-space is the aspect/interest name.
func (l *SharedMemLinker) targetURL(target string) *url.URL {
	return &url.URL{Host: target}
}

func (l *SharedMemLinker) LinkAspect(ctx context.Context, aspect string) (*pubsub.Topic, error) {
	return (*mempubsub.URLOpener)(l).OpenTopicURL(ctx, l.targetURL(aspect))
}

func (l *SharedMemLinker) LinkInterest(ctx context.Context, interest string) (*pubsub.Subscription, error) {
	return (*mempubsub.URLOpener)(l).OpenSubscriptionURL(ctx, l.targetURL(interest))
}
