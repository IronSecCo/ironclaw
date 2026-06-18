package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/gateway"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
)

func newSessionsTestEnv(t *testing.T) (*registry.MemRegistry, *gateway.Gateway, contract.SessionID) {
	t.Helper()
	gw := gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		gateway.NewMemoryStore(),
	)
	reg := registry.NewMemRegistry()
	if err := reg.PutAgentGroup(registry.AgentGroup{ID: "grp1", Name: "Research Bot"}); err != nil {
		t.Fatal(err)
	}
	sess, err := reg.ResolveSession("grp1", "mg1", nil, contract.SessionShared)
	if err != nil {
		t.Fatal(err)
	}
	return reg, gw, sess.ID
}

func TestUISessionsListEnriched(t *testing.T) {
	reg, gw, id := newSessionsTestEnv(t)
	h := New(gw).WithRegistry(reg).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ui/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/ui/sessions: got %d, want 200", rec.Code)
	}
	var views []sessionView
	if err := json.Unmarshal(rec.Body.Bytes(), &views); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if len(views) != 1 {
		t.Fatalf("got %d views, want 1", len(views))
	}
	if views[0].ID != id || views[0].AgentGroupName != "Research Bot" {
		t.Errorf("unexpected view: %+v", views[0])
	}
}

func TestUITerminateCallsHost(t *testing.T) {
	reg, gw, id := newSessionsTestEnv(t)

	var gotID contract.SessionID
	terminator := func(_ context.Context, sid contract.SessionID) error {
		gotID = sid
		return nil
	}
	h := New(gw).WithRegistry(reg).WithSessionTerminator(terminator).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/ui/sessions/"+string(id)+"/terminate", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate: got %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if gotID != id {
		t.Errorf("terminator called with %q, want %q", gotID, id)
	}
}

func TestUITerminateUnwiredIs503(t *testing.T) {
	reg, gw, id := newSessionsTestEnv(t)
	h := New(gw).WithRegistry(reg).Handler() // no terminator wired

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/ui/sessions/"+string(id)+"/terminate", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("unwired terminate: got %d, want 503", rec.Code)
	}
}

func TestUISessionsRequireToken(t *testing.T) {
	reg, gw, id := newSessionsTestEnv(t)
	h := New(gw).WithRegistry(reg).WithSessionTerminator(
		func(context.Context, contract.SessionID) error { return nil }).WithToken("s3cret").Handler()

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/v1/ui/sessions"},
		{http.MethodPost, "/v1/ui/sessions/" + string(id) + "/terminate"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token: got %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}
