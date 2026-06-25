package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCmdVaultRotate_RequiresGroupAndCredential(t *testing.T) {
	if err := cmdVaultRotate("http://127.0.0.1:0", []string{"--credential", "github"}); err == nil {
		t.Error("rotate without --group should error")
	}
	if err := cmdVaultRotate("http://127.0.0.1:0", []string{"--group", "grp-1"}); err == nil {
		t.Error("rotate without --credential should error")
	}
}

func TestCmdVaultRotate_ProposesGatewayChangeNoSecret(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chg-1"}`))
	}))
	defer srv.Close()

	err := cmdVaultRotate(srv.URL, []string{"--group", "grp-1", "--credential", "GitHub", "--by", "slack:alice"})
	if err != nil {
		t.Fatalf("cmdVaultRotate: %v", err)
	}
	if gotPath != "/v1/ui/config/change" {
		t.Fatalf("posted to %q, want /v1/ui/config/change (the gateway approval path)", gotPath)
	}
	if gotBody["kind"] != "permissions" {
		t.Fatalf("kind = %v, want permissions", gotBody["kind"])
	}
	if gotBody["agentGroupID"] != "grp-1" {
		t.Fatalf("agentGroupID = %v, want grp-1", gotBody["agentGroupID"])
	}
	after, ok := gotBody["after"].(map[string]any)
	if !ok {
		t.Fatalf("after missing/not an object: %v", gotBody["after"])
	}
	vr, ok := after["vaultRotate"].(map[string]any)
	if !ok {
		t.Fatalf("after.vaultRotate missing: %v", after)
	}
	if vr["credential"] != "github" {
		t.Fatalf("credential = %v, want normalized github", vr["credential"])
	}
	// The change body must carry the credential NAME only — never a secret field.
	raw, _ := json.Marshal(gotBody)
	for _, banned := range []string{"secret", "token", "value", "password"} {
		if strings.Contains(strings.ToLower(string(raw)), banned) {
			t.Fatalf("rotation change body contains a secret-like field %q: %s", banned, raw)
		}
	}
}
