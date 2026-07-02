// This file adds Azure OpenAI (Azure AI Foundry) as a first-class provider so orgs
// that can consume models only through Azure — a very common enterprise constraint —
// can run IronClaw. Azure OpenAI speaks the IDENTICAL OpenAI Chat Completions wire
// format ("messages"/tools out, choices back), so this file does NOT fork the
// translation or streaming logic: NewAzure returns a *OpenAIProvider whose only
// difference is the request URL. Two things differ from the OpenAI path, and both
// live in the transport envelope rather than the body:
//
//   - URL: the model is selected by a DEPLOYMENT NAME in the path and an api-version
//     query param —
//     /openai/deployments/{deployment}/chat/completions?api-version={version}
//     served from the per-resource {resource}.openai.azure.com host. Azure ignores
//     the body's "model" field (the deployment picks the model), so cfg.Model carries
//     the deployment name.
//   - Auth: the `api-key` header (or a Microsoft Entra ID Bearer token), injected
//     HOST-SIDE by modelproxy.AzureKeyInjector / AzureTokenInjector — NOT the Bearer
//     key the OpenAI path uses. As with every backend the sandbox holds no credential
//     and dials only the host model-proxy unix socket; the host proxy authenticates
//     and enforces the {resource}.openai.azure.com egress allowlist.

package provider

import (
	"fmt"
	"strings"
)

// defaultAzureAPIVersion is the Azure OpenAI api-version applied when cfg.APIVersion
// is empty. It targets a current GA data-plane version; operators pass a different
// one via --model-api-version (or AZURE_OPENAI_API_VERSION host-side) when their
// deployment requires it.
const defaultAzureAPIVersion = "2024-10-21"

// NewAzure constructs an Azure OpenAI backend, reusing OpenAIProvider unchanged (the
// wire format is identical) and overriding only the request URL so the deployment
// name and api-version ride in it. Unlike the single-global-host cloud providers,
// Azure is per-resource and the deployment selects the model, so there is no safe
// default host or model: NewAzure requires cfg.UpstreamHost
// ({resource}.openai.azure.com; the control-plane backfills it from
// AZURE_OPENAI_ENDPOINT) and cfg.Model (the Azure deployment name). The api-key /
// Entra token is added host-side by the model-proxy injector — this provider never
// holds a credential. Callers usually go through New.
func NewAzure(cfg Config) (*OpenAIProvider, error) {
	if cfg.UpstreamHost == "" {
		return nil, fmt.Errorf("sandbox/provider: azure provider requires an upstream host (set --model-host, e.g. my-resource.openai.azure.com)")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("sandbox/provider: azure provider requires a deployment name (set --model to the Azure deployment)")
	}
	if cfg.SocketPath == "" {
		cfg.SocketPath = DefaultSocketPath
	}
	if cfg.APIVersion == "" {
		cfg.APIVersion = defaultAzureAPIVersion
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = defaultMaxTokens
	}
	if cfg.HTTPTimeout == 0 {
		cfg.HTTPTimeout = defaultHTTPTimeout
	}

	return &OpenAIProvider{
		cfg:    cfg,
		client: newSocketClient(cfg.SocketPath, cfg.HTTPTimeout),
		url:    azureURL(cfg.UpstreamHost, cfg.Model, cfg.APIVersion),
	}, nil
}

// azureURL builds the Azure OpenAI Chat Completions endpoint: the deployment name
// rides in the path and the api-version in the query. The scheme is plain http to
// the unix socket — the host proxy upgrades to https upstream.
func azureURL(host, deployment, apiVersion string) string {
	var b strings.Builder
	b.WriteString("http://")
	b.WriteString(host)
	b.WriteString("/openai/deployments/")
	b.WriteString(deployment)
	b.WriteString("/chat/completions?api-version=")
	b.WriteString(apiVersion)
	return b.String()
}
