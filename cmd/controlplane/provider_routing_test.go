package main

import (
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/isolation"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
)

// registerGroupSession registers an agent group pinned to provider/model and
// resolves a session bound to it via a per-group messaging group, returning the
// session id. Unlike newSessionFor it takes distinct ids so several providers can
// coexist in one registry — the shape needed to prove PER-GROUP routing.
func registerGroupSession(t *testing.T, reg *registry.MemRegistry, groupID contract.AgentGroupID, mgID, provider, model string) contract.SessionID {
	t.Helper()
	if err := reg.PutAgentGroup(registry.AgentGroup{ID: groupID, Name: string(groupID), Provider: provider, Model: model}); err != nil {
		t.Fatalf("PutAgentGroup(%s): %v", groupID, err)
	}
	s, err := reg.ResolveSession(groupID, contract.MessagingGroupID(mgID), nil, contract.SessionShared)
	if err != nil {
		t.Fatalf("ResolveSession(%s): %v", groupID, err)
	}
	return s.ID
}

// TestSelectModel_PerGroupMultiProvider proves per-group provider selection across
// the supported providers (Anthropic via the sealed default, OpenAI, OpenRouter,
// and Codex) when several groups coexist in one deployment: each session resolves
// to its OWN group's provider/model, never another group's. This is the core
// multi-provider routing guarantee.
func TestSelectModel_PerGroupMultiProvider(t *testing.T) {
	// No deployment default, so a provider-less group keeps the zero (Anthropic)
	// selection and we can assert the empty-provider case unambiguously.
	t.Setenv("IRONCLAW_DEV_PROVIDER", "")
	t.Setenv("IRONCLAW_DEV_MODEL", "")

	reg := registry.NewMemRegistry()
	type want struct{ provider, model string }
	groups := []struct {
		id       contract.AgentGroupID
		mg       string
		provider string
		model    string
		want     want
	}{
		{"g-anthropic", "mg-a", "", "", want{"", ""}},                                                // sealed Anthropic default
		{"g-openai", "mg-o", "openai", "gpt-4o", want{"openai", "gpt-4o"}},                           // pinned OpenAI
		{"g-openrouter", "mg-r", "openrouter", "openai/gpt-4o", want{"openrouter", "openai/gpt-4o"}}, // pinned OpenRouter
		{"g-codex", "mg-c", "codex", "gpt-5.5", want{"codex", "gpt-5.5"}},                            // pinned Codex
	}

	sel := selectModelFromRegistry(reg)
	ids := make(map[contract.AgentGroupID]contract.SessionID, len(groups))
	for _, g := range groups {
		ids[g.id] = registerGroupSession(t, reg, g.id, g.mg, g.provider, g.model)
	}
	// Resolve every session and assert it routes to its own group's selection. The
	// loop interleaves groups so a leaked global default or cross-group bleed fails.
	for _, g := range groups {
		got := sel(ids[g.id])
		if got.Provider != g.want.provider || got.Model != g.want.model {
			t.Fatalf("group %s selected %+v, want provider=%q model=%q", g.id, got, g.want.provider, g.want.model)
		}
	}
}

// TestSelectModel_FallbackLadder exercises the full selection fallback ladder for a
// provider-less group across providers: an explicit deployment default
// (IRONCLAW_DEV_PROVIDER) is inherited, and with no default the selection falls
// back to the zero value (the sealed Anthropic backend). NOTE: this is SELECTION
// fallback (group -> deployment default -> Anthropic), not runtime provider
// failover — there is deliberately no automatic "primary provider down, try
// secondary" path in the codebase today (a sustained provider outage trips the
// sandbox loop's circuit breaker instead). See IRO-5 report for the follow-up.
func TestSelectModel_FallbackLadder(t *testing.T) {
	cases := []struct {
		name        string
		devProvider string
		devModel    string
		wantP       string
		wantM       string
	}{
		{"inherit-openai-default", "openai", "gpt-4o", "openai", "gpt-4o"},
		{"inherit-openrouter-default", "openrouter", "openai/gpt-4o", "openrouter", "openai/gpt-4o"},
		{"inherit-codex-default", "codex", "gpt-5.5", "codex", "gpt-5.5"},
		{"no-default-keeps-anthropic", "", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("IRONCLAW_DEV_PROVIDER", c.devProvider)
			t.Setenv("IRONCLAW_DEV_MODEL", c.devModel)
			reg := registry.NewMemRegistry()
			// A provider-less group: selection must fall through to the deployment ladder.
			id := registerGroupSession(t, reg, "g1", "mg1", "", "")
			got := selectModelFromRegistry(reg)(id)
			if got.Provider != c.wantP || got.Model != c.wantM {
				t.Fatalf("ladder %s: got %+v, want provider=%q model=%q", c.name, got, c.wantP, c.wantM)
			}
		})
	}
}

// TestSelectModel_SelectionFlowsIntoOCISpec ties the routing decision to the launch
// boundary: a group's selected provider/model must surface as the sandbox process
// flags BuildOCISpec emits, for each non-default provider. This guards the seam
// between "which provider did routing pick" and "what the sandbox is actually told
// to use".
func TestSelectModel_SelectionFlowsIntoOCISpec(t *testing.T) {
	t.Setenv("IRONCLAW_DEV_PROVIDER", "")
	t.Setenv("IRONCLAW_DEV_MODEL", "")
	cases := []struct {
		provider string
		model    string
		host     string
	}{
		{"openai", "gpt-4o", "api.openai.com"},
		{"openrouter", "openai/gpt-4o", "openrouter.ai"},
	}
	for _, c := range cases {
		t.Run(c.provider, func(t *testing.T) {
			reg := registry.NewMemRegistry()
			id := registerGroupSession(t, reg, contract.AgentGroupID("g-"+c.provider), "mg-"+c.provider, c.provider, c.model)
			sel := selectModelFromRegistry(reg)(id)
			if sel.Provider != c.provider || sel.Model != c.model {
				t.Fatalf("selection = %+v, want provider=%q model=%q", sel, c.provider, c.model)
			}

			spec := isolation.HardenedSpec(contract.SessionID("ses_"+c.provider), "img", "/in.db", "/out.db", "/run/ironclaw/modelproxy.sock")
			spec.ModelProvider = sel.Provider
			spec.ModelID = sel.Model
			spec.ModelHost = c.host
			oci, err := isolation.BuildOCISpec(spec)
			if err != nil {
				t.Fatalf("BuildOCISpec: %v", err)
			}
			want := []string{"/sandbox", "--provider", c.provider, "--model", c.model, "--model-host", c.host}
			got := oci.Process.Args
			if len(got) != len(want) {
				t.Fatalf("args = %v, want %v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("args = %v, want %v", got, want)
				}
			}
		})
	}
}
