package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nivardsec/ironclaw/internal/host/gateway"
	"github.com/nivardsec/ironclaw/internal/host/registry"
)

func newConfigTestEnv(t *testing.T) (*registry.MemRegistry, *gateway.Gateway, *gateway.MemoryStore) {
	t.Helper()
	store := gateway.NewMemoryStore()
	gw := gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		store,
	)
	reg := registry.NewMemRegistry()
	if err := reg.PutAgentGroup(registry.AgentGroup{ID: "ag1", Name: "Alpha", Folder: "/a"}); err != nil {
		t.Fatal(err)
	}
	return reg, gw, store
}

// TestUIOnboardReturnsStepsNoToken: the wizard endpoint mirrors `ironctl onboard`
// steps and must never leak a token to the browser.
func TestUIOnboardReturnsStepsNoToken(t *testing.T) {
	_, gw, _ := newConfigTestEnv(t)
	h := New(gw).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ui/onboard", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/ui/onboard: got %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// Step names from the onboard wizard.
	for _, name := range []string{"runtime", "api-token", "model-credential", "verify"} {
		if !strings.Contains(body, name) {
			t.Errorf("onboard steps missing %q: %s", name, body)
		}
	}
	// No token field should ever appear.
	var parsed struct {
		Steps []map[string]any `json:"steps"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Steps) == 0 {
		t.Error("expected onboard steps")
	}
	if strings.Contains(strings.ToLower(body), "\"token\"") {
		t.Error("onboard response must not contain a token field")
	}
}

func TestUIConfigGet(t *testing.T) {
	reg, gw, _ := newConfigTestEnv(t)
	h := New(gw).WithRegistry(reg).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ui/config/ag1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/ui/config/ag1: got %d, want 200", rec.Code)
	}
	var view configView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.AgentGroup.ID != "ag1" || view.AgentGroup.Name != "Alpha" {
		t.Errorf("agent group wrong: %+v", view.AgentGroup)
	}
	if view.AppliedChanges == nil {
		t.Error("appliedChanges should be a (possibly empty) array, not null")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ui/config/ghost", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown group: got %d, want 404", rec.Code)
	}
}

// TestUIConfigChangeRoutesThroughGateway: a valid capability change is accepted
// (202) and lands on the gateway's pending list — never auto-applied.
func TestUIConfigChangeRoutesThroughGateway(t *testing.T) {
	reg, gw, store := newConfigTestEnv(t)
	h := New(gw).WithRegistry(reg).Handler()

	body := `{"kind":"persona","agentGroupID":"ag1","requestedBy":"console","after":{"instructions":"be terse"}}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/ui/config/change", strings.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST config change: got %d, want 202 (%s)", rec.Code, rec.Body.String())
	}

	// The change must be held for human approval (pending), not applied. Submit is
	// async (goroutine), so poll briefly.
	got := false
	for i := 0; i < 100; i++ {
		if p, _ := store.Pending(); len(p) == 1 {
			got = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !got {
		t.Fatal("submitted capability change never reached the gateway pending list")
	}
}

// TestUIConfigChangeValidation: non-capability kinds and missing fields are
// rejected before anything is submitted.
func TestUIConfigChangeValidation(t *testing.T) {
	reg, gw, _ := newConfigTestEnv(t)
	h := New(gw).WithRegistry(reg).Handler()

	cases := []struct {
		name, body string
	}{
		{"create_agent rejected", `{"kind":"create_agent","agentGroupID":"ag1","after":{"x":1}}`},
		{"unknown kind rejected", `{"kind":"banana","agentGroupID":"ag1","after":{"x":1}}`},
		{"missing group", `{"kind":"persona","agentGroupID":"","after":{"x":1}}`},
		{"missing after", `{"kind":"persona","agentGroupID":"ag1"}`},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/ui/config/change", strings.NewReader(c.body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400 (%s)", c.name, rec.Code, rec.Body.String())
		}
	}
}

func TestUIConfigRequiresToken(t *testing.T) {
	reg, gw, _ := newConfigTestEnv(t)
	h := New(gw).WithRegistry(reg).WithToken("s3cret").Handler()
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/v1/ui/onboard"},
		{http.MethodGet, "/v1/ui/config/ag1"},
		{http.MethodPost, "/v1/ui/config/change"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token: got %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}
