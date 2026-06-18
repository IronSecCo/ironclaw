package gateway

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// AddInstalledSkillFunc records an approved skill install on a group. Satisfied
// host-side by registry.AddInstalledSkill (cmd/controlplane); a seam so the gateway
// stays decoupled from the registry package.
type AddInstalledSkillFunc func(id contract.AgentGroupID, name, version string) error

// SkillInstallApplier materializes an approved skill install. A skill install rides a
// ChangePermissions whose payload names the skill (internal/host/skills.SkillInstall);
// this records {name, version} on the group so the next launch mounts its read-only
// assets at /skills/<name>. A ChangePermissions that is NOT a skill install (no
// "skill" field) passes through untouched, as does every other kind. (The install's
// egress grant is handled by EgressApplier; its tools are already available unless
// the group is restricted.)
type SkillInstallApplier struct {
	add  AddInstalledSkillFunc
	next contract.Applier
}

// NewSkillInstallApplier wraps next. add may be nil (a recognized skill install then
// errors rather than silently dropping); next may be nil.
func NewSkillInstallApplier(add AddInstalledSkillFunc, next contract.Applier) *SkillInstallApplier {
	return &SkillInstallApplier{add: add, next: next}
}

// Apply records an approved skill install, then delegates.
func (a *SkillInstallApplier) Apply(ctx context.Context, req contract.ChangeRequest, d contract.Decision) error {
	if req.Kind == contract.ChangePermissions && len(req.After) > 0 {
		var p struct {
			Skill   string `json:"skill"`
			Version string `json:"version"`
		}
		// Best-effort: only a payload that names a skill is a skill install; any other
		// ChangePermissions (or unparseable payload) just passes through.
		if err := json.Unmarshal(req.After, &p); err == nil && p.Skill != "" {
			if a.add == nil {
				return fmt.Errorf("skill install apply: no installer wired")
			}
			if err := a.add(req.AgentGroupID, p.Skill, p.Version); err != nil {
				return fmt.Errorf("skill install apply: %w", err)
			}
		}
	}
	if a.next != nil {
		return a.next.Apply(ctx, req, d)
	}
	return nil
}
