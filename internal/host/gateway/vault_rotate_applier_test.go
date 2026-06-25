package gateway

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

func rotateAfter(t *testing.T, credential string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]any{"vaultRotate": map[string]any{"credential": credential}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestVaultRotateApplierSignalsApprovedRotation(t *testing.T) {
	var gotID contract.AgentGroupID
	var gotCred string
	called := 0
	rotate := func(id contract.AgentGroupID, cred string) error {
		called++
		gotID = id
		gotCred = cred
		return nil
	}
	a := NewVaultRotateApplier(rotate, nil)
	req := contract.ChangeRequest{
		Kind:         contract.ChangePermissions,
		AgentGroupID: "grp-1",
		After:        rotateAfter(t, "GitHub"),
	}
	if err := a.Apply(context.Background(), req, contract.Decision{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if called != 1 {
		t.Fatalf("rotate called %d times, want 1", called)
	}
	if gotID != "grp-1" {
		t.Fatalf("group id = %q, want grp-1 (must come from req, not payload)", gotID)
	}
	if gotCred != "github" {
		t.Fatalf("credential = %q, want normalized github", gotCred)
	}
}

func TestVaultRotateApplierPassesThroughNonRotatePayload(t *testing.T) {
	called := 0
	rotate := func(contract.AgentGroupID, string) error { called++; return nil }
	a := NewVaultRotateApplier(rotate, nil)
	req := contract.ChangeRequest{
		Kind:         contract.ChangePermissions,
		AgentGroupID: "grp-1",
		After:        json.RawMessage(`{"vaultPolicy":{"rules":[]}}`),
	}
	if err := a.Apply(context.Background(), req, contract.Decision{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if called != 0 {
		t.Fatalf("rotate called %d times for a non-rotate payload, want 0", called)
	}
}

func TestVaultRotateApplierErrorsWhenSignallerMissing(t *testing.T) {
	a := NewVaultRotateApplier(nil, nil)
	req := contract.ChangeRequest{
		Kind:         contract.ChangePermissions,
		AgentGroupID: "grp-1",
		After:        rotateAfter(t, "github"),
	}
	if err := a.Apply(context.Background(), req, contract.Decision{}); err == nil {
		t.Fatal("Apply with a recognized rotation but no signaller should error, not silently drop it")
	}
}

func TestVaultRotateVerifierRequiresHuman(t *testing.T) {
	v := VaultRotateVerifier{}
	req := contract.ChangeRequest{AgentGroupID: "grp-1", After: rotateAfter(t, "github")}
	verdict, _, err := v.Verify(context.Background(), req)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verdict != contract.VerdictRequireHuman {
		t.Fatalf("verdict = %v, want RequireHuman (rotation is privileged)", verdict)
	}
}

func TestVaultRotateVerifierRejectsBadName(t *testing.T) {
	v := VaultRotateVerifier{}
	req := contract.ChangeRequest{AgentGroupID: "grp-1", After: rotateAfter(t, "../etc/passwd")}
	verdict, _, err := v.Verify(context.Background(), req)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verdict != contract.VerdictReject {
		t.Fatalf("verdict = %v, want Reject for a path-traversal credential name", verdict)
	}
}

func TestVaultRotateVerifierRejectsMissingGroup(t *testing.T) {
	v := VaultRotateVerifier{}
	req := contract.ChangeRequest{After: rotateAfter(t, "github")}
	verdict, _, err := v.Verify(context.Background(), req)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verdict != contract.VerdictReject {
		t.Fatalf("verdict = %v, want Reject when no agent group is targeted", verdict)
	}
}

func TestVaultRotateVerifierPassesThroughNonRotate(t *testing.T) {
	v := VaultRotateVerifier{}
	req := contract.ChangeRequest{AgentGroupID: "grp-1", After: json.RawMessage(`{"vaultPolicy":{"rules":[]}}`)}
	verdict, _, err := v.Verify(context.Background(), req)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verdict != contract.VerdictPass {
		t.Fatalf("verdict = %v, want Pass for a non-rotate payload", verdict)
	}
}
