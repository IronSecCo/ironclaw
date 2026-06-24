package api

import (
	"net/http"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
)

// Vault credential-management read surface. The per-group vault policy — "which
// agent group may use which logical credential against which host" (threat-model
// §11) — is host-side authorization CONFIG, never a secret: every rule names a
// credential, never holds one. This endpoint exposes that config read-only so an
// operator (console or `ironctl vault list`) can see the deny-by-default state and a
// group's active grants before proposing a change.
//
// MUTATION is deliberately NOT here. A policy change is a capability change like any
// other: clients propose it through the gateway's human-approval floor by submitting
// a permissions-class change carrying a `vaultPolicy` body (POST /v1/ui/config/change
// or POST /v1/changes), which the gateway's VaultPolicyVerifier + VaultPolicyApplier
// validate, human-approve, and materialize. This read endpoint can therefore never be
// a back-channel that sets policy — it only reflects what the gateway has approved.
//
// No secret is ever returned: the store holds credential NAMES and host allowlists,
// not keys (the real credential lives only in the host-side injector — see
// internal/host/egress/vault.go).

// VaultPolicyReader is the read-only view the API needs over the vault policy
// store. The read surface only ever calls Get, so accepting the interface lets
// either the in-memory *registry.VaultPolicyStore or the durable
// *registry.DurableVaultPolicyStore back it — both satisfy this — without the
// wiring in cmd/controlplane caring which one is configured.
type VaultPolicyReader interface {
	Get(contract.AgentGroupID) (registry.VaultPolicy, bool)
}

// WithVault attaches the host-side per-group vault policy store that backs the
// GET /v1/vault/policy read surface. nil (the default) leaves the read surface
// disabled (503), mirroring the other opt-in subsystems.
func (s *Server) WithVault(store VaultPolicyReader) *Server {
	s.vault = store
	return s
}

func (s *Server) vaultRoutes() {
	s.mux.HandleFunc("GET /v1/vault/policy/{agentGroupId}", s.handleVaultPolicyGet)
}

// vaultRuleView is one approved grant in the read model: a logical credential NAME
// (never a key) and the bare upstream hosts it may be used against.
type vaultRuleView struct {
	Credential string   `json:"credential"`
	Hosts      []string `json:"hosts"`
}

// vaultPolicyView is a group's complete vault authorization state. DenyByDefault is
// always true — it is the invariant the store enforces — and is surfaced explicitly
// so the UI can show "deny-by-default; these are the only grants". HasPolicy
// distinguishes "group has an (empty) policy record" from "group is unconfigured";
// both deny everything not listed.
type vaultPolicyView struct {
	AgentGroupID  string          `json:"agentGroupId"`
	DenyByDefault bool            `json:"denyByDefault"`
	HasPolicy     bool            `json:"hasPolicy"`
	Rules         []vaultRuleView `json:"rules"`
}

// handleVaultPolicyGet returns a group's current vault policy (deny-by-default state
// + active grants). It is read-only and returns no secret. With no vault store wired
// it returns 503; an unknown group returns a well-formed empty (deny-everything)
// policy rather than 404, so the UI can render "no grants yet" uniformly.
func (s *Server) handleVaultPolicyGet(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil {
		http.Error(w, "the vault is not enabled on this control-plane", http.StatusServiceUnavailable)
		return
	}
	id := contract.AgentGroupID(r.PathValue("agentGroupId"))
	if id == "" {
		http.Error(w, "agentGroupId is required", http.StatusBadRequest)
		return
	}
	view := vaultPolicyView{
		AgentGroupID:  string(id),
		DenyByDefault: true,
		Rules:         []vaultRuleView{},
	}
	if p, ok := s.vault.Get(id); ok {
		view.HasPolicy = true
		for _, rule := range p.Rules {
			hosts := append([]string{}, rule.Hosts...)
			view.Rules = append(view.Rules, vaultRuleView{Credential: rule.Credential, Hosts: hosts})
		}
	}
	writeJSON(w, http.StatusOK, view)
}
