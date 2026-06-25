package vaultinjector

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestRotateSwapsHeldSecret: after Rotate re-reads the env, subsequent injections
// attach the NEW secret. The new value never travels through Rotate — it is read from
// the (mutated) host env, mirroring an operator rotating the secret in its source.
func TestRotateSwapsHeldSecret(t *testing.T) {
	var seen string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	env := map[string]string{"TOK": "old-secret"}
	cfg := &Config{Creds: map[string]CredSpec{"github": {Upstream: upstream.URL, SecretEnv: "TOK"}}}
	inj, err := New(cfg, fakeEnv(env))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv := httptest.NewServer(inj.Handler())
	defer srv.Close()
	do := func() {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
		req.Header.Set(CredHeader, "github")
		resp, derr := (&http.Client{}).Do(req)
		if derr != nil {
			t.Fatalf("do: %v", derr)
		}
		resp.Body.Close()
	}

	do()
	if seen != "Bearer old-secret" {
		t.Fatalf("before rotate: upstream saw %q, want Bearer old-secret", seen)
	}

	// Operator rotates the secret at its source, then signals the injector.
	env["TOK"] = "new-secret"
	if err := inj.Rotate("github"); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	do()
	if seen != "Bearer new-secret" {
		t.Fatalf("after rotate: upstream saw %q, want Bearer new-secret", seen)
	}
}

// TestRotateUnknownCredential: an unknown credential is refused (deny-by-default) and
// the control handler reports 403, never touching any held secret.
func TestRotateUnknownCredential(t *testing.T) {
	env := map[string]string{"TOK": "s"}
	cfg := &Config{Creds: map[string]CredSpec{"github": {Upstream: "https://api.github.com", SecretEnv: "TOK"}}}
	inj, err := New(cfg, fakeEnv(env))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := inj.Rotate("stripe"); err == nil {
		t.Fatal("Rotate of unknown credential should error")
	}

	srv := httptest.NewServer(inj.RotateHandler())
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/rotate", nil)
	req.Header.Set(CredHeader, "stripe")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// TestRotateFailsClosedOnUnsetSecret: if the secret env is now unset/empty, Rotate
// errors and KEEPS the previous secret rather than blanking a live credential.
func TestRotateFailsClosedOnUnsetSecret(t *testing.T) {
	var seen string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
	}))
	defer upstream.Close()

	env := map[string]string{"TOK": "live-secret"}
	cfg := &Config{Creds: map[string]CredSpec{"github": {Upstream: upstream.URL, SecretEnv: "TOK"}}}
	inj, err := New(cfg, fakeEnv(env))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	delete(env, "TOK") // secret source went away
	if err := inj.Rotate("github"); err == nil {
		t.Fatal("Rotate with unset secret env should error (fail closed)")
	}

	// The old secret must still be served.
	srv := httptest.NewServer(inj.Handler())
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	req.Header.Set(CredHeader, "github")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	resp.Body.Close()
	if seen != "Bearer live-secret" {
		t.Fatalf("after failed rotate: upstream saw %q, want the unchanged Bearer live-secret", seen)
	}
}

// TestRotateHandlerAuditNoSecret: a successful rotation audits with Action=="rotate"
// and the credential NAME, and the response body carries no secret.
func TestRotateHandlerAuditNoSecret(t *testing.T) {
	env := map[string]string{"TOK": "first"}
	cfg := &Config{Creds: map[string]CredSpec{"github": {Upstream: "https://api.github.com", SecretEnv: "TOK"}}}
	var mu sync.Mutex
	var recs []AuditRecord
	inj, err := New(cfg, fakeEnv(env), WithAudit(func(r AuditRecord) {
		mu.Lock()
		recs = append(recs, r)
		mu.Unlock()
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	env["TOK"] = "second"

	srv := httptest.NewServer(inj.RotateHandler())
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/rotate", nil)
	req.Header.Set(CredHeader, "github")
	req.Header.Set(CorrelationHeader, "corr-rot")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if strings.Contains(string(body), "second") || strings.Contains(string(body), "first") {
		t.Fatalf("rotate response leaked a secret: %q", body)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(recs) != 1 {
		t.Fatalf("audit records = %d, want 1", len(recs))
	}
	got := recs[0]
	if got.Action != "rotate" || got.Credential != "github" || !got.Allowed || got.CorrelationID != "corr-rot" {
		t.Fatalf("audit = %+v, want rotate/github/allowed/corr-rot", got)
	}
}
