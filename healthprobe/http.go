package healthprobe

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/pprof"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/danielorbach/go-component"
)

// StartHTTP initializes and runs an HTTP server dedicated to handling health
// check requests. The server provides two endpoints: "/health/startup" and
// "/health/liveness", each corresponding to a specific health check type. The
// server uses the Monitor interface to evaluate the application's startup and
// liveness status.
//
//   - The "/health/startup" endpoint checks if the application has successfully initialized
//     and is ready to serve. It returns a 200 OK response with a status of "SERVING"
//     if loaded, otherwise a 500 Internal Server Error with a status of
//     "NOT_SERVING".
//
//   - The "/health/liveness" endpoint verifies if the application is currently
//     alive and operational. It ensures that the startup check has passed. If the
//     application has not started yet, it responds with a 503 Service Unavailable
//     and an error message. If alive, it returns a 200 OK with a "SERVING" status;
//     otherwise, it sends a 500 Internal Server Error with "NOT_SERVING".
//
// Note for the users of this function, the http server wasn't tested, so the use
// of this is on your own risk, have fun ;)
func StartHTTP(l *component.L, address string, monitor Monitor) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/startup", HandleStartupRequest(monitor))
	mux.HandleFunc("/health/liveness", HandleLivenessRequest(monitor))
	s := &http.Server{
		Addr: address,
		// TODO(@yuvalmendelovich): once we figure out how to use traces, fill operation
		//  with meaningful value instead of "server".
		Handler:           otelhttp.NewHandler(mux, "server"),
		ReadHeaderTimeout: 3 * time.Second, // See <https://app.deepsource.com/directory/analyzers/go/issues/GO-S2112>.
	}

	// The cleanup triggers the server shutdown process by sending an abort signal
	// and waits for all associated goroutines handling server shutdown to complete.
	// This guarantees no running goroutines and proper resource cleanup.
	var wg sync.WaitGroup
	abort := make(chan struct{}, 1)
	l.Cleanup(func() {
		abort <- struct{}{}
		wg.Wait()
	})

	// HTTP server shutdown based on lifecycle signals by listening to three events:
	// a normal stopping signal (l.Stopping()), which triggers a graceful shutdown to
	// allow ongoing requests to complete; the cancellation of the component's
	// context (l.Context().Done()), prompting an immediate server closure; and an
	// abort signal via the abort channel, which initiates an immediate shutdown
	// regardless of context state. This approach ensures that the server can adapt
	// to various shutdown scenarios effectively, ensuring resources are released
	// appropriately.
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-l.Stopping():
			if err := s.Shutdown(l.Context()); err != nil {
				slog.Error("shutdown http server", "error", err)
			}
		case <-l.Context().Done():
			if err := s.Close(); err != nil {
				slog.Error("close http server", "error", err)
			}
		case <-abort:
			err := s.Shutdown(context.Background())
			if err != nil {
				slog.Error("shutdown http server", "error", err)
			}
		}
	}()

	// We must stack the listen and serve, to ensure we clean this resource.
	wg.Add(1)
	go pprof.Do(l.Context(), pprof.Labels("server", "http"), func(ctx context.Context) {
		defer wg.Done()
		if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("listen and serve", "error", err)
		}
	})
}

// httpHealthResponse defines the format of a health check response sent by the
// HTTP server. It contains a single field, Status, that represents the current
// health status of the application, such as "SERVING" or "NOT_SERVING", conveyed
// in JSON format. It is more for debuting purposes as kubernetes health checks
// relies on the status code.
type httpHealthResponse struct {
	Status string `json:"status"`
}

func HandleStartupRequest(monitor Monitor) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if monitor.Loaded() {
			writeJSONResponse(w, http.StatusOK, httpHealthResponse{Status: "SERVING"})
		} else {
			writeJSONResponse(w, http.StatusInternalServerError, httpHealthResponse{Status: "NOT_SERVING"})
		}
	}
}

func HandleLivenessRequest(monitor Monitor) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if !monitor.Loaded() {
			http.Error(w, "liveness called before startup", http.StatusServiceUnavailable)
			return
		}
		if monitor.Alive() {
			writeJSONResponse(w, http.StatusOK, httpHealthResponse{Status: "SERVING"})
		} else {
			writeJSONResponse(w, http.StatusInternalServerError, httpHealthResponse{Status: "NOT_SERVING"})
		}
	}
}

func writeJSONResponse(w http.ResponseWriter, status int, response httpHealthResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("encoding JSON response", "error", err)
	}
}
