package modelproxy

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestAllowedHost(t *testing.T) {
	p := New([]string{"api.anthropic.com"})
	tests := []struct {
		host string
		want bool
	}{
		{"api.anthropic.com", true},
		{"api.anthropic.com:443", true},
		{"API.ANTHROPIC.COM", true},
		{"evil.example.com", false},
		{"evil.example.com:443", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := p.allowedHost(tt.host); got != tt.want {
			t.Errorf("allowedHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestHandlerForbidsNonAllowed(t *testing.T) {
	p := New([]string{"api.anthropic.com"})
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/v1/messages", nil)
	req.Host = "evil.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestHandlerForwardsAllowed(t *testing.T) {
	// Stand up a fake upstream and point the proxy's allowlist + transport at it.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok from upstream")
	}))
	defer upstream.Close()

	p := New([]string{"api.anthropic.com"})
	// Redirect upstream HTTPS dials to the test server.
	p.transport = &redirectTransport{target: upstream.Listener.Addr().String()}

	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/v1/messages", nil)
	req.Host = "api.anthropic.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok from upstream" {
		t.Fatalf("body = %q", string(body))
	}
}

func TestInjectorAuthenticatesAndStripsSandboxAuth(t *testing.T) {
	// Capture what the upstream actually receives.
	var gotAPIKey, gotVersion, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p := New([]string{"api.anthropic.com"},
		WithInjector(AnthropicInjector("host-secret-key", "2023-06-01")),
		WithTransport(&redirectTransport{target: upstream.Listener.Addr().String()}),
	)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/v1/messages", nil)
	req.Host = "api.anthropic.com"
	// The sandbox tries to present its own credentials; the proxy must strip them.
	req.Header.Set("Authorization", "Bearer sandbox-forged-token")
	req.Header.Set("x-api-key", "sandbox-forged-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if gotAPIKey != "host-secret-key" {
		t.Errorf("upstream x-api-key = %q, want host-injected key", gotAPIKey)
	}
	if gotVersion != "2023-06-01" {
		t.Errorf("upstream anthropic-version = %q", gotVersion)
	}
	if gotAuth != "" {
		t.Errorf("sandbox-supplied Authorization leaked upstream: %q", gotAuth)
	}
}

func TestMultiInjectorRoutesCredentialByHost(t *testing.T) {
	// One proxy fronting three providers; the MultiInjector must stamp exactly the
	// matching provider's credential and nothing else, regardless of which upstream
	// the (untrusted) sandbox addresses.
	cases := []struct {
		host        string
		wantAPIKey  string // x-api-key (Anthropic)
		wantAuth    string // Authorization (OpenAI / OpenRouter)
		wantVersion string
	}{
		{"api.anthropic.com", "anthropic-secret", "", "2023-06-01"},
		{"api.openai.com", "", "Bearer openai-secret", ""},
		{"openrouter.ai", "", "Bearer openrouter-secret", ""},
	}

	inject := MultiInjector(
		AnthropicInjector("anthropic-secret", "2023-06-01"),
		OpenAIInjector("openai-secret"),
		OpenRouterInjector("openrouter-secret"),
	)

	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			var gotAPIKey, gotAuth, gotVersion string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAPIKey = r.Header.Get("x-api-key")
				gotAuth = r.Header.Get("Authorization")
				gotVersion = r.Header.Get("anthropic-version")
				w.WriteHeader(http.StatusOK)
			}))
			defer upstream.Close()

			p := New([]string{"api.anthropic.com", "api.openai.com", "openrouter.ai"},
				WithInjector(inject),
				WithTransport(&redirectTransport{target: upstream.Listener.Addr().String()}),
			)
			srv := httptest.NewServer(p.Handler())
			defer srv.Close()

			req, _ := http.NewRequest("GET", srv.URL+"/v1/x", nil)
			req.Host = tc.host
			// The sandbox forges both credential shapes; the proxy must strip them.
			req.Header.Set("Authorization", "Bearer sandbox-forged")
			req.Header.Set("x-api-key", "sandbox-forged")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()

			if gotAPIKey != tc.wantAPIKey {
				t.Errorf("x-api-key = %q, want %q", gotAPIKey, tc.wantAPIKey)
			}
			if gotAuth != tc.wantAuth {
				t.Errorf("Authorization = %q, want %q", gotAuth, tc.wantAuth)
			}
			if gotVersion != tc.wantVersion {
				t.Errorf("anthropic-version = %q, want %q", gotVersion, tc.wantVersion)
			}
		})
	}
}

func TestProviderInjectorsNoOpOffHost(t *testing.T) {
	// Each provider injector must touch only its own upstream so MultiInjector can
	// compose them without cross-contamination.
	req, _ := http.NewRequest("GET", "http://example.test/x", nil)
	OpenAIInjector("k")("api.anthropic.com", req)
	OpenRouterInjector("k")("api.openai.com", req)
	AnthropicInjector("k", "v")("openrouter.ai", req)
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization set off-host: %q", got)
	}
	if got := req.Header.Get("x-api-key"); got != "" {
		t.Errorf("x-api-key set off-host: %q", got)
	}
}

func TestServeUnixSocketRoundTrip(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "via-socket")
	}))
	defer upstream.Close()

	sock := filepath.Join(t.TempDir(), "proxy.sock")
	p := New([]string{"api.anthropic.com"})
	p.transport = &redirectTransport{target: upstream.Listener.Addr().String()}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = p.Serve(ctx, sock) }()

	// Wait for the socket to appear.
	waitFor(t, func() bool {
		_, err := net.Dial("unix", sock)
		return err == nil
	})

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", sock)
		},
	}}
	req, _ := http.NewRequest("GET", "http://api.anthropic.com/v1/messages", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "via-socket" {
		t.Fatalf("body = %q, want via-socket", string(body))
	}
}

// redirectTransport rewrites every dial to a fixed host:port so we can stand a
// plain-HTTP test server in for an HTTPS upstream.
type redirectTransport struct{ target string }

func (rt *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = rt.target
	req.Host = rt.target
	return http.DefaultTransport.RoundTrip(req)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}
