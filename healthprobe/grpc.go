package healthprobe

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"runtime/pprof"
	"sync"

	"google.golang.org/grpc"
	pb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/danielorbach/go-component"
)

// A gRPCServer serves as a health server that manages the status of the
// application. The server uses Monitor to determine if the system has fully
// loaded ("startup") and if it is alive ("liveness"). This allows external
// systems to query the health status through the gRPC health-check protocol.
//
// Although Check only supports querying the overall "liveness" of an application
// process, a HealthServer keeps track of all procedures that were started. It
// then responds by inspecting the status of all known procs.
type gRPCServer struct {
	Monitor
	pb.UnimplementedHealthServer
}

// StartGRPC initializes and starts the gRPC health server. The server will
// listen for gRPC health check requests and can be gracefully shutdown by the
// Cleanup function.
//
// Since component.L contains context, and we would like to use this context, it
// is redundent to pass context to this function, hence
// nolint:contextcheck
func StartGRPC(l *component.L, lis net.Listener, monitor Monitor) {
	s := grpc.NewServer()
	pb.RegisterHealthServer(s, &gRPCServer{Monitor: monitor})

	// The cleanup triggers the server shutdown process by sending an abort signal
	// and waits for all associated goroutines handling server shutdown to complete.
	// This guarantees no running goroutines and proper resource cleanup.
	var wg sync.WaitGroup
	abort := make(chan struct{}, 1)
	l.Cleanup(func() {
		abort <- struct{}{}
		wg.Wait()
	})

	// GRPC server shutdown based on lifecycle signals by listening to three events:
	// a normal stopping signal (l.Stopping()), which triggers a graceful stop to
	// allow ongoing requests to complete; the cancellation of the component's
	// context (l.Context().Done()), prompting an immediate server stop; and an abort
	// signal via the abort channel, which initiates an immediate stop regardless of
	// context state. This approach ensures that the server can adapt to various
	// stopping scenarios effectively, ensuring resources are released appropriately.
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-l.Stopping():
			s.GracefulStop()
		case <-l.Context().Done():
			s.Stop()
		case <-abort:
			s.Stop()
		}
	}()

	// We must stack the listen and serve, to ensure we clean this resource.
	wg.Add(1)
	go pprof.Do(l.Context(), pprof.Labels("server", "grpc"), func(ctx context.Context) {
		defer wg.Done()
		if err := s.Serve(lis); err != nil {
			slog.Error("serve:", "error", err)
		}
	})
}

// Check responds to HealthCheckRequests by assessing the health status of the
// application.
//
// For "startup" service requests, Check determines whether the application has
// completed its initialization phase. If the application is fully loaded, it
// responds with HealthCheckResponse_SERVING. If the application is still
// initializing or not fully loaded, it returns HealthCheckResponse_NOT_SERVING.
//
// For "liveness" service requests, the method first ensures that the application
// has already completed its startup process. If the application has not yet
// loaded, it returns an error because liveness cannot be accurately determined
// before startup is complete. If the application is determined to be alive and
// operational, it returns HealthCheckResponse_SERVING; otherwise, it returns
// HealthCheckResponse_NOT_SERVING.
//
// If the request specifies a service name that is not supported, Check will
// respond with an error message indicating the unsupported service.
func (s *gRPCServer) Check(_ context.Context, req *pb.HealthCheckRequest) (*pb.HealthCheckResponse, error) {
	switch req.GetService() {
	case "startup":
		if s.Loaded() {
			return &pb.HealthCheckResponse{Status: pb.HealthCheckResponse_SERVING}, nil
		} else {
			return &pb.HealthCheckResponse{Status: pb.HealthCheckResponse_NOT_SERVING}, nil
		}
	case "liveness":
		if !s.Loaded() {
			return nil, fmt.Errorf("liveness called before startup")
		}

		if s.Alive() {
			return &pb.HealthCheckResponse{Status: pb.HealthCheckResponse_SERVING}, nil
		} else {
			return &pb.HealthCheckResponse{Status: pb.HealthCheckResponse_NOT_SERVING}, nil
		}
	default:
		return nil, fmt.Errorf("unsupported service: %s", req.GetService())
	}
}
