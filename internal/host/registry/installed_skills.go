package registry

import (
	"fmt"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// AddInstalledSkill records (or upgrades) a skill installed into a group, so the
// next sandbox launch mounts its assets. It is the host-side seam the gateway's
// skill-install applier calls AFTER a human approves the install; the sandbox can
// never reach it. A skill already installed under the same name has its version
// updated in place (one version per skill per group). Returns an error if the group
// does not exist or the identifiers are invalid.
func AddInstalledSkill(r Registry, id contract.AgentGroupID, name, version string) error {
	if name == "" || version == "" {
		return fmt.Errorf("registry: install requires a skill name and version")
	}
	g, ok := r.GetAgentGroup(id)
	if !ok {
		return fmt.Errorf("registry: agent group %q not found", id)
	}
	for i := range g.InstalledSkills {
		if g.InstalledSkills[i].Name == name {
			g.InstalledSkills[i].Version = version // upgrade in place
			return r.PutAgentGroup(g)
		}
	}
	g.InstalledSkills = append(g.InstalledSkills, InstalledSkill{Name: name, Version: version})
	return r.PutAgentGroup(g)
}
