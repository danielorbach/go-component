// Package component provides building blocks for message-driven services whose
// startup, shutdown, and cleanup must happen in a controlled order.
//
// A component runs as a lifecycle, represented by [L]: a procedure with its own
// context that can spawn child lifecycles, register cleanup work, and react to a
// request to stop. [RunProc] starts one and blocks until it, its children, and
// their cleanup have all finished.
//
// # Testing with synctest
//
// The lifecycle's concurrency is exercised under [testing/synctest], which gives
// a test deterministic control over the goroutines a lifecycle starts. The L
// tests in lifecycle_test.go are the worked examples to follow when testing
// lifecycle behaviour.
//
// # Configuring a lifecycle
//
// [RunProc] and [Run] accept [Option] values that configure the lifecycle before
// it starts. The common ones are [WithName] to identify it in logs and traces,
// [WithContext] to root it in a parent context, [WithLogger] to direct its
// output, and [WithStopper] to hand it a channel whose closing asks the procedure
// to stop. That stop request is what [L.Continue] and [L.Stopping] report to the
// running code, so a procedure can wind down on its own terms.
//
// # Running a program
//
// A standalone program rarely calls [RunProc] itself. The
// [github.com/danielorbach/go-component/loader] subpackage provides Entrypoint,
// which installs a context, a logger, and an interrupt handler before running
// the procedure, so that pressing Ctrl-C or receiving a SIGTERM (as Kubernetes
// sends on shutdown) becomes the stop request the lifecycle observes. Reach for
// [RunProc] or [Run] when embedding a lifecycle inside a larger program or a
// test.
package component
