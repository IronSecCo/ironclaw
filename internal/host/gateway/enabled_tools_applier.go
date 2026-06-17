// OWNER: T-096 (apply-side — materialize approved enabled-tools changes)

package gateway

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// SetEnabledToolsFunc stores a group's approved enabled-tools set. Satisfied
// host-side in cmd/controlplane; a seam so the gateway stays decoupled from the
// registry package.
type SetEnabledToolsFunc func(id contract.AgentGroupID, tools []string) error

// EnabledToolsApplier materializes an approved ChangeEnabledTools: it parses the
// tool list (payload {"tools": [...]}) and stores it on the target group, so the
// next sandbox launch is restricted to that subset (the mandatory request/ask tools
// are always kept — see tools.FilterRegistry). Every other kind passes through.
type EnabledToolsApplier struct {
	set  SetEnabledToolsFunc
	next contract.Applier
}

// NewEnabledToolsApplier wraps next. set may be nil (a ChangeEnabledTools then errors
// rather than silently dropping); next may be nil.
func NewEnabledToolsApplier(set SetEnabledToolsFunc, next contract.Applier) *EnabledToolsApplier {
	return &EnabledToolsApplier{set: set, next: next}
}

// Apply stores an approved enabled-tools set, then delegates.
func (a *EnabledToolsApplier) Apply(ctx context.Context, req contract.ChangeRequest, d contract.Decision) error {
	if req.Kind == contract.ChangeEnabledTools {
		var p struct {
			Tools []string `json:"tools"`
		}
		if err := json.Unmarshal(req.After, &p); err != nil {
			return fmt.Errorf("enabled_tools apply: parse payload: %w", err)
		}
		if a.set == nil {
			return fmt.Errorf("enabled_tools apply: no enabled-tools setter wired")
		}
		if err := a.set(req.AgentGroupID, p.Tools); err != nil {
			return fmt.Errorf("enabled_tools apply: %w", err)
		}
	}
	if a.next != nil {
		return a.next.Apply(ctx, req, d)
	}
	return nil
}
