package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// Vault-policy gateway integration. "Which agent group may use which credential
// against which host" (threat-model §11) is host-side authorization CONFIG, never a
// secret — every rule names a credential, never holds one. A policy change is a
// capability change like any other: it rides the gateway's mandatory human-approval
// floor and is materialized only after a human approves it.
//
// The change carries a vaultPolicy payload on an existing permissions-class change
// (no frozen-contract edit): the After body names the per-group rules and the target
// group is taken from the trusted ChangeRequest.AgentGroupID — never the payload — so
// a change can only ever set policy for the group it is submitted against. The
// applier records the approved policy in the host-side VaultPolicyStore (read-only to
// the sandbox); the broker/injector consult that store before a credential is used.

// VaultRule is the gateway-local shape of one approved grant: a logical credential
// NAME (never a key) and the bare upstream hosts it may be used against. It mirrors
// registry.VaultRule but is defined here so the gateway stays decoupled from the
// registry package (the EgressApplier/Allower pattern).
type VaultRule struct {
	Credential string   `json:"credential"`
	Hosts      []string `json:"hosts"`
}

// SetVaultPolicyFunc records an approved per-group vault policy host-side. Satisfied
// by registry.VaultPolicyStore.Set via a small adapter in cmd/controlplane; a seam so
// the gateway does not depend on the registry package.
type SetVaultPolicyFunc func(id contract.AgentGroupID, rules []VaultRule) error

// vaultPolicyPayload is the vault portion of a change's After body. A change with no
// vaultPolicy field is not a vault-policy change and passes through untouched.
type vaultPolicyPayload struct {
	VaultPolicy *struct {
		Rules []VaultRule `json:"rules"`
	} `json:"vaultPolicy"`
}

// VaultPolicyApplier materializes an approved vault-policy change by recording the
// per-group rules in the host-side store, so subsequent vaulted calls are authorized
// deny-by-default against exactly the approved (credential, host) pairs. It is
// kind-agnostic (it keys off the payload, like EgressApplier): any approved change
// carrying a vaultPolicy body sets that group's policy; every other payload and kind
// passes through to next unchanged.
type VaultPolicyApplier struct {
	set  SetVaultPolicyFunc
	next contract.Applier
}

// NewVaultPolicyApplier wraps next. set may be nil (a recognized vault-policy change
// then errors rather than silently dropping the grant); next may be nil.
func NewVaultPolicyApplier(set SetVaultPolicyFunc, next contract.Applier) *VaultPolicyApplier {
	return &VaultPolicyApplier{set: set, next: next}
}

// Apply records an approved vault policy, then delegates. The gateway only invokes
// Apply for approved changes, so reaching here means a human granted the policy.
func (a *VaultPolicyApplier) Apply(ctx context.Context, req contract.ChangeRequest, d contract.Decision) error {
	if len(req.After) > 0 {
		var p vaultPolicyPayload
		// Best-effort: a payload with no "vaultPolicy" field simply yields none.
		if err := json.Unmarshal(req.After, &p); err == nil && p.VaultPolicy != nil {
			if a.set == nil {
				return fmt.Errorf("vault policy apply: no policy setter wired")
			}
			if strings.TrimSpace(string(req.AgentGroupID)) == "" {
				return fmt.Errorf("vault policy apply: change has no agent group id")
			}
			if err := a.set(req.AgentGroupID, p.VaultPolicy.Rules); err != nil {
				return fmt.Errorf("vault policy apply: %w", err)
			}
		}
	}
	if a.next != nil {
		return a.next.Apply(ctx, req, d)
	}
	return nil
}

// VaultPolicyVerifier rejects a malformed vault-policy change before it ever reaches a
// human, and marks a well-formed one as requiring human approval (a credential grant
// is privileged — it is never auto-approved). It is deterministic (a pure shape check,
// no I/O) and additive like every verifier: a change with no vaultPolicy body passes
// through untouched. The authoritative normalization/validation still happens at apply
// time in registry.VaultPolicyStore.Set; this is the pre-approval guard so obvious
// mistakes or injection attempts are refused without bothering a human.
type VaultPolicyVerifier struct{}

// Name identifies the verifier.
func (VaultPolicyVerifier) Name() string { return "vault-policy" }

// Verify checks a vault-policy change's shape. Non-vault changes pass through.
func (VaultPolicyVerifier) Verify(ctx context.Context, req contract.ChangeRequest) (contract.Verdict, string, error) {
	if len(req.After) == 0 {
		return contract.VerdictPass, "", nil
	}
	var p vaultPolicyPayload
	if err := json.Unmarshal(req.After, &p); err != nil || p.VaultPolicy == nil {
		// Not a vault-policy change (or an unrelated payload); nothing to say.
		return contract.VerdictPass, "", nil
	}
	if strings.TrimSpace(string(req.AgentGroupID)) == "" {
		return contract.VerdictReject, "vault_policy change must target an agent group", nil
	}
	for _, r := range p.VaultPolicy.Rules {
		if !validVaultCredName(r.Credential) {
			return contract.VerdictReject, fmt.Sprintf("invalid vault credential name %q", r.Credential), nil
		}
		if len(r.Hosts) == 0 {
			return contract.VerdictReject, fmt.Sprintf("vault credential %q must grant at least one host", r.Credential), nil
		}
		for _, h := range r.Hosts {
			if !validVaultHostName(h) {
				return contract.VerdictReject, fmt.Sprintf("invalid vault host %q for credential %q", h, r.Credential), nil
			}
		}
	}
	return contract.VerdictRequireHuman, "vault policy grant requires human approval", nil
}

// validVaultCredName mirrors the egress broker / registry credential-name rule: a
// non-empty logical label (letters, digits, -, _, .) with no path traversal.
func validVaultCredName(s string) bool {
	if s == "" || len(s) > 128 || s == "." || s == ".." || strings.Contains(s, "..") {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.'
		if !ok {
			return false
		}
	}
	return true
}

// validVaultHostName accepts a bare hostname only: no scheme, port, path, or wildcard.
// The authoritative DNS-label check is registry.validVaultHost at apply time; this is
// a cheap pre-approval reject of the obviously-wrong (scheme/wildcard/port/space).
func validVaultHostName(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	if strings.ContainsAny(h, "*/:\\?#@ \t") {
		return false
	}
	return true
}
