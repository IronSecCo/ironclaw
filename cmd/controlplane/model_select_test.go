package main

import (
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
	"github.com/IronSecCo/ironclaw/internal/host/session"
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

	sel := selectModelFromRegistry(reg, session.ModelSelection{}, "", "", "")(id)
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

	sel := selectModelFromRegistry(reg, session.ModelSelection{}, "", "", "")(id)
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

	sel := selectModelFromRegistry(reg, session.ModelSelection{}, "", "", "")("ses_does_not_exist")
	if sel.Provider != "codex" || sel.Model != "gpt-5.5" {
		t.Fatalf("unknown session must get dev default: got %+v", sel)
	}
}

// A group pinned to the vertex provider threads its project + location through the
// selection so they reach the sandbox URL path.
func TestSelectModel_VertexThreadsProjectLocation(t *testing.T) {
	reg := registry.NewMemRegistry()
	const groupID contract.AgentGroupID = "gv"
	if err := reg.PutAgentGroup(registry.AgentGroup{
		ID: groupID, Name: "gv", Provider: "vertex", Model: "gemini-2.5-pro",
		Project: "my-proj", Location: "europe-west4",
	}); err != nil {
		t.Fatalf("PutAgentGroup: %v", err)
	}
	s, err := reg.ResolveSession(groupID, "mg1", nil, contract.SessionShared)
	if err != nil {
		t.Fatalf("ResolveSession: %v", err)
	}
	sel := selectModelFromRegistry(reg, session.ModelSelection{}, "", "", "")(s.ID)
	if sel.Provider != "vertex" || sel.Project != "my-proj" || sel.Location != "europe-west4" {
		t.Fatalf("vertex selection must carry project+location: got %+v", sel)
	}
}

// A group pinned to azure but carrying no host / api-version of its own inherits the
// deployment's per-resource Azure host and configured api-version (which is what the
// proxy allowlisted and the sandbox builds the URL from).
func TestSelectModel_AzureInheritsHostAndAPIVersion(t *testing.T) {
	reg := registry.NewMemRegistry()
	id := newSessionFor(t, reg, "azure", "gpt-4o")

	sel := selectModelFromRegistry(reg, session.ModelSelection{}, "", "my-resource.openai.azure.com", "2024-10-21")(id)
	if sel.Provider != "azure" || sel.Model != "gpt-4o" {
		t.Fatalf("azure selection must carry provider+deployment: got %+v", sel)
	}
	if sel.Host != "my-resource.openai.azure.com" || sel.APIVersion != "2024-10-21" {
		t.Fatalf("azure group must inherit the deployment host+api-version: got %+v", sel)
	}
}

// A group pinned to azure WITH its own host / api-version keeps them (the deployment
// defaults do not clobber an explicit per-group value).
func TestSelectModel_AzureGroupOwnValuesWin(t *testing.T) {
	reg := registry.NewMemRegistry()
	const groupID contract.AgentGroupID = "gaz"
	if err := reg.PutAgentGroup(registry.AgentGroup{
		ID: groupID, Name: "gaz", Provider: "azure", Model: "gpt-4o",
		APIVersion: "2025-01-01-preview",
	}); err != nil {
		t.Fatalf("PutAgentGroup: %v", err)
	}
	s, err := reg.ResolveSession(groupID, "mg1", nil, contract.SessionShared)
	if err != nil {
		t.Fatalf("ResolveSession: %v", err)
	}
	sel := selectModelFromRegistry(reg, session.ModelSelection{}, "", "deploy-host.openai.azure.com", "2024-10-21")(s.ID)
	if sel.APIVersion != "2025-01-01-preview" {
		t.Fatalf("explicit group api-version must win: got %+v", sel)
	}
}

// azureEndpointHost accepts a full URL or a bare host and rejects non-Azure hosts so
// a misconfigured endpoint cannot widen egress.
func TestAzureEndpointHost(t *testing.T) {
	cases := map[string]string{
		"https://my-resource.openai.azure.com":        "my-resource.openai.azure.com",
		"https://my-resource.openai.azure.com/":       "my-resource.openai.azure.com",
		"https://My-Resource.OpenAI.Azure.com/openai": "my-resource.openai.azure.com",
		"my-resource.openai.azure.com":                "my-resource.openai.azure.com",
		"my-resource.openai.azure.com:443":            "my-resource.openai.azure.com",
		"https://evil.example.com":                    "", // not an azure host
		"api.openai.com":                              "",
		"":                                            "",
	}
	for in, want := range cases {
		if got := azureEndpointHost(in); got != want {
			t.Errorf("azureEndpointHost(%q) = %q, want %q", in, got, want)
		}
	}
}

// vertexAllowHost must match the host the sandbox provider addresses, including the
// default-region and global cases, so the model-proxy allowlist does not 403.
func TestVertexAllowHost(t *testing.T) {
	cases := map[string]string{
		"":             "us-central1-aiplatform.googleapis.com", // default region
		"global":       "aiplatform.googleapis.com",
		"us-central1":  "us-central1-aiplatform.googleapis.com",
		"europe-west4": "europe-west4-aiplatform.googleapis.com",
		"  asia-east1": "asia-east1-aiplatform.googleapis.com", // trimmed
	}
	for in, want := range cases {
		if got := vertexAllowHost(in); got != want {
			t.Errorf("vertexAllowHost(%q) = %q, want %q", in, got, want)
		}
	}
}

// When a local-model default is configured (--local-model-url), it overrides the
// env-based dev default so a provider-less group runs 100% local — provider, model,
// and the loopback host all flow through.
func TestSelectModel_LocalDefaultOverridesDevDefault(t *testing.T) {
	t.Setenv("IRONCLAW_DEV_PROVIDER", "codex")
	t.Setenv("IRONCLAW_DEV_MODEL", "gpt-5.5")
	reg := registry.NewMemRegistry()
	id := newSessionFor(t, reg, "", "")

	localDef := session.ModelSelection{Provider: "local", Model: "llama3.2", Host: "localhost:11434"}
	sel := selectModelFromRegistry(reg, localDef, "localhost:11434", "", "")(id)
	if sel.Provider != "local" || sel.Model != "llama3.2" || sel.Host != "localhost:11434" {
		t.Fatalf("local default must override dev default with host: got %+v", sel)
	}
}

// A group explicitly pinned to the local provider but carrying no host of its own
// inherits the deployment's configured loopback host.
func TestSelectModel_LocalGroupInheritsHost(t *testing.T) {
	reg := registry.NewMemRegistry()
	id := newSessionFor(t, reg, "local", "llama3.2")

	sel := selectModelFromRegistry(reg, session.ModelSelection{}, "127.0.0.1:11434", "", "")(id)
	if sel.Provider != "local" || sel.Host != "127.0.0.1:11434" {
		t.Fatalf("local group must inherit the configured loopback host: got %+v", sel)
	}
}

// With no deployment default configured, a provider-less group yields the zero
// selection (the built-in Anthropic backend) — the original, unchanged behavior.
func TestSelectModel_NoDevDefaultKeepsAnthropic(t *testing.T) {
	t.Setenv("IRONCLAW_DEV_PROVIDER", "")
	t.Setenv("IRONCLAW_DEV_MODEL", "")
	reg := registry.NewMemRegistry()
	id := newSessionFor(t, reg, "", "")

	sel := selectModelFromRegistry(reg, session.ModelSelection{}, "", "", "")(id)
	if sel.Provider != "" || sel.Model != "" {
		t.Fatalf("no dev default must keep the zero selection: got %+v", sel)
	}
}
