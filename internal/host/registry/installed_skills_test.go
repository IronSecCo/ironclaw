package registry

import (
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
)

func TestAddInstalledSkill(t *testing.T) {
	r := NewMemRegistry()
	const id contract.AgentGroupID = "grp-1"
	if err := r.PutAgentGroup(AgentGroup{ID: id, Name: "Triage"}); err != nil {
		t.Fatal(err)
	}

	if err := AddInstalledSkill(r, id, "incident-triage", "1.4.0"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := AddInstalledSkill(r, id, "status-page", "0.1.0"); err != nil {
		t.Fatalf("add 2: %v", err)
	}
	g, _ := r.GetAgentGroup(id)
	if len(g.InstalledSkills) != 2 {
		t.Fatalf("want 2 installed skills, got %v", g.InstalledSkills)
	}

	// Upgrade in place: same name, new version => still 2, version updated.
	if err := AddInstalledSkill(r, id, "incident-triage", "1.5.0"); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	g, _ = r.GetAgentGroup(id)
	if len(g.InstalledSkills) != 2 {
		t.Fatalf("upgrade must not duplicate: %v", g.InstalledSkills)
	}
	var ver string
	for _, s := range g.InstalledSkills {
		if s.Name == "incident-triage" {
			ver = s.Version
		}
	}
	if ver != "1.5.0" {
		t.Fatalf("version not upgraded in place, got %q", ver)
	}
}

func TestAddInstalledSkillRejects(t *testing.T) {
	r := NewMemRegistry()
	if err := AddInstalledSkill(r, "ghost", "x", "1"); err == nil {
		t.Error("unknown group must error")
	}
	_ = r.PutAgentGroup(AgentGroup{ID: "g"})
	if err := AddInstalledSkill(r, "g", "", "1"); err == nil {
		t.Error("empty name must error")
	}
	if err := AddInstalledSkill(r, "g", "x", ""); err == nil {
		t.Error("empty version must error")
	}
}
