package loader_test

import (
	"context"

	"github.com/danielorbach/go-component/loader"
)

// A SharedMemLinker is used to link component in the same address-space.
func ExampleSharedMemLinker() {
	// The zero value of a SharedMemLinker is usable
	var linker loader.SharedMemLinker

	// usually the aspect name is defined with the package that defines the
	// component-of-interest
	topic, err := linker.LinkAspect(context.Background(), "my-aspect")
	if err != nil {
		panic(err)
	}
	// ... send messages on the aspect ...
	// do not forget to shut down the topic when done
	_ = topic.Shutdown(context.Background())

	// the interest name must be the same as the component-of-interest's aspect
	subscription, err := linker.LinkInterest(context.Background(), "my-aspect")
	if err != nil {
		panic(err)
	}
	// ... receive aspect messages from the open interest ...
	// do not forget to shut down the subscription when done
	_ = subscription.Shutdown(context.Background())
}
