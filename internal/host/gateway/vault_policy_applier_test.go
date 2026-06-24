package gateway

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

func vaultAfter(t *testing.T, rules []VaultRule) json.RawMessage {
	t.Helper()
	body := map[string]any{"vaultPolicy": map[string]any{"rules": rules}}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestVaultPolicyApplierRecordsApprovedGrant(t *testing.T) {
	var gotID contract.AgentGroupID
	var gotRules []VaultRule
	called := 0
	set := func(id contract.AgentGroupID, rules []VaultRule) error {
		called++
		gotID = id
		gotRules = rules
		return nil
	}
	a := NewVaultPolicyApplier(set, nil)
	req := contract.ChangeRequest{
		Kind:         contract.ChangePermissions,
		AgentGroupID: "grp-1",
		After:        vaultAfter(t, []VaultRule{{Credential: "github", Hosts: []string{"api.github.com"}}}),
	}
	if err := a.Apply(context.Background(), req, contract.Decision{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if called != 1 {
		t.Fatalf("setter called %d times, want 1", called)
	}
	if gotID != "grp-1" {
		t.Fatalf("group id = %q, want grp-1 (must come from req, not payload)", gotID)
	}
	if len(gotRules) != 1 || gotRules[0].Credential != "github" || len(gotRules[0].Hosts) != 1 {
		t.Fatalf("rules = %+v, want one github rule", gotRules)
	}
}

func TestVaultPolicyApplierPassesThroughNonVaultPayload(t *testing.T) {
	called := 0
	set := func(contract.AgentGroupID, []VaultRule) error { called++; return nil }
	a := NewVaultPolicyApplier(set, nil)
	// A payload with no vaultPolicy field is not a vault change.
	req := contract.ChangeRequest{
		Kind:         contract.ChangeMCPAccess,
		AgentGroupID: "grp-1",
		After:        json.RawMessage(`{"server":"weather","tools":["forecast"]}`),
	}
	if err := a.Apply(context.Background(), req, contract.Decision{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if called != 0 {
		t.Fatalf("setter called %d times for a non-vault payload, want 0", called)
	}
}

func TestVaultPolicyApplierErrorsWhenSetterMissing(t *testing.T) {
	a := NewVaultPolicyApplier(nil, nil)
	req := contract.ChangeRequest{
		Kind:         contract.ChangePermissions,
		AgentGroupID: "grp-1",
		After:        vaultAfter(t, []VaultRule{{Credential: "github", Hosts: []string{"api.github.com"}}}),
	}
	if err := a.Apply(context.Background(), req, contract.Decision{}); err == nil {
		t.Fatal("expected error when a recognized vault change has no setter, got nil")
	}
}

func TestVaultPolicyApplierErrorsWhenNoGroup(t *testing.T) {
	set := func(contract.AgentGroupID, []VaultRule) error { return nil }
	a := NewVaultPolicyApplier(set, nil)
	req := contract.ChangeRequest{
		Kind:  contract.ChangePermissions,
		After: vaultAfter(t, []VaultRule{{Credential: "github", Hosts: []string{"api.github.com"}}}),
	}
	if err := a.Apply(context.Background(), req, contract.Decision{}); err == nil {
		t.Fatal("expected error when a vault change has no agent group id, got nil")
	}
}

func TestVaultPolicyVerifierRequiresHumanForValidGrant(t *testing.T) {
	v := VaultPolicyVerifier{}
	req := contract.ChangeRequest{
		AgentGroupID: "grp-1",
		After:        vaultAfter(t, []VaultRule{{Credential: "github", Hosts: []string{"api.github.com", "uploads.github.com"}}}),
	}
	verdict, _, err := v.Verify(context.Background(), req)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verdict != contract.VerdictRequireHuman {
		t.Fatalf("verdict = %v, want RequireHuman", verdict)
	}
}

func TestVaultPolicyVerifierPassesNonVaultChange(t *testing.T) {
	v := VaultPolicyVerifier{}
	req := contract.ChangeRequest{
		AgentGroupID: "grp-1",
		After:        json.RawMessage(`{"persona":"helpful"}`),
	}
	verdict, _, err := v.Verify(context.Background(), req)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verdict != contract.VerdictPass {
		t.Fatalf("verdict = %v, want Pass for a non-vault change", verdict)
	}
}

func TestVaultPolicyVerifierRejectsMalformed(t *testing.T) {
	v := VaultPolicyVerifier{}
	cases := []struct {
		name  string
		rules []VaultRule
		group contract.AgentGroupID
	}{
		{"bad cred traversal", []VaultRule{{Credential: "../etc", Hosts: []string{"api.github.com"}}}, "grp-1"},
		{"empty cred", []VaultRule{{Credential: "", Hosts: []string{"api.github.com"}}}, "grp-1"},
		{"no hosts", []VaultRule{{Credential: "github", Hosts: nil}}, "grp-1"},
		{"host with scheme", []VaultRule{{Credential: "github", Hosts: []string{"https://api.github.com"}}}, "grp-1"},
		{"host with wildcard", []VaultRule{{Credential: "github", Hosts: []string{"*.github.com"}}}, "grp-1"},
		{"host with port", []VaultRule{{Credential: "github", Hosts: []string{"api.github.com:443"}}}, "grp-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := contract.ChangeRequest{AgentGroupID: tc.group, After: vaultAfter(t, tc.rules)}
			verdict, reason, err := v.Verify(context.Background(), req)
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if verdict != contract.VerdictReject {
				t.Fatalf("verdict = %v (reason %q), want Reject", verdict, reason)
			}
		})
	}
}

func TestVaultPolicyVerifierRejectsMissingGroup(t *testing.T) {
	v := VaultPolicyVerifier{}
	req := contract.ChangeRequest{After: vaultAfter(t, []VaultRule{{Credential: "github", Hosts: []string{"api.github.com"}}})}
	verdict, _, err := v.Verify(context.Background(), req)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verdict != contract.VerdictReject {
		t.Fatalf("verdict = %v, want Reject when group id is missing", verdict)
	}
}
