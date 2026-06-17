package egress

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestNewVault(t *testing.T) {
	// Empty endpoint => disabled vault, not an error.
	v, err := NewVault("")
	if err != nil {
		t.Fatalf("empty endpoint should disable, not error: %v", err)
	}
	if v.Configured() {
		t.Fatal("empty endpoint must yield an unconfigured vault")
	}
	if v.Endpoint() != "" {
		t.Fatalf("unconfigured Endpoint() = %q, want empty", v.Endpoint())
	}

	// Valid endpoints.
	for _, ok := range []string{"http://127.0.0.1:8200", "https://vault.internal"} {
		v, err := NewVault(ok)
		if err != nil {
			t.Fatalf("NewVault(%q): %v", ok, err)
		}
		if !v.Configured() {
			t.Fatalf("NewVault(%q) not configured", ok)
		}
	}
	if got := mustVault(t, "http://127.0.0.1:8200").Endpoint(); got != "127.0.0.1:8200" {
		t.Fatalf("Endpoint() = %q", got)
	}

	// Invalid endpoints fail closed.
	for _, bad := range []string{"ftp://x", "127.0.0.1:8200" /* no scheme => no host */, "http://", "://nope"} {
		if _, err := NewVault(bad); err == nil {
			t.Errorf("NewVault(%q) should error", bad)
		}
	}
}

func TestIsVaultAddressed(t *testing.T) {
	for _, h := range []string{"vault", "VAULT", "vault:443", "Vault:8200"} {
		if !IsVaultAddressed(h, "") {
			t.Errorf("host %q should be vault-addressed", h)
		}
	}
	if !IsVaultAddressed("github", "vault") {
		t.Error("scheme vault:// should be vault-addressed")
	}
	for _, h := range []string{"api.github.com", "vaulted.example.com", "myvault.io"} {
		if IsVaultAddressed(h, "https") {
			t.Errorf("host %q must NOT be treated as the vault", h)
		}
	}
}

func TestParseCredential(t *testing.T) {
	cases := []struct {
		host, scheme, path string
		wantCred, wantUp   string
	}{
		{"vault", "", "/github/repos/acme/app", "github", "/repos/acme/app"},
		{"vault", "", "/github", "github", "/"},
		{"vault", "", "/github/", "github", "/"},
		{"vault:8200", "", "/pagerduty/incidents", "pagerduty", "/incidents"},
		{"github", "vault", "/repos/acme/app", "github", "/repos/acme/app"}, // scheme form
		{"github", "vault", "", "github", "/"},
	}
	for _, c := range cases {
		cred, up, err := ParseCredential(c.host, c.scheme, c.path)
		if err != nil {
			t.Errorf("ParseCredential(%q,%q,%q) error: %v", c.host, c.scheme, c.path, err)
			continue
		}
		if cred != c.wantCred || up != c.wantUp {
			t.Errorf("ParseCredential(%q,%q,%q) = (%q,%q), want (%q,%q)",
				c.host, c.scheme, c.path, cred, up, c.wantCred, c.wantUp)
		}
	}
}

func TestParseCredentialRejectsBadNames(t *testing.T) {
	bad := []struct{ host, scheme, path string }{
		{"vault", "", "/../etc/passwd"}, // cred ".."
		{"vault", "", "//x"},            // empty cred
		{"vault", "", "/"},              // empty cred
		{"vault", "", "/ab cd/x"},       // space in cred
	}
	for _, c := range bad {
		if _, _, err := ParseCredential(c.host, c.scheme, c.path); err == nil {
			t.Errorf("ParseCredential(%q,%q,%q) should reject", c.host, c.scheme, c.path)
		}
	}
}

// TestForwardInjectsNoSecret is the load-bearing B4-E test: Forward must rewrite the
// destination to the injector and add NO secret. A sandbox-supplied Authorization
// header must be stripped (the broker never forwards a credential), and the only
// thing the broker adds is the logical credential NAME.
func TestForwardInjectsNoSecret(t *testing.T) {
	v := mustVault(t, "http://127.0.0.1:8200")
	req := &http.Request{
		Host:   "vault",
		URL:    &url.URL{Path: "/github/repos/acme/app"},
		Header: http.Header{},
	}
	req.Header.Set("Authorization", "Bearer sandbox-forged-token")
	req.Header.Set("X-Custom", "keep-me")

	cred, err := v.Forward(req)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if cred != "github" {
		t.Fatalf("cred = %q, want github", cred)
	}
	// Destination is the injector, not the real API.
	if req.URL.Host != "127.0.0.1:8200" || req.URL.Scheme != "http" || req.Host != "127.0.0.1:8200" {
		t.Fatalf("not rewritten to injector: scheme=%q host=%q reqHost=%q", req.URL.Scheme, req.URL.Host, req.Host)
	}
	if req.URL.Path != "/repos/acme/app" {
		t.Fatalf("upstream path = %q, want /repos/acme/app", req.URL.Path)
	}
	// B4-E: the sandbox-supplied Authorization must be gone; the broker injects none.
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization must be stripped (broker injects no secret), got %q", got)
	}
	// The broker tags only the logical credential NAME (not a key).
	if got := req.Header.Get(VaultCredHeader); got != "github" {
		t.Fatalf("%s = %q, want github", VaultCredHeader, got)
	}
	if got := req.Header.Get("X-Custom"); got != "keep-me" {
		t.Fatalf("non-credential headers should pass through, X-Custom = %q", got)
	}
	// Defense in depth: assert no header value carries the forged token.
	for k, vals := range req.Header {
		for _, val := range vals {
			if strings.Contains(val, "sandbox-forged-token") {
				t.Fatalf("forged credential leaked through header %q: %q", k, val)
			}
		}
	}
}

func TestForwardDeniedWhenUnconfigured(t *testing.T) {
	v, _ := NewVault("") // disabled
	req := &http.Request{Host: "vault", URL: &url.URL{Path: "/github/x"}, Header: http.Header{}}
	if _, err := v.Forward(req); err == nil {
		t.Fatal("unconfigured vault must DENY vault addressing (deny-by-default)")
	}
	// req must be left untouched (no redirect to a nil injector).
	if req.URL.Host != "" {
		t.Fatalf("denied request must be unmodified, host=%q", req.URL.Host)
	}
}

func TestForwardRejectsNonVaultAndBadCred(t *testing.T) {
	v := mustVault(t, "http://127.0.0.1:8200")

	// Non-vault request misrouted to Forward => error, not a silent redirect.
	normal := &http.Request{Host: "api.github.com", URL: &url.URL{Path: "/x"}, Header: http.Header{}}
	if _, err := v.Forward(normal); err == nil {
		t.Error("Forward on a non-vault request should error")
	}
	if normal.URL.Host != "" {
		t.Errorf("misrouted normal request must be unmodified, host=%q", normal.URL.Host)
	}

	// Vault-addressed but malformed credential => error.
	bad := &http.Request{Host: "vault", URL: &url.URL{Path: "/../etc/passwd"}, Header: http.Header{}}
	if _, err := v.Forward(bad); err == nil {
		t.Error("Forward with a traversal credential should error")
	}
}

func mustVault(t *testing.T, endpoint string) *Vault {
	t.Helper()
	v, err := NewVault(endpoint)
	if err != nil {
		t.Fatalf("NewVault(%q): %v", endpoint, err)
	}
	return v
}
