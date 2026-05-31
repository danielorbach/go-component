package component

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// SyncTimeout is the maximum time a test is allowed to wait for a
// synchronisation event to occur.
const SyncTimeout = time.Second

func TestDrivenDevelopment(t *testing.T) {
	t.Run("FunctionExecution", func(t *testing.T) {
		tests := []struct {
			name      string
			fn        func(*L)
			wantAbort bool
		}{
			{
				name: "NoOp",
				fn:   func(*L) {},
			},
			{
				name: "Error",
				fn: func(l *L) {
					l.Error(fmt.Errorf("test error"))
				},
				wantAbort: false,
			},
			{
				name: "Fatal",
				fn: func(l *L) {
					l.Fatal(fmt.Errorf("test error"))
				},
				wantAbort: true,
			},
			{
				name: "FatalWhileCleanup",
				fn: func(l *L) {
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
					RunProc(func(l *L) {
						tt.fn(l)
						called = true
					}, WithName(t.Name()))
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
			RunProc(func(l *L) {
				done := make(chan struct{})
				for i := range goroutines {
					l.Go("child#"+strconv.Itoa(i), func(l *L) {
						done <- struct{}{}
					})
				}
				for range goroutines {
					<-done
				}
			}, WithName(t.Name()))
		})
	})

	t.Run("CalledAfterCompletion", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			// capture l variable to simulate a closure over it
			// (may happen in real code when storing a reference to
			// the lifecycle)
			var capture *L
			RunProc(func(l *L) { capture = l }, WithName(t.Name()))
			// calling L.Go after the lifecycle has completed should panic
			defer func() {
				if reason := recover(); reason == nil {
					t.Error("L.Go() must panic if called after component completion")
				}
			}()
			capture.Go("<irrelevant>", func(*L) {})
		})
	})
}

func TestL_Cleanup(t *testing.T) {
	t.Run("Order", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			const count = 100
			order := make(map[int]int) // map: cleanupFn#index -> called order
			RunProc(func(l *L) {
				for i := range count {
					index := i
					l.Cleanup(func() {
						order[index] = len(order)
					})
				}
			}, WithName(t.Name()))
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
			var capture *L
			RunProc(func(l *L) { capture = l }, WithName(t.Name()))
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
			ctx, cancel := context.WithTimeout(context.Background(), 180*time.Millisecond)
			defer cancel()
			RunProc(func(l *L) {
				canary := make(chan struct{})
				l.Cleanup(func() {
					// send a signal to the canary channel to indicate that the cleanup function has
					// been called. this case blocks indefinitely if the subcomponent has already
					// returned
					select {
					case <-ctx.Done():
					case canary <- struct{}{}:
						t.Error("Cleanup(): cleanup function was called before the subcomponent completed")
					}
				})
				l.Go("canary", func(*L) {
					// yield to increase the change of the cleanup being called in case the test will
					// have failed
					runtime.Gosched()
					// wait for the cleanup to be called - we only know it has not been called if the
					// context times out
					select {
					case <-ctx.Done():
					case <-canary:
						t.Error("Go(): cleanup function was called before the subcomponent completed")
					}
				})
			}, WithName(t.Name()), WithContext(ctx))
		})
	})
}

func TestL_Fatal(t *testing.T) {
	t.Run("CalledFromNestedFunctions", func(t *testing.T) {
		RunProc(func(l *L) {
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
		}, WithName(t.Name()))
	})

	t.Run("CalledFromMultipleGoroutines", func(t *testing.T) {
		for _, goroutines := range []int{1, 2, 16} {
			t.Run(fmt.Sprintf("ConcurrentCalls=%d", goroutines), func(t *testing.T) {
				RunProc(func(l *L) {
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
						select {
						case <-done:
						case <-time.After(SyncTimeout):
							t.Fatal("timeout: L.Fatal() did not call deferred functions in goroutines")
						}
					}
				}, WithName(t.Name()))
			})
		}
	})

	t.Run("CallsCleanupFunctions", func(t *testing.T) {
		var called bool
		RunProc(func(l *L) {
			l.Cleanup(func() { called = true })
			l.Fatal(fmt.Errorf("test error"))
			t.Error("L.Fatal() should have aborted execution")
		}, WithName(t.Name()))
		if !called {
			t.Error("Cleanup() function should have been called")
		}
	})

	t.Run("CalledFromCleanupFunction", func(t *testing.T) {
		// although only the first call to the Cleanup() has any potential
		// to be aborted by a call to Fatal() executed inside a subsequent
		// Cleanup() function, we test the other side of the coin as well
		// to ensure this test does not pass by accident due to human error.
		var calledBeforeFatal, calledAfterFatal bool
		RunProc(func(l *L) {
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
		}, WithName(t.Name()))
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
			fn   func(*L)
		}{
			{
				name: "Directly",
				fn: func(l *L) {
					l.Fatal(sentinel)
				},
			},
			{
				name: "Goroutine",
				fn: func(l *L) {
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
				fn: func(l *L) {
					l.Cleanup(func() {
						l.Fatal(sentinel)
					})
				},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				RunProc(func(l *L) {
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
				}, WithName(t.Name()))
			})
		}
	})

	t.Run("CalledAfterCompletion", func(t *testing.T) {
		// capture l variable to simulate a closure over it
		// (may happen in real code when storing a reference to
		// the lifecycle)
		var capture *L
		RunProc(func(l *L) { capture = l }, WithName(t.Name()))
		// calling L.Fatal after the lifecycle has completed should panic
		defer func() {
			if reason := recover(); reason == nil {
				t.Error("L.Fatal() must panic if called after component completion")
			}
		}()
		capture.Fatal(fmt.Errorf("test error"))
	})
}

func TestL_Context(t *testing.T) {
	t.Run("Background", func(t *testing.T) {
		RunProc(func(l *L) {
			_, ok := l.Context().Deadline()
			if ok {
				t.Error("context should not have a deadline")
			}
		}, WithName(t.Name()))
	})
	t.Run("Canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		RunProc(func(l *L) {
			if !errors.Is(l.Context().Err(), context.Canceled) {
				t.Error("context should have been canceled")
			}
		}, WithName(t.Name()), WithContext(ctx))
	})
	t.Run("DeadlineExceeded", func(t *testing.T) {
		t.Parallel()
		const timeout = time.Millisecond
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		RunProc(func(l *L) {
			// wait for context to expire
			select {
			case <-l.Context().Done():
				if !errors.Is(l.Context().Err(), context.DeadlineExceeded) {
					t.Error("context deadline should have been exceeded")
				}
			case <-time.After(SyncTimeout):
				t.Error("timeout: context should have been canceled by now")
			}
		}, WithName(t.Name()), WithContext(ctx))
	})
}

func TestL_Name(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		RunProc(func(l *L) {
			if l.Name() != "" {
				t.Errorf("parent Name() = %q, want %q", l.Name(), "")
			}
			// see child initialization for why this is "/"
			l.Go("", func(l *L) {
				if l.Name() != "/" {
					t.Errorf("child Name() = %q, want %q", l.Name(), "./")
				}
			})
		}, WithName(""))
	})

	t.Run("Sanity", func(t *testing.T) {
		RunProc(func(l *L) {
			if l.Name() != "TestLifecycle" {
				t.Errorf("Name() = %q, want %q", l.Name(), "TestLifecycle")
			}
			l.Go("Child", func(l *L) {
				if l.Name() != "TestLifecycle/Child" {
					t.Errorf("Name() = %q, want %q", l.Name(), "TestLifecycle/Child")
				}
			})
		}, WithName("TestLifecycle"))
	})

	t.Run("Spaces", func(t *testing.T) {
		const name = "with spaces"
		RunProc(func(l *L) {
			if l.Name() != name {
				t.Errorf("L.Name() = %q, want %q", l.Name(), name)
			}
			l.Go(name, func(l *L) {
				const subname = "with spaces/with spaces"
				if l.Name() != subname {
					t.Errorf("L.Name() = %q, want %q", l.Name(), subname)
				}
			})
		}, WithName(name))
	})

	t.Run("Duplicate", func(t *testing.T) {
		RunProc(func(l *L) {
			l.Go("dup", func(l *L) {
				if l.Name() != "/dup" {
					t.Errorf("L.Name() = %q, want %q", l.Name(), "./dup")
				}
			})
			l.Go("dup", func(l *L) {
				if l.Name() != "/dup" {
					t.Errorf("L.Name() = %q, want %q", l.Name(), "./dup")
				}
			})
		}, WithName(""))
	})
}

func TestL_Terminate(t *testing.T) {
	RunProc(func(l *L) {
		l.Terminate()
		if !errors.Is(l.Context().Err(), context.Canceled) {
			t.Error("context should have been canceled")
		}
		if cause := context.Cause(l.Context()); !errors.Is(cause, ErrTerminated) {
			t.Errorf("Cause() = %v, want %v", cause, ErrTerminated)
		}
	}, WithName(t.Name()))
}

func TestL_Stop(t *testing.T) {
	t.Parallel() // parallel because of a subtest which must time out to pass (also parallel)

	// the following test verifies Stop() honors its timeout parameter
	// (crucial for the next test to work)
	t.Run("Ignored", func(t *testing.T) {
		t.Parallel()
		// in order to "ignore" the Stop() call,
		// we must block RunProc() until Stop() returns
		// although Stop() blocks, we block RunProc() until it returns
		RunProc(func(l *L) {
			stopped := make(chan bool, 1) // buffered to avoid deadlock with the Stop() goroutine
			go func() {
				stopped <- l.Stop(180 * time.Millisecond)
			}()
			// block the lifecycle until Stop() returns
			select {
			case ok := <-stopped:
				if ok {
					t.Error("Stop() should have failed")
				}
			case <-time.After(SyncTimeout):
				t.Error("timeout: Stop() should have returned by now")
			}
		}, WithName(t.Name()))
	})

	t.Run("Respected", func(t *testing.T) {
		t.Parallel()
		stopped := make(chan bool, 1)
		var signalled atomic.Bool // only used for logging
		RunProc(func(l *L) {
			go func() {
				// Stop() blocks, but we know it honors the given timeout
				// because of the previous test ("Ignored")
				stopped <- l.Stop(SyncTimeout)
			}()
			// returning after waiting for Stopping() to close
			// completes the lifecycle
			<-l.Stopping()
			signalled.Store(true)
		}, WithName(t.Name()))
		if !<-stopped {
			t.Logf("stop signal received = %v", signalled.Load())
			t.Error("Stop() should have succeeded")
		}
	})

	t.Run("Concurrent", func(t *testing.T) {
		t.Parallel()
		// start multiple Stop() calls concurrently with different timeouts
		timeouts := []time.Duration{18 * time.Millisecond, 36 * time.Millisecond, 180 * time.Millisecond}
		// we do the actual testing within a call to RunProc()
		// because this guarantees that the lifecycle is not
		// respecting its Stopped() signal.
		RunProc(func(l *L) {
			stopped := make(chan bool, len(timeouts))
			for i := range timeouts {
				go func(timeout time.Duration) {
					stopped <- l.Stop(timeout)
				}(timeouts[i])
			}
			// wait for all Stop() calls to return
			timer := time.NewTimer(SyncTimeout)
			defer timer.Stop()
			for i := range timeouts {
				select {
				case ok := <-stopped:
					if ok {
						t.Errorf("Stop() should have failed (already stopped: %d/%d)", i, len(timeouts))
					}
				case <-timer.C:
					t.Fatal("timeout: Stop() did not return")
				}
			}
		}, WithName(t.Name()))
	})

	t.Run("ChildLifecycle", func(t *testing.T) {
		t.Parallel()
		stopped := make(chan bool)
		ready := make(chan struct{})
		RunProc(func(l *L) {
			go func() {
				// stop parent-lifecycle immediately after the two
				// child-lifecycles have been scheduled (by Go())
				// because starting them after initiating the stop
				// causes a panic.
				<-ready
				<-ready
				stopped <- l.Stop(time.Second)
			}()

			l.Go("child1", func(l *L) {
				ready <- struct{}{}
				<-l.Stopping() // wait for child-lifecycle to signal a stop
			})
			l.Go("child2", func(l *L) {
				ready <- struct{}{}
				<-l.Stopping() // wait for child-lifecycle to signal a stop
			})
			<-l.Stopping() // wait for parent-lifecycle to signal a stop
		}, WithName(t.Name()))
		if !<-stopped {
			t.Error("Stop() should have succeeded")
		}
	})
}

func TestL_Continue(t *testing.T) {
	stopper := make(chan struct{})
	RunProc(func(l *L) {
		if !l.Continue() {
			t.Error("Continue() should have returned true")
		}

		close(stopper) // stop the lifecycle
		// synchronise with the lifecycle signal propagation (otherwise Continue() might
		// return before the lifecycle has had a chance to propagate the stop signal)
		select {
		case <-l.Stopping():
		case <-time.After(SyncTimeout):
			t.Fatal("timeout: lifecycle stop did not synchronise with Continue()")
		}

		if l.Continue() {
			t.Error("Continue() should have returned false")
		}
	}, WithName(t.Name()), WithStopper(stopper))
}
