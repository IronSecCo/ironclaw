// OWNER: T-234 (apply-side — materialize approved persona changes)

package gateway

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// SetPersonaFunc stores an approved persona for a group. Satisfied host-side by
// registry.SetPersona (bound in cmd/controlplane); kept as a seam so the gateway
// does not depend on the registry package.
type SetPersonaFunc func(id contract.AgentGroupID, persona string) error

// PersonaApplier materializes an approved ChangePersona: it parses the persona from
// the change payload and stores it on the target group, so the next sandbox launch
// loads it into the system prompt (T-234). Every other kind passes through to next.
type PersonaApplier struct {
	set  SetPersonaFunc
	next contract.Applier
}

// NewPersonaApplier wraps next with persona materialization. set may be nil (a
// ChangePersona then errors rather than silently dropping); next may be nil.
func NewPersonaApplier(set SetPersonaFunc, next contract.Applier) *PersonaApplier {
	return &PersonaApplier{set: set, next: next}
}

// Apply stores an approved persona, then delegates. The gateway only invokes Apply
// for approved changes, so reaching ChangePersona here means a human granted it.
func (a *PersonaApplier) Apply(ctx context.Context, req contract.ChangeRequest, d contract.Decision) error {
	if req.Kind == contract.ChangePersona {
		var p struct {
			Persona string `json:"persona"`
		}
		if err := json.Unmarshal(req.After, &p); err != nil {
			return fmt.Errorf("persona apply: parse payload: %w", err)
		}
		if a.set == nil {
			return fmt.Errorf("persona apply: no persona setter wired")
		}
		if err := a.set(req.AgentGroupID, p.Persona); err != nil {
			return fmt.Errorf("persona apply: %w", err)
		}
	}
	if a.next != nil {
		return a.next.Apply(ctx, req, d)
	}
	return nil
}
