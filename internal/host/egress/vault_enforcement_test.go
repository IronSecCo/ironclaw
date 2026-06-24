package egress

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/host/vaultinjector"
)

// mustHost returns the bare host (no port) of a URL, for asserting the upstream the
// vault policy is enforced against.
func mustHost(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u.Hostname()
}

// buildVaultStack wires a real upstream + the reference injector + a policy-enforcing
// broker, returning the broker, the injector endpoint host, the upstream host, and a
// pointer to the last Authorization the upstream observed.
func buildVaultStack(t *testing.T, secret string, guard VaultGuard) (*Broker, string, *string, *[]AuditRecord) {
	t.Helper()

	gotUpstreamAuth := new(string)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotUpstreamAuth = r.Header.Get("Authorization")
		if r.Header.Get("Authorization") != "Bearer "+secret {
			http.Error(w, "upstream: bad credential", http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, "ok from upstream; secret was "+secret)
	}))
	t.Cleanup(upstream.Close)
	upstreamHost := mustHost(t, upstream.URL)

	cfg := &vaultinjector.Config{Creds: map[string]vaultinjector.CredSpec{
		"github": {Upstream: upstream.URL, SecretEnv: "VAULT_GITHUB_TOKEN"},
	}}
	inj, err := vaultinjector.New(cfg, func(string) (string, bool) { return secret, true })
	if err != nil {
		t.Fatalf("vaultinjector.New: %v", err)
	}
	injector := httptest.NewServer(inj.Handler())
	t.Cleanup(injector.Close)

	vault, err := NewVault(injector.URL)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	audits := new([]AuditRecord)
	b := New(nil,
		WithVault(vault),
		WithVaultGuard(guard),
		WithCorrelator(NewCorrelator()),
		WithResponseRedactor(NewRedactor(secret)),
		WithAudit(func(rec AuditRecord) { *audits = append(*audits, rec) }),
	)
	b.Allow(vault.Endpoint())
	_ = upstreamHost
	return b, vault.Endpoint(), gotUpstreamAuth, audits
}

// TestVaultGrantedResolvesEndToEnd: a gateway-approved policy lets a TRUSTED session
// reach a credential end-to-end through the broker -> injector -> upstream, the agent
// never holding a key, and the secret never echoing back.
func TestVaultGrantedResolvesEndToEnd(t *testing.T) {
	const secret = "real-upstream-secret"
	guard := func(session, cred string) (string, bool) {
		return "api.example.test", session == "s1" && cred == "github"
	}
	b, _, gotUpstreamAuth, audits := buildVaultStack(t, secret, guard)

	// Per-session socket for s1: the broker TRUSTS the socket identity. The request
	// even forges the advisory header to another session — it must be ignored.
	h := b.sessionHandler("s1")
	req := httptest.NewRequest(http.MethodGet, "http://vault/github/repos/acme", nil)
	req.Host = "vault"
	req.Header.Set(sessionHeader, "s2-attacker")
	req.Header.Set("Authorization", "Bearer sandbox-forged")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	// The upstream saw the host-injected real credential, NOT the sandbox's forged one.
	if *gotUpstreamAuth != "Bearer "+secret {
		t.Errorf("upstream Authorization = %q, want host-injected %q", *gotUpstreamAuth, "Bearer "+secret)
	}
	// Response redaction: the secret never reaches the sandbox even if reflected.
	if body := rec.Body.String(); strings.Contains(body, secret) {
		t.Errorf("secret leaked to sandbox: %q", body)
	}
	// Audit records the use with the credential NAME + a correlation id, allowed.
	last := (*audits)[len(*audits)-1]
	if last.VaultCredential != "github" || last.CorrelationID == "" || !last.Allowed {
		t.Fatalf("audit not correlated/allowed: %+v", last)
	}
}

// TestVaultDenyByDefaultUngrantedSession: a TRUSTED session whose group has no grant
// for the credential is refused 403 — deny-by-default — and the upstream is never hit.
func TestVaultDenyByDefaultUngrantedSession(t *testing.T) {
	const secret = "real-upstream-secret"
	guard := func(session, cred string) (string, bool) {
		return "api.example.test", session == "s1" && cred == "github" // s2 granted nothing
	}
	b, _, gotUpstreamAuth, audits := buildVaultStack(t, secret, guard)

	h := b.sessionHandler("s2") // un-granted group
	req := httptest.NewRequest(http.MethodGet, "http://vault/github/repos/acme", nil)
	req.Host = "vault"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (deny-by-default)", rec.Code)
	}
	if *gotUpstreamAuth != "" {
		t.Errorf("upstream was reached on a denied request (auth=%q)", *gotUpstreamAuth)
	}
	last := (*audits)[len(*audits)-1]
	if last.Allowed || last.VaultCredential != "github" {
		t.Fatalf("denied audit must record the cred name and not-allowed: %+v", last)
	}
}

// TestVaultSpoofedSessionCannotEscalate: a compromised sandbox on an UN-granted
// per-session socket cannot borrow a granted group's credential by forging the
// X-Ironclaw-Session header — the broker keys policy on the socket identity, not the
// header.
func TestVaultSpoofedSessionCannotEscalate(t *testing.T) {
	const secret = "real-upstream-secret"
	guard := func(session, cred string) (string, bool) {
		return "api.example.test", session == "granted" && cred == "github"
	}
	b, _, gotUpstreamAuth, _ := buildVaultStack(t, secret, guard)

	// Socket bound for the un-granted "victimless" session; the request forges the
	// header of the granted session.
	h := b.sessionHandler("attacker")
	req := httptest.NewRequest(http.MethodGet, "http://vault/github/repos/acme", nil)
	req.Host = "vault"
	req.Header.Set(sessionHeader, "granted") // spoof the granted session id
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 — spoofed session must not escalate", rec.Code)
	}
	if *gotUpstreamAuth != "" {
		t.Errorf("upstream reached via spoofed session (auth=%q)", *gotUpstreamAuth)
	}
}

// TestVaultRefusedOnSharedUntrustedSocket: with enforcement wired, the shared Handler
// socket (session only from the spoofable header) refuses vault addressing entirely —
// a credential is reachable only over a host-created per-session socket.
func TestVaultRefusedOnSharedUntrustedSocket(t *testing.T) {
	const secret = "real-upstream-secret"
	guard := func(session, cred string) (string, bool) {
		return "api.example.test", true // would allow if it ran — but it must not
	}
	b, _, gotUpstreamAuth, _ := buildVaultStack(t, secret, guard)

	req := httptest.NewRequest(http.MethodGet, "http://vault/github/repos/acme", nil)
	req.Host = "vault"
	req.Header.Set(sessionHeader, "anything")
	rec := httptest.NewRecorder()
	b.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 — vault must not be served on the shared socket", rec.Code)
	}
	if *gotUpstreamAuth != "" {
		t.Errorf("upstream reached over the untrusted shared socket (auth=%q)", *gotUpstreamAuth)
	}
}

// TestVaultUngrantedCredentialRefused: even a trusted, otherwise-granted session is
// refused a credential its group is not granted (per-credential deny-by-default).
func TestVaultUngrantedCredentialRefused(t *testing.T) {
	const secret = "real-upstream-secret"
	guard := func(session, cred string) (string, bool) {
		return "api.example.test", session == "s1" && cred == "github" // only github
	}
	b, _, _, _ := buildVaultStack(t, secret, guard)

	h := b.sessionHandler("s1")
	req := httptest.NewRequest(http.MethodGet, "http://vault/gitlab/projects", nil)
	req.Host = "vault"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for an un-granted credential", rec.Code)
	}
}
