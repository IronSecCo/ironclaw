package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/host/vaultinjector"
)

func TestVaultRotateSignaller_PostsCredNameToControlEndpoint(t *testing.T) {
	var gotPath, gotCred, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotCred = r.Header.Get(vaultinjector.CredHeader)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	signal, err := newVaultRotateSignaller(srv.URL)
	if err != nil {
		t.Fatalf("newVaultRotateSignaller: %v", err)
	}
	if err := signal("grp-1", "github"); err != nil {
		t.Fatalf("signal: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/rotate" {
		t.Fatalf("got %s %s, want POST /rotate", gotMethod, gotPath)
	}
	if gotCred != "github" {
		t.Fatalf("cred header = %q, want github", gotCred)
	}
}

func TestVaultRotateSignaller_NonTwoXXIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unknown credential", http.StatusForbidden)
	}))
	defer srv.Close()

	signal, err := newVaultRotateSignaller(srv.URL)
	if err != nil {
		t.Fatalf("newVaultRotateSignaller: %v", err)
	}
	if err := signal("grp-1", "stripe"); err == nil {
		t.Fatal("a 403 from the injector must surface as an error, not a silent success")
	}
}

func TestVaultRotateSignaller_RejectsBadEndpoint(t *testing.T) {
	if _, err := newVaultRotateSignaller(""); err == nil {
		t.Error("empty endpoint should error")
	}
	if _, err := newVaultRotateSignaller("127.0.0.1:8201"); err == nil {
		t.Error("a scheme-less endpoint should error (must be http(s):// or unix:)")
	}
	if _, err := newVaultRotateSignaller("unix:"); err == nil {
		t.Error("unix: with no socket path should error")
	}
}
