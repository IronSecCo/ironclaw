package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestAzureQuerySuccess checks the Azure OpenAI transport envelope against a
// known-good vector: the deployment name rides in the path, the api-version rides in
// the query, the host is the per-resource *.openai.azure.com endpoint, and the
// body/response reuse the OpenAI Chat Completions wire format unchanged. It also
// asserts the sandbox sends NO credential header — auth is added host-side.
func TestAzureQuerySuccess(t *testing.T) {
	var gotBody []byte
	var gotPath, gotRawQuery, gotHost, gotAPIKey, gotAuth string
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRawQuery = r.URL.RawQuery
		gotHost = r.Host
		gotAPIKey = r.Header.Get("api-key")
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		writeSSE(w, chatHelloStream) // identical wire format to OpenAI
	}))

	p, err := NewAzure(Config{
		SocketPath:   sock,
		UpstreamHost: "my-resource.openai.azure.com",
		Model:        "gpt-4o", // Azure deployment name
	})
	if err != nil {
		t.Fatalf("NewAzure: %v", err)
	}
	out, err := p.Query(context.Background(), "hi there")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if out != "hello world" {
		t.Fatalf("Query output = %q, want %q", out, "hello world")
	}

	// Known-good vector: deployment in path, api-version in query, chat/completions.
	const wantPath = "/openai/deployments/gpt-4o/chat/completions"
	if gotPath != wantPath {
		t.Fatalf("path = %q, want %q", gotPath, wantPath)
	}
	wantQuery := "api-version=" + defaultAzureAPIVersion
	if gotRawQuery != wantQuery {
		t.Fatalf("query = %q, want %q", gotRawQuery, wantQuery)
	}
	if gotHost != "my-resource.openai.azure.com" {
		t.Fatalf("Host = %q, want the per-resource azure host (proxy allowlists on Host)", gotHost)
	}
	// The sandbox holds no credential: neither the api-key header nor Authorization
	// is set here — the host injector stamps auth on the way upstream.
	if gotAPIKey != "" || gotAuth != "" {
		t.Fatalf("sandbox sent credentials (api-key=%q auth=%q), want none", gotAPIKey, gotAuth)
	}

	// Body is the OpenAI chat shape (reused translation): one user message, streamed.
	var req oaiChatRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if !req.Stream {
		t.Fatal("stream = false, want true")
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" || req.Messages[0].Content != "hi there" {
		t.Fatalf("messages = %+v, want one user message %q", req.Messages, "hi there")
	}
}

// TestAzureURLWithExplicitAPIVersion checks a caller-supplied api-version overrides
// the default and rides in the query verbatim.
func TestAzureURLWithExplicitAPIVersion(t *testing.T) {
	p, err := NewAzure(Config{
		SocketPath:   "/x.sock",
		UpstreamHost: "acme.openai.azure.com",
		Model:        "my-deploy",
		APIVersion:   "2025-01-01-preview",
	})
	if err != nil {
		t.Fatalf("NewAzure: %v", err)
	}
	want := "http://acme.openai.azure.com/openai/deployments/my-deploy/chat/completions?api-version=2025-01-01-preview"
	if p.url != want {
		t.Fatalf("url = %q, want %q", p.url, want)
	}
}

// TestAzureRequiresHostAndDeployment checks Azure fails closed when the per-resource
// host or the deployment name is missing (there is no safe default for either).
func TestAzureRequiresHostAndDeployment(t *testing.T) {
	if _, err := NewAzure(Config{SocketPath: "/x.sock", Model: "gpt-4o"}); err == nil {
		t.Fatal("NewAzure with no host: want error, got nil")
	}
	if _, err := NewAzure(Config{SocketPath: "/x.sock", UpstreamHost: "acme.openai.azure.com"}); err == nil {
		t.Fatal("NewAzure with no deployment: want error, got nil")
	}
}

// TestAzureFactory checks New(Kind:"azure") builds an Azure-shaped OpenAIProvider
// (reused wire format) with the deployment + api-version URL.
func TestAzureFactory(t *testing.T) {
	pv, err := New(Config{Kind: "Azure", UpstreamHost: "acme.openai.azure.com", Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatalf("azure: %v", err)
	}
	op, ok := pv.(*OpenAIProvider)
	if !ok {
		t.Fatalf("azure kind = %T, want *OpenAIProvider (reused)", pv)
	}
	want := "acme.openai.azure.com/openai/deployments/gpt-4o-mini/chat/completions?api-version=" + defaultAzureAPIVersion
	if !strings.Contains(op.url, want) {
		t.Fatalf("azure url = %q, want it to contain %q", op.url, want)
	}
}

// TestAzureIntegration is an env-gated live smoke test. It runs only when
// AZURE_OPENAI_ENDPOINT + AZURE_OPENAI_API_KEY + AZURE_OPENAI_DEPLOYMENT are set and
// exercises the provider directly against the real Azure endpoint (bypassing the
// model-proxy: it dials the host in the endpoint and injects the api-key locally).
// It is skipped in CI and any credential-free run.
func TestAzureIntegration(t *testing.T) {
	endpoint := os.Getenv("AZURE_OPENAI_ENDPOINT")
	apiKey := os.Getenv("AZURE_OPENAI_API_KEY")
	deployment := os.Getenv("AZURE_OPENAI_DEPLOYMENT")
	if endpoint == "" || apiKey == "" || deployment == "" {
		t.Skip("set AZURE_OPENAI_ENDPOINT, AZURE_OPENAI_API_KEY, AZURE_OPENAI_DEPLOYMENT to run the Azure integration test")
	}
	host := strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	host = strings.TrimSuffix(host, "/")

	// Direct-to-Azure client: TLS to the real host, api-key header set locally (this
	// test path is the only place the sandbox provider talks to a real endpoint; in
	// production the host proxy injects the key and this test never runs).
	p, err := NewAzure(Config{
		UpstreamHost: host,
		Model:        deployment,
		APIVersion:   os.Getenv("AZURE_OPENAI_API_VERSION"),
	})
	if err != nil {
		t.Fatalf("NewAzure: %v", err)
	}
	p.url = "https://" + host + "/openai/deployments/" + deployment + "/chat/completions?api-version=" + p.cfg.APIVersion
	p.client = &http.Client{Transport: &azureKeyRoundTripper{key: apiKey, base: http.DefaultTransport}}

	out, err := p.Query(context.Background(), "Reply with the single word: pong")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !strings.Contains(strings.ToLower(out), "pong") {
		t.Fatalf("Query output = %q, want it to contain 'pong'", out)
	}
}

// azureKeyRoundTripper stamps the api-key header for the env-gated integration test
// only. Production auth is host-side (modelproxy.AzureKeyInjector), never here.
type azureKeyRoundTripper struct {
	key  string
	base http.RoundTripper
}

func (rt *azureKeyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("api-key", rt.key)
	return rt.base.RoundTrip(req)
}
