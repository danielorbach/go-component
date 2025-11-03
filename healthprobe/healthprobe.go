// Package healthprobe provides HTTP and gRPC health-check implementations
// that monitor the startup and liveness status of an application. It exposes
// endpoints to confirm whether an application has successfully initialized
// and is currently operational, ensuring readiness and health compliance for
// services relying on these checks.
//
// The package defines both HTTP and gRPC servers, each offering startup and
// liveness endpoints. These endpoints enable external systems to perform
// health checks via HTTP requests or gRPC calls, facilitating integration
// with orchestration tools such as Kubernetes for robust health monitoring.
//
// Users should note that while the HTTP server setup illustrates proper
// endpoint registration and resource cleanup, it has not been tested
// extensively. Thus, it should be utilized at your own risk. The core
// functionality revolves around the `Monitor` interface, which services use
// to report their respective health status based on custom business logic.
package healthprobe

// Monitor defines methods to assess the health state of an application. It
// provides the functionality needed to determine both the startup and liveness
// statuses, which are critical for health-checking mechanisms integrated with
// the application's services.
type Monitor interface {
	// Alive returns a boolean indicating the application's current liveness status.
	// True signifies that the application is running and capable of performing its
	// intended operations without any critical failures. Conversely, a false value
	// indicates that the application is facing issues that impair its ability to
	// function properly.
	Alive() (liveness bool)
	// Loaded returns a boolean indicating if the application's startup phase is
	// complete. True value means that the application has successfully loaded all
	// required components and is fully operational. False return value indicates
	// that the application is still in the process of starting up or initializing,
	// suggesting that it might not yet be ready to handle full workloads or process
	// requests effectively.
	Loaded() (startup bool)
}
