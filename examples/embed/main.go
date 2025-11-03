// This 'embed' example demonstrates how to use the loader package to manually
// construct a loader.Footprint with resource allocations that link three
// components together - ping, pong and probe.
//
// These components communicate over Kafka pub-sub topics, just like
// production-ready service executables.
//
// For simplicity, run all components in a single executable:
//
//	go run github.com/danielorbach/go-component/examples/embed
//
// To simulate a real deployment, run each component in its own executable:
//  1. To run the only Ping component, use the '-ping' flag.
//  2. To run the only Pong component, use the '-pong' flag.
//  3. To run the only Probe component, use the '-probe' flag.
package main

import (
	_ "embed"

	"github.com/danielorbach/go-component/fileloader"
	"github.com/danielorbach/go-component/loader"
)

//go:embed footprint.yaml
var footprint []byte

func main() {
	loader.ParseFlags(PingComponent, PongComponent, ProbeComponent)
	fileloader.Load(footprint, fileloader.FormatYAML)
}

// Doc contains the documentation for the services of this example.
//
// Usually, there is one doc.go file per service package, but to simplify the
// example, we put all documentation in one place as a constant.
const Doc = `
// This is the content of a doc.go file.
//
// Each service definition is documented in a separate section.
// Each section starts with a header that contains the name of the service.
// The header is followed by a one-liner description of the service, followed
// by a blank line.
// The rest of the section is the detailed description of the service.
// The section ends where the next section starts, or at the end of the file.
//
// # Service ping
//
// ping: continually publishes a simple message to the PingTarget aspect
//
// The ping component sends simple messages every PingInterval duration on 
// its aspect (PingTarget).
//
// # Service pong
//
// pong: continually subscribes to simple message from the PingTarget interest
//
// The pong component receives simple messages from the pong component and echoes
// them onto its aspect (PongTarget).
//
// # Service probe
//
// probe: continually subscribes to simple message from the PongTarget interest
//
// The probe component receives messages from its list of interests and echoes
// them to the lifecycle log.
package example
`
