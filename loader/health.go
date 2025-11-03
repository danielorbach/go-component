package loader

import (
	"sync"
)

var (
	// Health exposes the internal lifecycle of the loading process.
	//
	// The loader package takes control over the entire process, and is used like a
	// singleton. As such, it reports the health of the entire process. This
	// behaviour is supported by a single global variable.
	Health health

	HealthAddress string
)

func init() {
	Health = health{procs: make(map[string]bool)}
}

type health struct {
	mu     sync.Mutex
	loaded bool
	procs  map[string]bool // map[name]running
}

func (h *health) LoadingCompleted() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.loaded = true
}

func (h *health) Loaded() (startup bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.loaded
}

func (h *health) ProcStarted(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.procs[name] = true
}

func (h *health) ProcCompleted(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// We set to false instead of deleting from the internal map because that allows
	// us to differentiate "dead" procs from those that never ran. We expect a closed
	// set of procs, so this map will not "leak" memory, although it grows
	// "indefinitely".
	h.procs[name] = false
}

func (h *health) Alive() (liveness bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, running := range h.procs {
		if !running {
			return false
		}
	}
	return true
}
