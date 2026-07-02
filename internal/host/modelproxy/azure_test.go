package modelproxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAzureKeyInjectorHeader checks the api-key injector stamps the header for any
// *.openai.azure.com resource host and no-ops for every other provider.
func TestAzureKeyInjectorHeader(t *testing.T) {
	inj := AzureKeyInjector("sk-azure-123")
	cases := []struct {
		host string
		want string
	}{
		{"my-resource.openai.azure.com", "sk-azure-123"},
		{"acme-eastus.openai.azure.com", "sk-azure-123"},
		{"my-resource.openai.azure.com:443", "sk-azure-123"},
		{"api.openai.com", ""},
		{"api.anthropic.com", ""},
		{"evil.openai.azure.com.attacker.test", ""},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest("POST", "http://"+tc.host+"/openai/deployments/d/chat/completions", nil)
		inj(tc.host, req)
		if got := req.Header.Get("api-key"); got != tc.want {
			t.Errorf("host %q: api-key = %q, want %q", tc.host, got, tc.want)
		}
	}
}

// TestAzureKeyInjectorEmptyKey checks an empty key is a no-op (no header stamped).
func TestAzureKeyInjectorEmptyKey(t *testing.T) {
	inj := AzureKeyInjector("")
	req, _ := http.NewRequest("POST", "http://my-resource.openai.azure.com/x", nil)
	inj("my-resource.openai.azure.com", req)
	if got := req.Header.Get("api-key"); got != "" {
		t.Fatalf("api-key = %q, want empty for an empty key", got)
	}
}

// TestAzureTokenInjectorBearer checks the Entra token injector stamps the bearer for
// azure hosts and no-ops elsewhere.
func TestAzureTokenInjectorBearer(t *testing.T) {
	inj := AzureTokenInjector(StaticTokenSource("entra.token"))
	cases := []struct {
		host string
		want string
	}{
		{"my-resource.openai.azure.com", "Bearer entra.token"},
		{"api.openai.com", ""},
		{"aiplatform.googleapis.com", ""},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest("POST", "http://"+tc.host+"/x", nil)
		inj(tc.host, req)
		if got := req.Header.Get("Authorization"); got != tc.want {
			t.Errorf("host %q: Authorization = %q, want %q", tc.host, got, tc.want)
		}
	}
}

// TestAzureTokenInjectorErrorLeavesUnauthenticated checks a token-source failure does
// not panic or fail the proxy — the request goes out unauthenticated and the upstream
// rejects it.
func TestAzureTokenInjectorErrorLeavesUnauthenticated(t *testing.T) {
	inj := AzureTokenInjector(errTokenSource{})
	req, _ := http.NewRequest("POST", "http://my-resource.openai.azure.com/x", nil)
	inj("my-resource.openai.azure.com", req)
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want empty when the token source errors", got)
	}
}

// TestAzureKeyInjectorEndToEnd verifies the proxy injects the host-held api-key end
// to end and the sandbox never sees it.
func TestAzureKeyInjectorEndToEnd(t *testing.T) {
	var gotKey string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("api-key")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	const host = "my-resource.openai.azure.com"
	p := New([]string{host},
		WithInjector(AzureKeyInjector("host-held-key")),
		WithTransport(&redirectTransport{target: upstream.Listener.Addr().String()}),
	)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/openai/deployments/gpt-4o/chat/completions?api-version=2024-10-21", nil)
	req.Host = host
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotKey != "host-held-key" {
		t.Fatalf("upstream api-key = %q, want the host-held key", gotKey)
	}
}
