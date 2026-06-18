package gateway

import (
	"context"
	"sync"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// GroupRespawner stops an agent group's live sessions so the next message relaunches
// them with the just-approved configuration. Satisfied host-side by the
// session.Manager (RespawnGroup). Kept as a tiny interface so the gateway does not
// depend on the session package.
type GroupRespawner interface {
	RespawnGroup(id contract.AgentGroupID) int
}

// RespawnApplier makes an approved change take effect on ALREADY-RUNNING sessions.
// The request→approve→apply chain stores a grant in the registry, but a sandbox reads
// its launch spec (enabled tools, persona, skill mounts) only at launch — so without
// this a granted tool would not reach a live agent until its next cold start, which
// reads to an operator as "approval did nothing". After the inner appliers materialize
// the change, this stops the target group's live sandboxes for the kinds that alter a
// launch spec; the next message relaunches them with the capability. Egress-host grants
// already take effect live in the broker, so they need no respawn.
//
// The respawner is set after construction (SetRespawner) because the session manager is
// built after the gateway; until then this is a transparent pass-through.
type RespawnApplier struct {
	mu      sync.RWMutex
	respawn GroupRespawner
	next    contract.Applier
}

// NewRespawnApplier wraps next. respawn may be nil now and supplied later via
// SetRespawner; next may be nil (delegation then no-ops).
func NewRespawnApplier(respawn GroupRespawner, next contract.Applier) *RespawnApplier {
	return &RespawnApplier{respawn: respawn, next: next}
}

// SetRespawner installs the live-lifecycle hook after construction. Safe for
// concurrent use with Apply.
func (a *RespawnApplier) SetRespawner(r GroupRespawner) {
	a.mu.Lock()
	a.respawn = r
	a.mu.Unlock()
}

// Apply materializes the change via the inner chain FIRST (so the new config is stored
// before any relaunch reads it), then relaunches the group's live sessions when the
// change alters a launch spec.
func (a *RespawnApplier) Apply(ctx context.Context, req contract.ChangeRequest, d contract.Decision) error {
	if a.next != nil {
		if err := a.next.Apply(ctx, req, d); err != nil {
			return err
		}
	}
	if !affectsLaunchSpec(req.Kind) || req.AgentGroupID == "" {
		return nil
	}
	a.mu.RLock()
	r := a.respawn
	a.mu.RUnlock()
	if r != nil {
		r.RespawnGroup(req.AgentGroupID)
	}
	return nil
}

// affectsLaunchSpec reports whether an approved change alters what a sandbox is
// launched with, so a running session must be relaunched to pick it up. Persona,
// enabled tools, packages, and mounts feed the spec directly; permissions covers skill
// installs (a bundle of mounts + enabled tools). Wiring (message routing) and
// create_agent (no live session yet) do not change a running sandbox's spec.
func affectsLaunchSpec(k contract.ChangeKind) bool {
	switch k {
	case contract.ChangePersona, contract.ChangeEnabledTools, contract.ChangePackages,
		contract.ChangeMounts, contract.ChangePermissions, contract.ChangeMCPAccess:
		// ChangeMCPAccess alters the per-session MCP broker surface a sandbox reads at
		// launch, so a live session must relaunch to see the newly-granted tools.
		return true
	default:
		return false
	}
}
