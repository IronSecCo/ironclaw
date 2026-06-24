package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/gateway"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
)

// newVaultTestServer returns a Server with a vault store wired and a no-op gateway.
func newVaultTestServer(t *testing.T) (*Server, *registry.VaultPolicyStore) {
	t.Helper()
	gw := gateway.New(gateway.VerifierChain{}, gateway.NewManualApprover(), gateway.NewLogApplier(), gateway.NewMemoryStore())
	store := registry.NewVaultPolicyStore()
	s := New(gw).WithVault(store)
	return s, store
}

func TestVaultPolicyGet_DenyByDefaultWhenUnconfigured(t *testing.T) {
	s, _ := newVaultTestServer(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/vault/policy/group-a", nil)
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var got vaultPolicyView
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.DenyByDefault {
		t.Errorf("DenyByDefault = false, want true")
	}
	if got.HasPolicy {
		t.Errorf("HasPolicy = true for an unconfigured group, want false")
	}
	if len(got.Rules) != 0 {
		t.Errorf("Rules = %v, want empty", got.Rules)
	}
	if got.AgentGroupID != "group-a" {
		t.Errorf("AgentGroupID = %q, want group-a", got.AgentGroupID)
	}
}

func TestVaultPolicyGet_ReflectsApprovedGrants(t *testing.T) {
	s, store := newVaultTestServer(t)
	if err := store.Set(registry.VaultPolicy{
		AgentGroupID: "group-a",
		Rules:        []registry.VaultRule{{Credential: "github", Hosts: []string{"api.github.com"}}},
	}); err != nil {
		t.Fatalf("store.Set: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/vault/policy/group-a", nil)
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var got vaultPolicyView
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.HasPolicy {
		t.Errorf("HasPolicy = false, want true")
	}
	if len(got.Rules) != 1 || got.Rules[0].Credential != "github" {
		t.Fatalf("Rules = %+v, want one github rule", got.Rules)
	}
	if len(got.Rules[0].Hosts) != 1 || got.Rules[0].Hosts[0] != "api.github.com" {
		t.Errorf("Hosts = %v, want [api.github.com]", got.Rules[0].Hosts)
	}
}

// A read of one group must never leak another group's policy.
func TestVaultPolicyGet_IsolatedPerGroup(t *testing.T) {
	s, store := newVaultTestServer(t)
	_ = store.Set(registry.VaultPolicy{
		AgentGroupID: "group-a",
		Rules:        []registry.VaultRule{{Credential: "stripe", Hosts: []string{"api.stripe.com"}}},
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/vault/policy/group-b", nil)
	s.Handler().ServeHTTP(rr, req)

	var got vaultPolicyView
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got.HasPolicy || len(got.Rules) != 0 {
		t.Errorf("group-b leaked group-a policy: %+v", got)
	}
}

func TestVaultPolicyGet_DisabledWhenNoStore(t *testing.T) {
	gw := gateway.New(gateway.VerifierChain{}, gateway.NewManualApprover(), gateway.NewLogApplier(), gateway.NewMemoryStore())
	s := New(gw) // no WithVault
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/vault/policy/group-a", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
}

// Ensure the read model carries no field that could ever hold a secret value — it is
// credential NAMES + hosts only.
func TestVaultPolicyView_ShapeHasNoSecretField(t *testing.T) {
	b, _ := json.Marshal(vaultPolicyView{
		AgentGroupID:  "g",
		DenyByDefault: true,
		HasPolicy:     true,
		Rules:         []vaultRuleView{{Credential: "github", Hosts: []string{"api.github.com"}}},
	})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, banned := range []string{"secret", "key", "token", "value", "password"} {
		if _, ok := m[banned]; ok {
			t.Errorf("read model exposes a %q field", banned)
		}
	}
	_ = contract.AgentGroupID("") // keep contract import meaningful
}
