// OWNER: T-260c (credential vault — per-group vault policy)

package registry

// Per-group vault policy: "which agent group may use which credential against which
// host." It is host-side authorization CONFIG, not a secret — every rule names a
// credential, never holds one (.agents/spikes/credential-vault.md §5/§6.1). Because
// the registry is host-internal (the sandbox never sees it), this store is read-only
// to the sandbox by construction; the only mutators (Set/Delete) are invoked from
// the gateway apply path after a human-approved change, so a policy change is a
// gateway-gated capability change like any other — never sandbox-settable.
//
// This file is the policy model + decision (T-260c). Persisting it durably and
// wiring Set into the gateway apply step are the follow-on integration (the main.go
// owner's job); this unit is standalone and fully tested.

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// VaultRule grants one agent group the use of a single logical credential against a
// fixed set of upstream hosts. Deny-by-default: a host not listed is refused.
type VaultRule struct {
	// Credential is the logical credential NAME (e.g. "github"), never a key.
	Credential string
	// Hosts are the bare upstream hostnames the credential may be used against. No
	// wildcards — every host is explicit and human-approved.
	Hosts []string
}

// VaultPolicy is one agent group's complete set of vault grants. The zero value (no
// rules) denies everything — the safe default for a group with no vault access.
type VaultPolicy struct {
	AgentGroupID contract.AgentGroupID
	Rules        []VaultRule
}

// VaultPolicyStore holds per-group vault policy host-side and answers the
// authorization decision. Mutation is gateway-apply only; reads (Allows/Get) are
// what the broker/injector consult before a credential is used.
type VaultPolicyStore struct {
	mu      sync.RWMutex
	byGroup map[contract.AgentGroupID]VaultPolicy
}

// NewVaultPolicyStore returns an empty store: every group denied (deny-by-default).
func NewVaultPolicyStore() *VaultPolicyStore {
	return &VaultPolicyStore{byGroup: make(map[contract.AgentGroupID]VaultPolicy)}
}

// Set installs or replaces a group's policy after validating it. On an invalid
// group id, credential name, or host it returns an error and leaves the store
// unchanged. Call site: the gateway apply step for an approved vault-policy change.
func (s *VaultPolicyStore) Set(p VaultPolicy) error {
	if err := p.validate(); err != nil {
		return err
	}
	norm := p.normalized()
	s.mu.Lock()
	s.byGroup[p.AgentGroupID] = norm
	s.mu.Unlock()
	return nil
}

// Delete removes a group's policy (idempotent). Gateway-apply only.
func (s *VaultPolicyStore) Delete(group contract.AgentGroupID) {
	s.mu.Lock()
	delete(s.byGroup, group)
	s.mu.Unlock()
}

// Get returns a group's stored (normalized) policy.
func (s *VaultPolicyStore) Get(group contract.AgentGroupID) (VaultPolicy, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.byGroup[group]
	return p, ok
}

// Allows reports whether group may use credential against host. Deny-by-default:
// false unless the group has a rule for exactly that credential whose host list
// includes host. host may carry a :port, which is ignored for the match.
func (s *VaultPolicyStore) Allows(group contract.AgentGroupID, credential, host string) bool {
	cred := strings.ToLower(strings.TrimSpace(credential))
	h := vaultHostKey(host)
	if cred == "" || h == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.byGroup[group]
	if !ok {
		return false
	}
	for _, r := range p.Rules {
		if r.Credential != cred {
			continue
		}
		for _, allowed := range r.Hosts {
			if allowed == h {
				return true
			}
		}
	}
	return false
}

func (p VaultPolicy) validate() error {
	if strings.TrimSpace(string(p.AgentGroupID)) == "" {
		return fmt.Errorf("registry: vault policy requires an agent group id")
	}
	for _, r := range p.Rules {
		if !validVaultCred(r.Credential) {
			return fmt.Errorf("registry: invalid vault credential name %q", r.Credential)
		}
		if len(r.Hosts) == 0 {
			return fmt.Errorf("registry: vault credential %q must grant at least one host", r.Credential)
		}
		for _, h := range r.Hosts {
			if !validVaultHost(h) {
				return fmt.Errorf("registry: invalid vault host %q for credential %q", h, r.Credential)
			}
		}
	}
	return nil
}

// normalized lowercases and trims credential names and hosts so stored policy
// compares stably against a lowercased query.
func (p VaultPolicy) normalized() VaultPolicy {
	out := VaultPolicy{AgentGroupID: p.AgentGroupID, Rules: make([]VaultRule, 0, len(p.Rules))}
	for _, r := range p.Rules {
		nr := VaultRule{Credential: strings.ToLower(strings.TrimSpace(r.Credential))}
		for _, h := range r.Hosts {
			nr.Hosts = append(nr.Hosts, vaultHostKey(h))
		}
		out.Rules = append(out.Rules, nr)
	}
	return out
}

// vaultHostKey lowercases host and strips any :port.
func vaultHostKey(host string) string {
	host = strings.TrimSpace(host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(host)
}

// validVaultCred mirrors the egress broker's credential-name rule: a non-empty
// logical label (letters, digits, -, _, .) with no path traversal.
func validVaultCred(s string) bool {
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

// validVaultHost accepts a bare hostname only: no scheme, port, path, or wildcard,
// with well-formed DNS labels — what becomes an approved upstream for a credential.
func validVaultHost(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	if strings.ContainsAny(h, "*/:\\?#@ \t") {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
	}
	return true
}
