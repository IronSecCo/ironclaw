package registry

import (
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
)

const (
	grpA contract.AgentGroupID = "group-a"
	grpB contract.AgentGroupID = "group-b"
)

func TestVaultPolicyDenyByDefault(t *testing.T) {
	s := NewVaultPolicyStore()
	if s.Allows(grpA, "github", "api.github.com") {
		t.Fatal("empty store must deny by default")
	}
	// A group with a policy but no matching rule still denies.
	if err := s.Set(VaultPolicy{AgentGroupID: grpA, Rules: []VaultRule{{Credential: "github", Hosts: []string{"api.github.com"}}}}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if s.Allows(grpA, "stripe", "api.github.com") {
		t.Fatal("a credential with no rule must be denied")
	}
}

func TestVaultPolicySetAndAllows(t *testing.T) {
	s := NewVaultPolicyStore()
	err := s.Set(VaultPolicy{AgentGroupID: grpA, Rules: []VaultRule{
		{Credential: "github", Hosts: []string{"api.github.com"}},
		{Credential: "pagerduty", Hosts: []string{"api.pagerduty.com", "events.pagerduty.com"}},
	}})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}

	if !s.Allows(grpA, "github", "api.github.com") {
		t.Error("granted credential+host must be allowed")
	}
	if !s.Allows(grpA, "pagerduty", "events.pagerduty.com") {
		t.Error("second host in a rule must be allowed")
	}
	if s.Allows(grpA, "github", "evil.example.com") {
		t.Error("host not listed for the credential must be denied")
	}
	if s.Allows(grpB, "github", "api.github.com") {
		t.Error("a different group must not inherit grpA's policy")
	}
}

func TestVaultPolicyHostPortIgnored(t *testing.T) {
	s := NewVaultPolicyStore()
	_ = s.Set(VaultPolicy{AgentGroupID: grpA, Rules: []VaultRule{{Credential: "github", Hosts: []string{"api.github.com"}}}})
	if !s.Allows(grpA, "github", "api.github.com:443") {
		t.Fatal("a :port on the queried host must be ignored for the match")
	}
}

func TestVaultPolicyCaseInsensitive(t *testing.T) {
	s := NewVaultPolicyStore()
	_ = s.Set(VaultPolicy{AgentGroupID: grpA, Rules: []VaultRule{{Credential: "GitHub", Hosts: []string{"API.GitHub.com"}}}})
	if !s.Allows(grpA, "github", "api.github.com") {
		t.Fatal("credential and host matching must be case-insensitive")
	}
}

func TestVaultPolicySetValidation(t *testing.T) {
	s := NewVaultPolicyStore()
	bad := []VaultPolicy{
		{AgentGroupID: "", Rules: []VaultRule{{Credential: "github", Hosts: []string{"api.github.com"}}}},      // no group id
		{AgentGroupID: grpA, Rules: []VaultRule{{Credential: "../etc", Hosts: []string{"api.github.com"}}}},    // bad cred
		{AgentGroupID: grpA, Rules: []VaultRule{{Credential: "github", Hosts: nil}}},                           // no hosts
		{AgentGroupID: grpA, Rules: []VaultRule{{Credential: "github", Hosts: []string{"https://api.x.com"}}}}, // scheme in host
		{AgentGroupID: grpA, Rules: []VaultRule{{Credential: "github", Hosts: []string{"*.github.com"}}}},      // wildcard
		{AgentGroupID: grpA, Rules: []VaultRule{{Credential: "github", Hosts: []string{"api.x.com:443"}}}},     // port in host
	}
	for i, p := range bad {
		if err := s.Set(p); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
	// A rejected Set must not have stored anything.
	if _, ok := s.Get(grpA); ok {
		t.Fatal("a rejected Set must leave the store unchanged")
	}
}

func TestVaultPolicyDeleteAndGet(t *testing.T) {
	s := NewVaultPolicyStore()
	_ = s.Set(VaultPolicy{AgentGroupID: grpA, Rules: []VaultRule{{Credential: "github", Hosts: []string{"api.github.com"}}}})

	p, ok := s.Get(grpA)
	if !ok || len(p.Rules) != 1 || p.Rules[0].Credential != "github" {
		t.Fatalf("Get returned unexpected policy: %+v ok=%v", p, ok)
	}

	s.Delete(grpA)
	if _, ok := s.Get(grpA); ok {
		t.Fatal("Get after Delete must report absent")
	}
	if s.Allows(grpA, "github", "api.github.com") {
		t.Fatal("Allows after Delete must deny")
	}
	s.Delete(grpA) // idempotent
}
