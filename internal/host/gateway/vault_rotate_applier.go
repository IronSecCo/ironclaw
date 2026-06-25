package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// Vault credential-secret ROTATION gateway integration (IRO-144). Rotating the secret
// a logical credential maps to is an INJECTOR operation — the control plane never
// holds the secret (threat-model §11) — so a rotation change carries NO secret: only
// the credential NAME and the (trusted) target group taken from the ChangeRequest.
// Like a vault-policy change it rides the gateway's mandatory human-approval floor; on
// approval the applier SIGNALS the host-side injector to re-resolve its held secret
// from the host environment. The secret value never touches the control plane, the
// change body, or the audit log.

// RotateCredentialFunc signals the host-side injector to rotate the held secret for a
// logical credential. Satisfied by a small injector-control client in
// cmd/controlplane; a seam so the gateway stays decoupled from the injector wiring (the
// VaultPolicyApplier/SetVaultPolicyFunc pattern). A nil setter makes an approved
// rotation fail loudly rather than silently no-op.
type RotateCredentialFunc func(id contract.AgentGroupID, credential string) error

// vaultRotatePayload is the rotation portion of a change's After body. A change with no
// vaultRotate field is not a rotation change and passes through untouched.
type vaultRotatePayload struct {
	VaultRotate *struct {
		Credential string `json:"credential"`
	} `json:"vaultRotate"`
}

// VaultRotateApplier materializes an approved credential-rotation change by signalling
// the injector to re-resolve its held secret for the named credential. It is
// kind-agnostic (it keys off the payload, like VaultPolicyApplier): any approved change
// carrying a vaultRotate body triggers a rotation; every other payload and kind passes
// through to next unchanged.
type VaultRotateApplier struct {
	rotate RotateCredentialFunc
	next   contract.Applier
}

// NewVaultRotateApplier wraps next. rotate may be nil (a recognized rotation change
// then errors rather than silently dropping it); next may be nil.
func NewVaultRotateApplier(rotate RotateCredentialFunc, next contract.Applier) *VaultRotateApplier {
	return &VaultRotateApplier{rotate: rotate, next: next}
}

// Apply signals an approved rotation, then delegates. The gateway only invokes Apply
// for approved changes, so reaching here means a human approved the rotation.
func (a *VaultRotateApplier) Apply(ctx context.Context, req contract.ChangeRequest, d contract.Decision) error {
	if len(req.After) > 0 {
		var p vaultRotatePayload
		// Best-effort: a payload with no "vaultRotate" field simply yields none.
		if err := json.Unmarshal(req.After, &p); err == nil && p.VaultRotate != nil {
			if a.rotate == nil {
				return fmt.Errorf("vault rotate apply: no rotation signaller wired")
			}
			if strings.TrimSpace(string(req.AgentGroupID)) == "" {
				return fmt.Errorf("vault rotate apply: change has no agent group id")
			}
			cred := strings.ToLower(strings.TrimSpace(p.VaultRotate.Credential))
			if !validVaultCredName(cred) {
				return fmt.Errorf("vault rotate apply: invalid credential name %q", p.VaultRotate.Credential)
			}
			if err := a.rotate(req.AgentGroupID, cred); err != nil {
				return fmt.Errorf("vault rotate apply: %w", err)
			}
		}
	}
	if a.next != nil {
		return a.next.Apply(ctx, req, d)
	}
	return nil
}

// VaultRotateVerifier rejects a malformed rotation change before it ever reaches a
// human, and marks a well-formed one as requiring human approval (rotating a
// credential is privileged — it is never auto-approved). It is deterministic (a pure
// shape check, no I/O) and additive: a change with no vaultRotate body passes through
// untouched.
type VaultRotateVerifier struct{}

// Name identifies the verifier.
func (VaultRotateVerifier) Name() string { return "vault-rotate" }

// Verify checks a rotation change's shape. Non-rotation changes pass through.
func (VaultRotateVerifier) Verify(ctx context.Context, req contract.ChangeRequest) (contract.Verdict, string, error) {
	if len(req.After) == 0 {
		return contract.VerdictPass, "", nil
	}
	var p vaultRotatePayload
	if err := json.Unmarshal(req.After, &p); err != nil || p.VaultRotate == nil {
		// Not a rotation change (or an unrelated payload); nothing to say.
		return contract.VerdictPass, "", nil
	}
	if strings.TrimSpace(string(req.AgentGroupID)) == "" {
		return contract.VerdictReject, "vault rotate change must target an agent group", nil
	}
	if !validVaultCredName(p.VaultRotate.Credential) {
		return contract.VerdictReject, fmt.Sprintf("invalid vault credential name %q", p.VaultRotate.Credential), nil
	}
	return contract.VerdictRequireHuman, "vault credential rotation requires human approval", nil
}
