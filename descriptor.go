package component

import (
	"context"
	"errors"
	"flag"
	"reflect"

	"gocloud.dev/pubsub"
)

// A Descriptor describes a function of the system and its options.
type Descriptor struct {
	// The Name of the component must be a valid Go identifier
	// as it may appear in command-line flags, URLs, and so on.
	Name string

	// Doc is brief documentation for the component.
	// The part before the first "\n\n" is the title
	// (no capital or period, max ~60 letters).
	// See ExtractDoc for details and examples.
	Doc string

	// Flag set defines any flags accepted by the component.
	// The manner in which these flags are exposed to the user
	// depends on the driver which runs the component.
	Flags flag.FlagSet

	// Bootstrap should run the component using the provided L.
	// It returns an error if loading the component failed.
	Bootstrap func(l *L, target Linker, options any) error

	// OptionsType is the type of the "options" parameter passed to Bootstrap.
	OptionsType reflect.Type

	// Aspects define the pubsub topics that the component publishes to.
	Aspects []string
	// Interests define the pubsub topics that the component subscribes to.
	Interests []string
}

// ErrTargetUnknown is returned from a component.Linker when it does not know the
// appropriate target for a given aspect or interest.
var ErrTargetUnknown = errors.New("target unknown")

// A Linker establishes a well-defined pathway that enabled two components to
// interact. That is, a linkage is implicitly formed when one component defines
// an aspect and another defines an interest, such that both the aspect and the
// interest reference that same target.
//
// A target designates a point of data exchange; e.g., the name of a topic on a
// message broker.
//
// Implementations of Linker should correlate aspects/interests with their
// respective data exchange targets based on different footprint specifications.
type Linker interface {
	// LinkAspect should return an event sink (a.k.a. target) for the given aspect
	// based on its concomitant pubsub topic - as specified as part of a footprint.
	//
	// The event sink defines a point of egress from a component - that is,
	// publication of notifications.
	//
	// The aspect must be one of those explicitly specified in Descriptor.Aspects,
	// otherwise an error should be returned.
	LinkAspect(ctx context.Context, aspect string) (target *pubsub.Topic, err error)
	// LinkInterest should return an event source (a.k.a. target) for the given
	// interest based on its concomitant pubsub topic - as specified as part of a
	// footprint.
	//
	// The event source defines a point of ingress to a component - that is,
	// subscription to notifications.
	//
	// The interest must be one of those explicitly specified in
	// Descriptor.Interests, otherwise an error should be returned.
	LinkInterest(ctx context.Context, interest string) (target *pubsub.Subscription, err error)
}

// MergeTargets is a shorthand to concatenate targets (i.e., aspects and
// interests) into a single list, usually to serve as Descriptor.Interests.
func MergeTargets(t ...[]string) (targets []string) {
	for i := range t {
		targets = append(targets, t[i]...)
	}
	return targets
}
