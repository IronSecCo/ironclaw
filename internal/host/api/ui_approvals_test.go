package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/gateway"
	"github.com/nivardsec/ironclaw/internal/host/registry"
)

// seedPending inserts a pending change directly into the store so the read-model
// has something to project, without driving the blocking Submit flow.
func seedPending(store *gateway.MemoryStore, c contract.ChangeRequest) error {
	return store.Put(c)
}

func TestUIApprovalsEnrichesNames(t *testing.T) {
	store := gateway.NewMemoryStore()
	gw := gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		store,
	)
	reg := registry.NewMemRegistry()
	if err := reg.PutAgentGroup(registry.AgentGroup{ID: "grp1", Name: "Research Bot"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.PutUser(registry.User{ID: "user1", Kind: "human", DisplayName: "Ada Lovelace"}); err != nil {
		t.Fatal(err)
	}
	if err := seedPending(store, contract.ChangeRequest{
		ID:           "chg1",
		Kind:         contract.ChangePersona,
		AgentGroupID: "grp1",
		RequestedBy:  "user1",
		After:        json.RawMessage(`{"persona":"helpful"}`),
		CreatedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	h := New(gw).WithRegistry(reg).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ui/approvals", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/ui/approvals: got %d, want 200", rec.Code)
	}

	var views []approvalView
	if err := json.Unmarshal(rec.Body.Bytes(), &views); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, rec.Body.String())
	}
	if len(views) != 1 {
		t.Fatalf("got %d views, want 1", len(views))
	}
	v := views[0]
	if v.ID != "chg1" || v.Kind != contract.ChangePersona {
		t.Errorf("unexpected id/kind: %+v", v)
	}
	if v.AgentGroupName != "Research Bot" {
		t.Errorf("agentGroupName = %q, want %q", v.AgentGroupName, "Research Bot")
	}
	if v.RequestedByName != "Ada Lovelace" {
		t.Errorf("requestedByName = %q, want %q", v.RequestedByName, "Ada Lovelace")
	}
	if string(v.After) != `{"persona":"helpful"}` {
		t.Errorf("after payload = %s", v.After)
	}
}

// TestUIApprovalsUnknownIDsDegrade: with no registry attached, the endpoint still
// returns the change with empty names (UI falls back to the raw id) — never 500s.
func TestUIApprovalsUnknownIDsDegrade(t *testing.T) {
	store := gateway.NewMemoryStore()
	gw := gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		store,
	)
	if err := seedPending(store, contract.ChangeRequest{
		ID: "chg2", Kind: contract.ChangeWiring, AgentGroupID: "ghost", RequestedBy: "nobody",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	h := New(gw).Handler() // no registry
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ui/approvals", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	var views []approvalView
	if err := json.Unmarshal(rec.Body.Bytes(), &views); err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].AgentGroupName != "" || views[0].RequestedByName != "" {
		t.Errorf("expected one view with empty names, got %+v", views)
	}
}

// TestUIApprovalsRequiresToken: the read-model lives under /v1, so it stays
// bearer-gated (unlike the static /ui/ shell).
func TestUIApprovalsRequiresToken(t *testing.T) {
	store := gateway.NewMemoryStore()
	gw := gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		store,
	)
	h := New(gw).WithToken("s3cret").Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ui/approvals", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /v1/ui/approvals without token: got %d, want 401", rec.Code)
	}
}
