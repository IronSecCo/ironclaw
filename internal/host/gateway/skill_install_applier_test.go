package gateway

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

func TestSkillInstallApplierRecordsThenDelegates(t *testing.T) {
	var gotID contract.AgentGroupID
	var gotName, gotVer string
	add := func(id contract.AgentGroupID, name, version string) error {
		gotID, gotName, gotVer = id, name, version
		return nil
	}
	next := &countingApplier{}
	a := NewSkillInstallApplier(add, next)

	// A skill-install payload (mirrors skills.SkillInstall).
	after, _ := json.Marshal(map[string]any{"skill": "incident-triage", "version": "1.4.0", "egress": []string{"x.com"}})
	req := contract.ChangeRequest{Kind: contract.ChangePermissions, AgentGroupID: "grp-1", After: after}
	if err := a.Apply(context.Background(), req, contract.Decision{Outcome: "approve"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if gotID != "grp-1" || gotName != "incident-triage" || gotVer != "1.4.0" {
		t.Fatalf("installer got (%q,%q,%q)", gotID, gotName, gotVer)
	}
	if next.n != 1 {
		t.Errorf("next must be called once (n=%d)", next.n)
	}
}

func TestSkillInstallApplierIgnoresNonSkillPermissions(t *testing.T) {
	called := false
	a := NewSkillInstallApplier(func(contract.AgentGroupID, string, string) error { called = true; return nil }, &countingApplier{})
	// A ChangePermissions with no "skill" field is not a skill install.
	after, _ := json.Marshal(map[string]any{"permissions": []string{"admin"}})
	if err := a.Apply(context.Background(), contract.ChangeRequest{Kind: contract.ChangePermissions, After: after}, contract.Decision{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if called {
		t.Error("a non-skill ChangePermissions must not record an install")
	}
}

func TestSkillInstallApplierIgnoresOtherKinds(t *testing.T) {
	called := false
	a := NewSkillInstallApplier(func(contract.AgentGroupID, string, string) error { called = true; return nil }, &countingApplier{})
	after, _ := json.Marshal(map[string]any{"skill": "x", "version": "1"})
	if err := a.Apply(context.Background(), contract.ChangeRequest{Kind: contract.ChangePersona, After: after}, contract.Decision{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if called {
		t.Error("a non-ChangePermissions kind must not record an install")
	}
}
