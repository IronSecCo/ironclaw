package main

import (
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
)

// newSessionFor registers an agent group with the given provider/model and returns
// the id of a fresh session wired to it.
func newSessionFor(t *testing.T, reg *registry.MemRegistry, provider, model string) contract.SessionID {
	t.Helper()
	const groupID contract.AgentGroupID = "g1"
	if err := reg.PutAgentGroup(registry.AgentGroup{ID: groupID, Name: "g1", Provider: provider, Model: model}); err != nil {
		t.Fatalf("PutAgentGroup: %v", err)
	}
	s, err := reg.ResolveSession(groupID, "mg1", nil, contract.SessionShared)
	if err != nil {
		t.Fatalf("ResolveSession: %v", err)
	}
	return s.ID
}

// A group pinned to an explicit provider uses it, regardless of the deployment
// default — existing per-group selection is unchanged.
func TestSelectModel_ExplicitProviderWins(t *testing.T) {
	t.Setenv("IRONCLAW_DEV_PROVIDER", "codex")
	t.Setenv("IRONCLAW_DEV_MODEL", "gpt-5.5")
	reg := registry.NewMemRegistry()
	id := newSessionFor(t, reg, "openai", "gpt-4o")

	sel := selectModelFromRegistry(reg)(id)
	if sel.Provider != "openai" || sel.Model != "gpt-4o" {
		t.Fatalf("explicit group provider must win: got %+v", sel)
	}
}

// Regression for the gateway-only 403: a group with NO provider must inherit the
// deployment default (IRONCLAW_DEV_PROVIDER) rather than falling back to Anthropic,
// whose host is not on the gateway-only allowlist.
func TestSelectModel_GroupWithoutProviderInheritsDevDefault(t *testing.T) {
	t.Setenv("IRONCLAW_DEV_PROVIDER", "codex")
	t.Setenv("IRONCLAW_DEV_MODEL", "gpt-5.5")
	reg := registry.NewMemRegistry()
	id := newSessionFor(t, reg, "", "")

	sel := selectModelFromRegistry(reg)(id)
	if sel.Provider != "codex" || sel.Model != "gpt-5.5" {
		t.Fatalf("group without provider must inherit dev default: got %+v", sel)
	}
}

// An unresolved session (not yet in the registry) also gets the deployment default,
// so the very first turn does not transiently fall back to an unallowlisted host.
func TestSelectModel_UnknownSessionGetsDevDefault(t *testing.T) {
	t.Setenv("IRONCLAW_DEV_PROVIDER", "codex")
	t.Setenv("IRONCLAW_DEV_MODEL", "gpt-5.5")
	reg := registry.NewMemRegistry()

	sel := selectModelFromRegistry(reg)("ses_does_not_exist")
	if sel.Provider != "codex" || sel.Model != "gpt-5.5" {
		t.Fatalf("unknown session must get dev default: got %+v", sel)
	}
}

// With no deployment default configured, a provider-less group yields the zero
// selection (the built-in Anthropic backend) — the original, unchanged behavior.
func TestSelectModel_NoDevDefaultKeepsAnthropic(t *testing.T) {
	t.Setenv("IRONCLAW_DEV_PROVIDER", "")
	t.Setenv("IRONCLAW_DEV_MODEL", "")
	reg := registry.NewMemRegistry()
	id := newSessionFor(t, reg, "", "")

	sel := selectModelFromRegistry(reg)(id)
	if sel.Provider != "" || sel.Model != "" {
		t.Fatalf("no dev default must keep the zero selection: got %+v", sel)
	}
}
