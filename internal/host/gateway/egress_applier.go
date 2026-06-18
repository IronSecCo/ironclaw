package gateway

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// Allower receives an approved egress host so the grant takes effect. It is
// satisfied by the egress broker (internal/host/egress.Broker.Allow); kept as a tiny
// interface so the gateway does not depend on the egress package.
type Allower interface {
	Allow(host string)
}

// EgressApplier materializes the EGRESS portion of an approved change: when the
// change payload carries egress hosts (e.g. a skill install's bundle — see
// internal/host/skills.SkillInstall), each host is added to the broker's
// deny-by-default allowlist so the grant actually takes effect. This is the wiring
// the broker's allowlist anticipates ("mutated only after the change clears the
// gateway's human approval"). Every other payload field, and every other kind, pass
// through to next unchanged.
type EgressApplier struct {
	allower Allower
	next    contract.Applier
}

// NewEgressApplier wraps next with egress materialization. allower may be nil (egress
// grants then no-op); next may be nil (delegation then no-ops).
func NewEgressApplier(allower Allower, next contract.Applier) *EgressApplier {
	return &EgressApplier{allower: allower, next: next}
}

// Apply allowlists any egress hosts in the approved change payload, then delegates.
// The gateway only invokes Apply for approved changes, so reaching here means a human
// granted the egress.
func (a *EgressApplier) Apply(ctx context.Context, req contract.ChangeRequest, d contract.Decision) error {
	if a.allower != nil && len(req.After) > 0 {
		var p struct {
			Egress []string `json:"egress"`
		}
		// Best-effort: a payload with no "egress" field simply yields none.
		if err := json.Unmarshal(req.After, &p); err == nil {
			for _, h := range p.Egress {
				if h = strings.TrimSpace(h); h != "" {
					a.allower.Allow(h)
				}
			}
		}
	}
	if a.next != nil {
		return a.next.Apply(ctx, req, d)
	}
	return nil
}
