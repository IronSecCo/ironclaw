package modelproxy

import (
	"net/http"
	"strings"
)

// azureHostSuffix is the Azure OpenAI host suffix the injectors match. Azure OpenAI
// is served per-resource as {resource}.openai.azure.com, all ending in this suffix.
const azureHostSuffix = ".openai.azure.com"

// AzureKeyInjector returns an Injector that authenticates requests to Azure OpenAI
// ({resource}.openai.azure.com) with a host-held API key via the `api-key` header
// (e.g. from AZURE_OPENAI_API_KEY). The key lives only on the host and never enters
// the sandbox. It self-guards on the Azure host suffix so it no-ops for any other
// provider — safe to compose through MultiInjector. An empty key is a no-op.
func AzureKeyInjector(apiKey string) Injector {
	return func(upstreamHost string, req *http.Request) {
		if apiKey == "" {
			return
		}
		if !isAzureHost(upstreamHost) {
			return
		}
		req.Header.Set("api-key", apiKey)
	}
}

// AzureTokenInjector returns an Injector that authenticates requests to Azure OpenAI
// with a host-held Microsoft Entra ID (Azure AD) OAuth2 bearer token obtained from
// ts, for deployments configured for Entra auth instead of a static key. Like
// VertexInjector the token is short-lived and refreshed host-side by ts; the sandbox
// never holds it. A token-source error leaves the request unauthenticated (the
// upstream rejects with 401) rather than failing closed inside the proxy; the error
// is never logged here to avoid leaking token material. It self-guards on the Azure
// host suffix so it no-ops for any other provider — safe to compose through
// MultiInjector.
func AzureTokenInjector(ts TokenSource) Injector {
	return func(upstreamHost string, req *http.Request) {
		if ts == nil {
			return
		}
		if !isAzureHost(upstreamHost) {
			return
		}
		tok, err := ts.Token()
		if err != nil || tok == "" {
			return
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
}

// isAzureHost reports whether host is an Azure OpenAI host, tolerating a trailing
// :port.
func isAzureHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	return strings.HasSuffix(host, azureHostSuffix)
}
