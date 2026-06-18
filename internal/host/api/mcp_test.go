package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nivardsec/ironclaw/internal/host/gateway"
	"github.com/nivardsec/ironclaw/internal/host/mcp"
	"github.com/nivardsec/ironclaw/internal/host/registry"
)

func mcpTestServer(reg registry.Registry, catalog *mcp.Catalog, broker *mcp.Broker) http.Handler {
	gw := gateway.New(gateway.VerifierChain{gateway.AlwaysRequireHuman{}}, gateway.NewManualApprover(), gateway.NewLogApplier(), gateway.NewMemoryStore())
	s := New(gw).WithRegistry(reg)
	if catalog != nil {
		s = s.WithMCP(catalog, broker)
	}
	return s.Handler()
}

func TestMCP_DisabledReturns503(t *testing.T) {
	h := mcpTestServer(registry.NewMemRegistry(), nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/registry/mcp-servers", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when MCP disabled", rec.Code)
	}
}

func TestMCP_CRUDAndMasking(t *testing.T) {
	cat, _ := mcp.NewCatalog("")
	h := mcpTestServer(registry.NewMemRegistry(), cat, nil)

	// PUT a remote server with a raw secret header.
	put := `{"transport":"http","url":"https://mcp.example.com/rpc","headers":{"Authorization":"Bearer sk-secret"}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/registry/mcp-servers/github", strings.NewReader(put))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d (%s)", rec.Code, rec.Body.String())
	}
	// The PUT response masks the secret.
	if strings.Contains(rec.Body.String(), "sk-secret") {
		t.Fatalf("PUT response leaked the secret: %s", rec.Body.String())
	}

	// LIST masks the secret too, but stores it (the catalog has the raw value).
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/registry/mcp-servers", nil))
	var list []mcp.ServerConfig
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Name != "github" {
		t.Fatalf("list = %+v, want one github server", list)
	}
	if list[0].Headers["Authorization"] != mcp.MaskString {
		t.Fatalf("listed secret not masked: %q", list[0].Headers["Authorization"])
	}
	if raw, _ := cat.Get("github"); raw.Headers["Authorization"] != "Bearer sk-secret" {
		t.Fatalf("catalog should store the raw secret, got %q", raw.Headers["Authorization"])
	}

	// EDIT without re-entering the secret (send the mask) preserves it.
	edit := `{"transport":"http","url":"https://mcp.example.com/v2","headers":{"Authorization":"` + mcp.MaskString + `"}}`
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/v1/registry/mcp-servers/github", strings.NewReader(edit)))
	if rec.Code != http.StatusOK {
		t.Fatalf("edit PUT status = %d (%s)", rec.Code, rec.Body.String())
	}
	raw, _ := cat.Get("github")
	if raw.Headers["Authorization"] != "Bearer sk-secret" {
		t.Fatalf("editing with the mask should preserve the secret, got %q", raw.Headers["Authorization"])
	}
	if raw.URL != "https://mcp.example.com/v2" {
		t.Fatalf("edit did not update the url: %q", raw.URL)
	}

	// Invalid config is rejected.
	bad := `{"transport":"stdio"}` // no command
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/v1/registry/mcp-servers/bad", strings.NewReader(bad)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid PUT status = %d, want 400", rec.Code)
	}

	// DELETE removes it.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/v1/registry/mcp-servers/github", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", rec.Code)
	}
	if _, ok := cat.Get("github"); ok {
		t.Fatal("server still present after delete")
	}
}

func TestMCP_ProbeAndReadModel(t *testing.T) {
	upstream := httptest.NewServer(mcp.SampleServer().Handler())
	defer upstream.Close()

	cat, _ := mcp.NewCatalog("")
	_ = cat.Put(mcp.ServerConfig{Name: "sample", Transport: mcp.TransportHTTP, URL: upstream.URL})
	broker := mcp.New(context.Background(), cat, func(string) []mcp.Grant { return nil })
	defer broker.Close()

	reg := registry.NewMemRegistry()
	_ = reg.PutAgentGroup(registry.AgentGroup{ID: "team", Name: "Team"})
	_ = registry.SetGrantedMCP(reg, "team", "sample", []string{"echo"})

	h := mcpTestServer(reg, cat, broker)

	// Probe discovers the server's tools.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/registry/mcp-servers/sample/probe", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("probe status = %d (%s)", rec.Code, rec.Body.String())
	}
	var probe struct {
		Tools []mcp.Tool `json:"tools"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &probe)
	if len(probe.Tools) != 2 {
		t.Fatalf("probe found %d tools, want 2 (echo+add)", len(probe.Tools))
	}

	// Read-model shows the server and the team's grant.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ui/mcp-servers", nil))
	var view []mcpServerView
	_ = json.Unmarshal(rec.Body.Bytes(), &view)
	if len(view) != 1 || view[0].Server.Name != "sample" {
		t.Fatalf("read-model = %+v, want one sample server", view)
	}
	if len(view[0].Grants) != 1 || view[0].Grants[0].AgentGroupID != "team" || view[0].Grants[0].Tools[0] != "echo" {
		t.Fatalf("read-model grants = %+v, want team->[echo]", view[0].Grants)
	}
}
