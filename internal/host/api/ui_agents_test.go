package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nivardsec/ironclaw/internal/host/gateway"
	"github.com/nivardsec/ironclaw/internal/host/registry"
)

// TestUIAgentsLists verifies GET /v1/ui/agents returns every agent group (the
// picker/builder read-model) with the registry fields the console renders.
func TestUIAgentsLists(t *testing.T) {
	reg := registry.NewMemRegistry()
	if err := reg.PutAgentGroup(registry.AgentGroup{ID: "beta", Name: "Beta", Folder: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.PutAgentGroup(registry.AgentGroup{ID: "alpha", Name: "Alpha", Folder: "a", Model: "claude-x"}); err != nil {
		t.Fatal(err)
	}
	h := New(gateway.New(gateway.VerifierChain{gateway.AlwaysRequireHuman{}}, gateway.NewManualApprover(), gateway.NewLogApplier(), gateway.NewMemoryStore())).
		WithRegistry(reg).Handler()

	req := httptest.NewRequest(http.MethodGet, "/v1/ui/agents", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	var got []agentView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d agents, want 2", len(got))
	}
	// ListAgentGroups orders by ID, so alpha precedes beta.
	if got[0].ID != "alpha" || got[1].ID != "beta" {
		t.Fatalf("order = %q,%q, want alpha,beta", got[0].ID, got[1].ID)
	}
	if got[0].Name != "Alpha" || got[0].Model != "claude-x" {
		t.Fatalf("alpha view = %+v, want Name=Alpha Model=claude-x", got[0])
	}
}

// TestUIMessagingGroupsLists verifies GET /v1/ui/messaging-groups returns every
// connected surface (the picker read-model) with its wiring count.
func TestUIMessagingGroupsLists(t *testing.T) {
	reg := registry.NewMemRegistry()
	if _, err := reg.GetOrCreateMessagingGroup("slack", "C9", "", true, "strict"); err != nil {
		t.Fatal(err)
	}
	h := New(gateway.New(gateway.VerifierChain{gateway.AlwaysRequireHuman{}}, gateway.NewManualApprover(), gateway.NewLogApplier(), gateway.NewMemoryStore())).
		WithRegistry(reg).Handler()

	req := httptest.NewRequest(http.MethodGet, "/v1/ui/messaging-groups", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var got []messagingGroupView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].ChannelType != "slack" || got[0].PlatformID != "C9" {
		t.Fatalf("got %+v, want one slack/C9 surface", got)
	}
}
