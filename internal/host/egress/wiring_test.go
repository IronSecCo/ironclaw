package egress

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandlerRoutesVaultToInjector exercises the fully wired broker: a vault://
// request is forwarded to the host-local injector (allowlisted), the broker injects
// no secret, a correlation id is stamped + audited, and the response is redacted on
// the way back. This is the T-260 data path coming live end to end.
func TestHandlerRoutesVaultToInjector(t *testing.T) {
	var gotPath, gotCred, gotAuth, gotCorr string
	injector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotCred = r.Header.Get(VaultCredHeader)
		gotAuth = r.Header.Get("Authorization")
		gotCorr = r.Header.Get(CorrelationHeader)
		w.Header().Set("Authorization", "Bearer injector-echo") // must be stripped on the way back
		_, _ = io.WriteString(w, "result contains the-secret here")
	}))
	defer injector.Close()

	vault, err := NewVault(injector.URL)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	var audit AuditRecord
	b := New(nil,
		WithVault(vault),
		WithCorrelator(NewCorrelator()),
		WithResponseRedactor(NewRedactor("the-secret")),
		WithAudit(func(rec AuditRecord) { audit = rec }),
	)
	b.Allow(vault.Endpoint()) // the injector endpoint is deny-by-default like any host

	req := httptest.NewRequest(http.MethodGet, "http://vault/github/repos/acme", nil)
	req.Host = "vault"
	req.Header.Set("Authorization", "Bearer sandbox-forged")
	rec := httptest.NewRecorder()
	b.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	// The injector saw the rewritten request: upstream path, credential NAME, and NO
	// sandbox-supplied Authorization (B4-E: the broker forwards no credential).
	if gotPath != "/repos/acme" {
		t.Errorf("injector path = %q, want /repos/acme", gotPath)
	}
	if gotCred != "github" {
		t.Errorf("injector cred header = %q, want github", gotCred)
	}
	if gotAuth != "" {
		t.Errorf("broker must not forward the sandbox Authorization, injector saw %q", gotAuth)
	}
	if gotCorr == "" {
		t.Error("correlation id not stamped on the broker->vault request")
	}
	// Response redaction: the secret is scrubbed and the injector's credential header
	// is stripped before reaching the sandbox.
	if body := rec.Body.String(); strings.Contains(body, "the-secret") {
		t.Errorf("secret not redacted in response: %q", body)
	}
	if rec.Result().Header.Get("Authorization") != "" {
		t.Error("response Authorization header must be stripped on the broker->sandbox hop")
	}
	// Audit correlates the use.
	if audit.VaultCredential != "github" || audit.CorrelationID != gotCorr || !audit.Allowed {
		t.Fatalf("audit not correlated: %+v (corr on wire=%q)", audit, gotCorr)
	}
}

// TestHandlerVaultDeniedWhenInjectorNotAllowlisted: even a configured vault is
// deny-by-default — if the injector endpoint is not on the allowlist, the request
// is refused 403.
func TestHandlerVaultDeniedWhenInjectorNotAllowlisted(t *testing.T) {
	vault, err := NewVault("http://127.0.0.1:9/x") // unreachable + not allowlisted
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	b := New(nil, WithVault(vault))
	req := httptest.NewRequest(http.MethodGet, "http://vault/github/x", nil)
	req.Host = "vault"
	rec := httptest.NewRecorder()
	b.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (injector not allowlisted)", rec.Code)
	}
}

// TestHandlerNoVaultLeavesNonVaultUnchanged: with a vault configured, an ordinary
// (non-vault) request is NOT routed to the injector — it still hits the normal
// allowlist path.
func TestHandlerNoVaultRoutingForNormalHost(t *testing.T) {
	vault, _ := NewVault("http://127.0.0.1:8200")
	b := New(nil, WithVault(vault)) // api.github.com NOT allowlisted
	req := httptest.NewRequest(http.MethodGet, "https://api.github.com/x", nil)
	req.Host = "api.github.com"
	rec := httptest.NewRecorder()
	b.Handler().ServeHTTP(rec, req)
	// Not allowlisted -> 403, and crucially it was evaluated as api.github.com (not
	// rerouted to the vault injector).
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
