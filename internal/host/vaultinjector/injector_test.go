package vaultinjector

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeEnv builds a lookupEnv over a map.
func fakeEnv(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

// TestInjectorAttachesSecretHostSide: the injector adds the host-held credential on the
// upstream hop, the request carrying only the credential NAME from the broker.
func TestInjectorAttachesSecretHostSide(t *testing.T) {
	const secret = "s3cr3t-token"
	var gotAuth, gotPath, gotCredHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotCredHeader = r.Header.Get(CredHeader)
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	cfg := &Config{Creds: map[string]CredSpec{"github": {Upstream: upstream.URL, SecretEnv: "TOK"}}}
	inj, err := New(cfg, fakeEnv(map[string]string{"TOK": secret}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv := httptest.NewServer(inj.Handler())
	defer srv.Close()

	client := &http.Client{}
	r2, _ := http.NewRequest(http.MethodGet, srv.URL+"/repos/acme", nil)
	r2.Header.Set(CredHeader, "github")
	r2.Header.Set(CorrelationHeader, "corr-123")
	resp2, err := client.Do(r2)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp2.StatusCode)
	}
	if gotAuth != "Bearer "+secret {
		t.Errorf("upstream Authorization = %q, want %q", gotAuth, "Bearer "+secret)
	}
	if gotPath != "/repos/acme" {
		t.Errorf("upstream path = %q, want /repos/acme", gotPath)
	}
	// Host-internal headers must not leak upstream.
	if gotCredHeader != "" {
		t.Errorf("cred header leaked upstream: %q", gotCredHeader)
	}
}

// TestInjectorDenyUnknownCredential: an unknown credential is refused 403 and never
// reaches an upstream.
func TestInjectorDenyUnknownCredential(t *testing.T) {
	cfg := &Config{Creds: map[string]CredSpec{"github": {Upstream: "https://api.github.com", SecretEnv: "TOK"}}}
	inj, err := New(cfg, fakeEnv(map[string]string{"TOK": "x"}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv := httptest.NewServer(inj.Handler())
	defer srv.Close()

	client := &http.Client{}
	r, _ := http.NewRequest(http.MethodGet, srv.URL+"/x", nil)
	r.Header.Set(CredHeader, "gitlab") // not configured
	resp, err := client.Do(r)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for unknown credential", resp.StatusCode)
	}
}

// TestInjectorMissingCredentialHeader: a request with no credential header is refused.
func TestInjectorMissingCredentialHeader(t *testing.T) {
	cfg := &Config{Creds: map[string]CredSpec{"github": {Upstream: "https://api.github.com", SecretEnv: "TOK"}}}
	inj, _ := New(cfg, fakeEnv(map[string]string{"TOK": "x"}))
	srv := httptest.NewServer(inj.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 with no credential header", resp.StatusCode)
	}
}

// TestInjectorFailsClosedOnUnsetSecret: a credential whose secret env var is unset is a
// construction error — the injector never serves a credential it cannot attach.
func TestInjectorFailsClosedOnUnsetSecret(t *testing.T) {
	cfg := &Config{Creds: map[string]CredSpec{"github": {Upstream: "https://api.github.com", SecretEnv: "MISSING"}}}
	if _, err := New(cfg, fakeEnv(map[string]string{})); err == nil {
		t.Fatal("New must fail when a credential's secret env var is unset")
	}
}

// TestConfigUpstreamHost: the control-plane reads cred -> upstream host for policy.
func TestConfigUpstreamHost(t *testing.T) {
	cfg := &Config{Creds: map[string]CredSpec{"github": {Upstream: "https://api.github.com:443", SecretEnv: "TOK"}}}
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	host, ok := cfg.UpstreamHost("github")
	if !ok || host != "api.github.com" {
		t.Fatalf("UpstreamHost = %q,%v, want api.github.com,true", host, ok)
	}
	if _, ok := cfg.UpstreamHost("nope"); ok {
		t.Error("UpstreamHost should report false for an unknown credential")
	}
	if got := cfg.CredHosts()["github"]; got != "api.github.com" {
		t.Errorf("CredHosts[github] = %q, want api.github.com", got)
	}
}

// TestConfigValidateRejectsBadUpstream: a non-http(s) or hostless upstream is rejected.
func TestConfigValidateRejectsBadUpstream(t *testing.T) {
	bad := []CredSpec{
		{Upstream: "ftp://x", SecretEnv: "T"},
		{Upstream: "not a url", SecretEnv: "T"},
		{Upstream: "https://api.github.com", SecretEnv: ""},
	}
	for i, spec := range bad {
		cfg := &Config{Creds: map[string]CredSpec{"c": spec}}
		if err := cfg.validate(); err == nil {
			t.Errorf("case %d: validate accepted invalid spec %+v", i, spec)
		}
	}
}

// TestInjectorDoesNotEchoSecret: a quick guard that the configured secret value is not
// written into the response by the injector itself (the broker redactor is the
// belt-and-braces backstop, but the injector adds the secret only on the upstream hop).
func TestInjectorDoesNotEchoSecret(t *testing.T) {
	const secret = "must-not-appear"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "upstream body without the token")
	}))
	defer upstream.Close()
	cfg := &Config{Creds: map[string]CredSpec{"c": {Upstream: upstream.URL, SecretEnv: "TOK"}}}
	inj, _ := New(cfg, fakeEnv(map[string]string{"TOK": secret}))
	srv := httptest.NewServer(inj.Handler())
	defer srv.Close()
	client := &http.Client{}
	r, _ := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	r.Header.Set(CredHeader, "c")
	resp, err := client.Do(r)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), secret) {
		t.Errorf("injector echoed the secret in its response: %q", string(body))
	}
}
