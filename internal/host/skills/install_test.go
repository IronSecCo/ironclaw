package skills

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/gateway"
)

func sampleManifest() *Manifest {
	return &Manifest{
		APIVersion: APIVersion,
		Name:       "incident-triage",
		Version:    "1.4.0",
		Grants: Grants{
			Persona: "Stay calm and follow the runbook.",
			Tools:   []string{"send_message"},
			Egress:  []string{"status.example.com"},
			Assets:  []string{"runbook.md"},
		},
	}
}

func TestBuildChangeRequestBundlesGrants(t *testing.T) {
	cr, err := BuildChangeRequest(sampleManifest(), "ag1", "user:admin")
	if err != nil {
		t.Fatalf("BuildChangeRequest: %v", err)
	}
	if cr.Kind != contract.ChangePermissions {
		t.Errorf("kind = %q, want %q", cr.Kind, contract.ChangePermissions)
	}
	if cr.AgentGroupID != "ag1" || cr.RequestedBy != "user:admin" {
		t.Errorf("target/requester wrong: %+v", cr)
	}
	if cr.CreatedAt.IsZero() {
		t.Error("CreatedAt not set")
	}

	var p SkillInstall
	if err := json.Unmarshal(cr.After, &p); err != nil {
		t.Fatalf("decode After: %v", err)
	}
	if p.Skill != "incident-triage" || p.Version != "1.4.0" {
		t.Errorf("skill/version wrong: %+v", p)
	}
	if len(p.Tools) != 1 || p.Tools[0] != "send_message" {
		t.Errorf("tools = %v", p.Tools)
	}
	if len(p.Egress) != 1 || p.Egress[0] != "status.example.com" {
		t.Errorf("egress = %v", p.Egress)
	}
	if p.Mount != "/skills/incident-triage" {
		t.Errorf("mount = %q, want /skills/incident-triage", p.Mount)
	}
}

func TestBuildChangeRequestNoMountWithoutAssets(t *testing.T) {
	m := sampleManifest()
	m.Grants.Assets = nil
	cr, err := BuildChangeRequest(m, "ag1", "user:admin")
	if err != nil {
		t.Fatal(err)
	}
	var p SkillInstall
	_ = json.Unmarshal(cr.After, &p)
	if p.Mount != "" {
		t.Errorf("mount = %q, want empty when no assets", p.Mount)
	}
}

// TestInstallPayloadConfigOnly is the sealed-runtime guard: the install payload
// may only ever carry config grants — never a command/script/rootfs/exec field
// that could smuggle code past the gateway.
func TestInstallPayloadConfigOnly(t *testing.T) {
	cr, err := BuildChangeRequest(sampleManifest(), "ag1", "user:admin")
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(cr.After, &raw); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"skill": true, "version": true, "persona": true,
		"tools": true, "egress": true, "mount": true, "assets": true,
	}
	for k := range raw {
		if !allowed[k] {
			t.Errorf("install payload carries non-config field %q (sealed-runtime violation)", k)
		}
	}
}

func TestBuildChangeRequestFailsClosed(t *testing.T) {
	if _, err := BuildChangeRequest(nil, "ag1", "u"); err == nil {
		t.Error("nil manifest should error")
	}
	if _, err := BuildChangeRequest(sampleManifest(), "", "u"); err == nil {
		t.Error("empty agent group id should error")
	}
	bad := sampleManifest()
	bad.Name = "Not A Valid Name"
	if _, err := BuildChangeRequest(bad, "ag1", "u"); err == nil {
		t.Error("unvalidated/bad manifest name should error")
	}
}

// TestSkillInstallClearsHumanFloor proves a skill install rides the gateway like
// any other capability change: the kind-specific verifiers do not reject it, and
// the AlwaysRequireHuman floor holds it for a human (never auto-approved).
func TestSkillInstallClearsHumanFloor(t *testing.T) {
	cr, err := BuildChangeRequest(sampleManifest(), "ag1", "user:admin")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	mountV := gateway.MountAllowlistVerifier{AllowedPrefixes: []string{"/srv/mounts"}}
	if v, _, _ := mountV.Verify(ctx, cr); v != contract.VerdictPass {
		t.Errorf("mount verifier verdict = %v, want Pass (skill install is not a mount change)", v)
	}
	pkgV := gateway.PackageNameVerifier{}
	if v, _, _ := pkgV.Verify(ctx, cr); v != contract.VerdictPass {
		t.Errorf("package verifier verdict = %v, want Pass", v)
	}
	floor := gateway.AlwaysRequireHuman{}
	if v, _, _ := floor.Verify(ctx, cr); v != contract.VerdictRequireHuman {
		t.Errorf("floor verdict = %v, want RequireHuman", v)
	}
}

func TestInstallChangeNilResolver(t *testing.T) {
	if _, err := InstallChange(nil, "x", "1.0.0", "ag1", "u"); err == nil {
		t.Error("nil resolver should error")
	}
}
