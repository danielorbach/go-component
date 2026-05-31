package component_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/danielorbach/go-component"
)

// SyncTimeout is the maximum time a test is allowed to wait for a
// synchronisation event to occur.
const SyncTimeout = time.Second

func TestDrivenDevelopment(t *testing.T) {
	t.Run("FunctionExecution", func(t *testing.T) {
		tests := []struct {
			name      string
			fn        func(*component.L)
			wantAbort bool
		}{
			{
				name: "NoOp",
				fn:   func(*component.L) {},
			},
			{
				name: "Error",
				fn: func(l *component.L) {
					l.Error(fmt.Errorf("test error"))
				},
				wantAbort: false,
			},
			{
				name: "Fatal",
				fn: func(l *component.L) {
					l.Fatal(fmt.Errorf("test error"))
				},
				wantAbort: true,
			},
			{
				name: "FatalWhileCleanup",
				fn: func(l *component.L) {
					l.Cleanup(func() {
						l.Fatal(fmt.Errorf("test error"))
					})
				},
				wantAbort: false, // cleanup is called after fn returns
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					var called bool
					component.RunProc(func(l *component.L) {
						tt.fn(l)
						called = true
					}, component.WithName(t.Name()))
					if tt.wantAbort && called {
						t.Error("execution should have been aborted")
					}
					if !tt.wantAbort && !called {
						t.Error("execution should not have been aborted")
					}
				})
			})
		}
	})
}

func TestL_Run(t *testing.T) {
	t.Run("Concurrent", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			const goroutines = 16
			component.RunProc(func(l *component.L) {
				done := make(chan struct{})
				for i := range goroutines {
					l.Go("child#"+strconv.Itoa(i), func(l *component.L) {
						done <- struct{}{}
					})
				}
				for range goroutines {
					<-done
				}
			}, component.WithName(t.Name()))
		})
	})

	t.Run("CalledAfterCompletion", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			// capture l variable to simulate a closure over it
			// (may happen in real code when storing a reference to
			// the lifecycle)
			var capture *component.L
			component.RunProc(func(l *component.L) { capture = l }, component.WithName(t.Name()))
			// calling L.Go after the lifecycle has completed should panic
			defer func() {
				if reason := recover(); reason == nil {
					t.Error("L.Go() must panic if called after component completion")
				}
			}()
			capture.Go("<irrelevant>", func(*component.L) {})
		})
	})
}

func TestL_Cleanup(t *testing.T) {
	t.Run("Order", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			const count = 100
			order := make(map[int]int) // map: cleanupFn#index -> called order
			component.RunProc(func(l *component.L) {
				for i := range count {
					index := i
					l.Cleanup(func() {
						order[index] = len(order)
					})
				}
			}, component.WithName(t.Name()))
			if len(order) != count {
				t.Fatalf("cleanup functions were not called: got %d, want %d", len(order), count)
			}
			for index, called := range order {
				if called != count-index-1 {
					t.Errorf("cleanup#%d was called out of order: got %d, want %d", index, called, count-index-1)
				}
			}
		})
	})

	t.Run("CalledAfterCompletion", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			// capture l variable to simulate a closure over it
			// (may happen in real code when storing a reference to
			// the lifecycle)
			var capture *component.L
			component.RunProc(func(l *component.L) { capture = l }, component.WithName(t.Name()))
			// calling L.Cleanup after the lifecycle has completed should panic
			defer func() {
				if reason := recover(); reason == nil {
					t.Error("L.Cleanup() must panic if called after component completion")
				}
			}()
			capture.Cleanup(func() {})
		})
	})

	t.Run("SynchronisesBeforeSubComponents", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var cleanedUp atomic.Bool
			release := make(chan struct{})
			done := make(chan struct{})
			go func() {
				defer close(done)
				component.RunProc(func(l *component.L) {
					l.Cleanup(func() { cleanedUp.Store(true) })
					l.Go("subcomponent", func(*component.L) {
						// stay blocked so the lifecycle - and therefore its
						// cleanup - cannot complete until we release it below
						<-release
					})
				}, component.WithName(t.Name()))
			}()

			// synctest.Wait blocks until every other goroutine in the bubble is
			// durably blocked: the subcomponent parked on <-release, the
			// lifecycle waiting on it in wg.Wait, and the reapers idle. At that
			// quiescent point a cleanup that had run early would already be
			// observable, so this asserts the negative directly rather than
			// inferring it from a deadline that never fires.
			synctest.Wait()
			if cleanedUp.Load() {
				t.Error("cleanup ran before the subcomponent completed")
			}

			close(release) // let the subcomponent finish so the lifecycle completes
			<-done
			if !cleanedUp.Load() {
				t.Error("cleanup did not run after the subcomponent completed")
			}
		})
	})
}

func TestL_Fatal(t *testing.T) {
	t.Run("CalledFromNestedFunctions", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			component.RunProc(func(l *component.L) {
				func() {
					func() {
						func() {
							l.Fatal(fmt.Errorf("test error"))
							t.Fatal("L.Fatal() should have aborted execution (nesting level 0)")
						}()
						t.Error("L.Fatal() should have aborted execution (nesting level -1)")
					}()
					t.Error("L.Fatal() should have aborted execution (nesting level -2)")
				}()
				t.Error("L.Fatal() should have aborted execution (nesting level -3)")
			}, component.WithName(t.Name()))
		})
	})

	t.Run("CalledFromMultipleGoroutines", func(t *testing.T) {
		for _, goroutines := range []int{1, 2, 16} {
			t.Run(fmt.Sprintf("ConcurrentCalls=%d", goroutines), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					component.RunProc(func(l *component.L) {
						done := make(chan struct{})
						for i := range goroutines {
							go func(index int) {
								defer func() { done <- struct{}{} }()
								l.Fatal(fmt.Errorf("test error from goroutine #%d", index))
								t.Errorf("goroutine #%d: L.Fatal() should have aborted execution", index)
							}(i)
						}
						// pay attention to the fact that we're still in the primary goroutine
						// of the lifecycle - calls to L.Fatal from other goroutines do not
						// (and cannot) affect the primary goroutine.
						for range goroutines {
							<-done
						}
					}, component.WithName(t.Name()))
				})
			})
		}
	})

	t.Run("CallsCleanupFunctions", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var called bool
			component.RunProc(func(l *component.L) {
				l.Cleanup(func() { called = true })
				l.Fatal(fmt.Errorf("test error"))
				t.Error("L.Fatal() should have aborted execution")
			}, component.WithName(t.Name()))
			if !called {
				t.Error("Cleanup() function should have been called")
			}
		})
	})

	t.Run("CalledFromCleanupFunction", func(t *testing.T) {
		// although only the first call to the Cleanup() has any potential
		// to be aborted by a call to Fatal() executed inside a subsequent
		// Cleanup() function, we test the other side of the coin as well
		// to ensure this test does not pass by accident due to human error.
		var calledBeforeFatal, calledAfterFatal bool
		synctest.Test(t, func(t *testing.T) {
			component.RunProc(func(l *component.L) {
				l.Cleanup(func() {
					// scheduled before Fatal(), but executed after because
					// cleanup functions are executed in reverse order
					calledAfterFatal = true
				})
				l.Cleanup(func() {
					l.Fatal(fmt.Errorf("test error"))
					t.Error("L.Fatal() should have aborted execution of this Cleanup() function")
				})
				l.Cleanup(func() {
					// scheduled after Fatal(), but executed before because
					// cleanup functions are executed in reverse order
					calledBeforeFatal = true
				})
			}, component.WithName(t.Name()))
		})
		if !calledBeforeFatal {
			t.Error("Cleanup() function should have been called before L.Fatal()")
		}
		if !calledAfterFatal {
			t.Error("Cleanup() function should have been called after L.Fatal()")
		}
	})

	t.Run("CancelsContext", func(t *testing.T) {
		var sentinel = fmt.Errorf("test error value")
		tests := []struct {
			name string
			fn   func(*component.L)
		}{
			{
				name: "Directly",
				fn: func(l *component.L) {
					l.Fatal(sentinel)
				},
			},
			{
				name: "Goroutine",
				fn: func(l *component.L) {
					done := make(chan struct{})
					go func() {
						defer close(done)
						l.Fatal(sentinel)
					}()
					<-done
				},
			},
			{
				name: "Cleanup",
				fn: func(l *component.L) {
					l.Cleanup(func() {
						l.Fatal(sentinel)
					})
				},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					component.RunProc(func(l *component.L) {
						l.Cleanup(func() {
							if !errors.Is(l.Context().Err(), context.Canceled) {
								t.Error("context should have been canceled")
							}
							if cause := context.Cause(l.Context()); !errors.Is(cause, sentinel) {
								t.Logf("context error: %q", cause)
								t.Error("context should have been canceled with the sentinel error")
							}
						})
						tt.fn(l)
					}, component.WithName(t.Name()))
				})
			})
		}
	})

	t.Run("CalledAfterCompletion", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			// capture l variable to simulate a closure over it
			// (may happen in real code when storing a reference to
			// the lifecycle)
			var capture *component.L
			component.RunProc(func(l *component.L) { capture = l }, component.WithName(t.Name()))
			// calling L.Fatal after the lifecycle has completed should panic
			defer func() {
				if reason := recover(); reason == nil {
					t.Error("L.Fatal() must panic if called after component completion")
				}
			}()
			capture.Fatal(fmt.Errorf("test error"))
		})
	})
}

func TestL_Context(t *testing.T) {
	t.Run("Background", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			component.RunProc(func(l *component.L) {
				_, ok := l.Context().Deadline()
				if ok {
					t.Error("context should not have a deadline")
				}
			}, component.WithName(t.Name()))
		})
	})
	t.Run("Canceled", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			component.RunProc(func(l *component.L) {
				if !errors.Is(l.Context().Err(), context.Canceled) {
					t.Error("context should have been canceled")
				}
			}, component.WithName(t.Name()), component.WithContext(ctx))
		})
	})
	t.Run("DeadlineExceeded", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			const timeout = time.Millisecond
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			component.RunProc(func(l *component.L) {
				// wait for context to expire
				<-l.Context().Done()
				if !errors.Is(l.Context().Err(), context.DeadlineExceeded) {
					t.Error("context deadline should have been exceeded")
				}
			}, component.WithName(t.Name()), component.WithContext(ctx))
		})
	})
}

func TestL_Name(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			component.RunProc(func(l *component.L) {
				if l.Name() != "" {
					t.Errorf("parent Name() = %q, want %q", l.Name(), "")
				}
				// see child initialization for why this is "/"
				l.Go("", func(l *component.L) {
					if l.Name() != "/" {
						t.Errorf("child Name() = %q, want %q", l.Name(), "./")
					}
				})
			}, component.WithName(""))
		})
	})

	t.Run("Sanity", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			component.RunProc(func(l *component.L) {
				if l.Name() != "TestLifecycle" {
					t.Errorf("Name() = %q, want %q", l.Name(), "TestLifecycle")
				}
				l.Go("Child", func(l *component.L) {
					if l.Name() != "TestLifecycle/Child" {
						t.Errorf("Name() = %q, want %q", l.Name(), "TestLifecycle/Child")
					}
				})
			}, component.WithName("TestLifecycle"))
		})
	})

	t.Run("Spaces", func(t *testing.T) {
		const name = "with spaces"
		synctest.Test(t, func(t *testing.T) {
			component.RunProc(func(l *component.L) {
				if l.Name() != name {
					t.Errorf("L.Name() = %q, want %q", l.Name(), name)
				}
				l.Go(name, func(l *component.L) {
					const subname = "with spaces/with spaces"
					if l.Name() != subname {
						t.Errorf("L.Name() = %q, want %q", l.Name(), subname)
					}
				})
			}, component.WithName(name))
		})
	})

	t.Run("Duplicate", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			component.RunProc(func(l *component.L) {
				l.Go("dup", func(l *component.L) {
					if l.Name() != "/dup" {
						t.Errorf("L.Name() = %q, want %q", l.Name(), "./dup")
					}
				})
				l.Go("dup", func(l *component.L) {
					if l.Name() != "/dup" {
						t.Errorf("L.Name() = %q, want %q", l.Name(), "./dup")
					}
				})
			}, component.WithName(""))
		})
	})
}

func TestL_Terminate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		component.RunProc(func(l *component.L) {
			l.Terminate()
			if !errors.Is(l.Context().Err(), context.Canceled) {
				t.Error("context should have been canceled")
			}
			if cause := context.Cause(l.Context()); !errors.Is(cause, component.ErrTerminated) {
				t.Errorf("Cause() = %v, want %v", cause, component.ErrTerminated)
			}
		}, component.WithName(t.Name()))
	})
}

func TestL_Stop(t *testing.T) {
	// the following test verifies Stop() honors its timeout parameter
	// (crucial for the next test to work)
	t.Run("Ignored", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			// in order to "ignore" the Stop() call,
			// we must block RunProc() until Stop() returns
			// although Stop() blocks, we block RunProc() until it returns
			component.RunProc(func(l *component.L) {
				stopped := make(chan bool, 1) // buffered to avoid deadlock with the Stop() goroutine
				go func() {
					stopped <- l.Stop(180 * time.Millisecond)
				}()
				// block the lifecycle until Stop() returns
				if ok := <-stopped; ok {
					t.Error("Stop() should have failed")
				}
			}, component.WithName(t.Name()))
		})
	})

	t.Run("Respected", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			stopped := make(chan bool, 1)
			var signalled atomic.Bool // only used for logging
			component.RunProc(func(l *component.L) {
				go func() {
					// Stop() blocks, but we know it honors the given timeout
					// because of the previous test ("Ignored")
					stopped <- l.Stop(SyncTimeout)
				}()
				// returning after waiting for Stopping() to close
				// completes the lifecycle
				<-l.Stopping()
				signalled.Store(true)
			}, component.WithName(t.Name()))
			if !<-stopped {
				t.Logf("stop signal received = %v", signalled.Load())
				t.Error("Stop() should have succeeded")
			}
		})
	})

	t.Run("Concurrent", func(t *testing.T) {
		// start multiple Stop() calls concurrently with different timeouts
		timeouts := []time.Duration{18 * time.Millisecond, 36 * time.Millisecond, 180 * time.Millisecond}
		// we do the actual testing within a call to RunProc()
		// because this guarantees that the lifecycle is not
		// respecting its Stopped() signal.
		synctest.Test(t, func(t *testing.T) {
			component.RunProc(func(l *component.L) {
				stopped := make(chan bool, len(timeouts))
				for i := range timeouts {
					go func(timeout time.Duration) {
						stopped <- l.Stop(timeout)
					}(timeouts[i])
				}
				// wait for all Stop() calls to return
				for i := range timeouts {
					if ok := <-stopped; ok {
						t.Errorf("Stop() should have failed (already stopped: %d/%d)", i, len(timeouts))
					}
				}
			}, component.WithName(t.Name()))
		})
	})

	t.Run("ChildLifecycle", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			stopped := make(chan bool)
			ready := make(chan struct{})
			component.RunProc(func(l *component.L) {
				go func() {
					// stop parent-lifecycle immediately after the two
					// child-lifecycles have been scheduled (by Go())
					// because starting them after initiating the stop
					// causes a panic.
					<-ready
					<-ready
					stopped <- l.Stop(time.Second)
				}()

				l.Go("child1", func(l *component.L) {
					ready <- struct{}{}
					<-l.Stopping() // wait for child-lifecycle to signal a stop
				})
				l.Go("child2", func(l *component.L) {
					ready <- struct{}{}
					<-l.Stopping() // wait for child-lifecycle to signal a stop
				})
				<-l.Stopping() // wait for parent-lifecycle to signal a stop
			}, component.WithName(t.Name()))
			if !<-stopped {
				t.Error("Stop() should have succeeded")
			}
		})
	})
}

func TestL_Continue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stopper := make(chan struct{})
		component.RunProc(func(l *component.L) {
			if !l.Continue() {
				t.Error("Continue() should have returned true")
			}

			close(stopper) // stop the lifecycle
			// synchronise with the lifecycle signal propagation (otherwise Continue() might
			// return before the lifecycle has had a chance to propagate the stop signal)
			<-l.Stopping()

			if l.Continue() {
				t.Error("Continue() should have returned false")
			}
		}, component.WithName(t.Name()), component.WithStopper(stopper))
	})
}
