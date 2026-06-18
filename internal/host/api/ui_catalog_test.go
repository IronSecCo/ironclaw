package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/host/catalog"
	"github.com/IronSecCo/ironclaw/internal/host/gateway"
)

func catalogTestServer() http.Handler {
	return New(gateway.New(gateway.VerifierChain{gateway.AlwaysRequireHuman{}}, gateway.NewManualApprover(), gateway.NewLogApplier(), gateway.NewMemoryStore())).Handler()
}

// TestUITools verifies GET /v1/ui/tools returns the full built-in catalog. It needs
// no registry (the catalog is static), proving discovery works on a bare daemon.
func TestUITools(t *testing.T) {
	h := catalogTestServer()
	req := httptest.NewRequest(http.MethodGet, "/v1/ui/tools", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var got []catalog.ToolInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != len(catalog.Tools()) {
		t.Fatalf("got %d tools, want %d", len(got), len(catalog.Tools()))
	}
	// Spot-check a known tool round-trips with its badges.
	var web *catalog.ToolInfo
	for i := range got {
		if got[i].Name == "web_search" {
			web = &got[i]
		}
	}
	if web == nil {
		t.Fatalf("web_search missing from /v1/ui/tools")
	}
	if !web.Egress || web.Title == "" {
		t.Fatalf("web_search view = %+v, want Egress=true and a Title", *web)
	}
}

// TestUITemplates verifies GET /v1/ui/templates returns the starter presets.
func TestUITemplates(t *testing.T) {
	h := catalogTestServer()
	req := httptest.NewRequest(http.MethodGet, "/v1/ui/templates", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var got []catalog.Template
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("expected at least one template")
	}
}

// TestUIPersonaSections verifies GET /v1/ui/persona-sections returns the persona
// document schema (identity/soul/instructions) with display copy.
func TestUIPersonaSections(t *testing.T) {
	h := catalogTestServer()
	req := httptest.NewRequest(http.MethodGet, "/v1/ui/persona-sections", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var got []catalog.PersonaSection
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != len(catalog.PersonaSections()) || got[0].Key == "" || got[0].Title == "" {
		t.Fatalf("unexpected persona sections: %+v", got)
	}
}

// TestUICatalogBearerGated verifies the catalog endpoints sit behind the bearer gate
// (they're under /v1), so a token-protected daemon doesn't leak them unauthenticated.
func TestUICatalogBearerGated(t *testing.T) {
	h := New(gateway.New(gateway.VerifierChain{gateway.AlwaysRequireHuman{}}, gateway.NewManualApprover(), gateway.NewLogApplier(), gateway.NewMemoryStore())).
		WithToken("secret").Handler()
	for _, path := range []string{"/v1/ui/tools", "/v1/ui/templates", "/v1/ui/persona-sections"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s without token = %d, want 401", path, rec.Code)
		}
	}
}
