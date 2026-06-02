// Package component provides building blocks for message-driven services whose
// startup, shutdown, and cleanup must happen in a controlled order.
//
// A component runs as a lifecycle, represented by [L]: a procedure with its own
// context that can spawn child lifecycles, register cleanup work, and react to a
// request to stop. [RunProc] starts one and blocks until it, its children, and
// their cleanup have all finished.
//
// The lifecycle's concurrency is exercised under [testing/synctest]; the L tests
// in lifecycle_test.go are the worked examples to follow.
package component
