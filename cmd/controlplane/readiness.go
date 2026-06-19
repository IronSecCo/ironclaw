package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// readinessGate tracks per-subsystem readiness for the API's /readyz probe. It is
// created with the set of subsystems that must be live before the daemon can
// serve real traffic, all initially pending; each subsystem flips its own flag
// once it is up. check() (passed to api.WithReadiness) returns a non-nil error
// naming the still-pending subsystems until every one has reported ready, so
// /readyz stays 503 during startup and only flips to 200 once the control-plane
// can actually do work. /healthz (liveness) is unaffected — the process is alive
// the moment it binds; /readyz additionally asserts the serving loops are up.
type readinessGate struct {
	mu      sync.Mutex
	pending map[string]struct{}
}

// newReadinessGate builds a gate whose every named subsystem starts not-ready.
// With no subsystems it is ready immediately (check returns nil).
func newReadinessGate(subsystems ...string) *readinessGate {
	g := &readinessGate{pending: make(map[string]struct{}, len(subsystems))}
	for _, s := range subsystems {
		g.pending[s] = struct{}{}
	}
	return g
}

// markReady records that subsystem name is live. Unknown or duplicate names are
// ignored, so a double-signal (or a signal for a subsystem this gate does not
// track) is harmless and never makes the gate go backwards.
func (g *readinessGate) markReady(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.pending, name)
}

// check is the func handed to api.WithReadiness: nil once every tracked
// subsystem has reported ready, otherwise an error naming what is still pending
// (sorted, so the message is deterministic).
func (g *readinessGate) check() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.pending) == 0 {
		return nil
	}
	names := make([]string, 0, len(g.pending))
	for n := range g.pending {
		names = append(names, n)
	}
	sort.Strings(names)
	return fmt.Errorf("subsystems not ready: %s", strings.Join(names, ", "))
}
