package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/gateway"
	"github.com/nivardsec/ironclaw/internal/host/registry"
)

func newChannelsTestEnv(t *testing.T) (*registry.MemRegistry, *gateway.Gateway, contract.MessagingGroupID) {
	t.Helper()
	gw := gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		gateway.NewMemoryStore(),
	)
	reg := registry.NewMemRegistry()
	if err := reg.PutAgentGroup(registry.AgentGroup{ID: "ag1", Name: "Alpha"}); err != nil {
		t.Fatal(err)
	}
	mg, err := reg.GetOrCreateMessagingGroup("slack", "C123", "", true, contract.UnknownStrict)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.PutWiring(registry.Wiring{
		MessagingGroupID: mg.ID, AgentGroupID: "ag1",
		EngageMode: contract.EngagePattern, EngagePattern: ".", SessionMode: contract.SessionShared,
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddDestination("ag1", "slack", "C999"); err != nil {
		t.Fatal(err)
	}
	return reg, gw, mg.ID
}

func TestUIChannelEnriched(t *testing.T) {
	reg, gw, mgID := newChannelsTestEnv(t)
	h := New(gw).WithRegistry(reg).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ui/channels/"+string(mgID), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/ui/channels/{id}: got %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var view channelsView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.MessagingGroup.ID != mgID {
		t.Errorf("messaging group id = %q, want %q", view.MessagingGroup.ID, mgID)
	}
	if len(view.Wirings) != 1 {
		t.Fatalf("got %d wirings, want 1", len(view.Wirings))
	}
	if view.Wirings[0].AgentGroupName != "Alpha" {
		t.Errorf("wiring agentGroupName = %q, want Alpha", view.Wirings[0].AgentGroupName)
	}
}

func TestUIChannelNotFound(t *testing.T) {
	reg, gw, _ := newChannelsTestEnv(t)
	h := New(gw).WithRegistry(reg).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ui/channels/does-not-exist", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown messaging group: got %d, want 404", rec.Code)
	}
}

func TestUIDestinationsList(t *testing.T) {
	reg, gw, _ := newChannelsTestEnv(t)
	h := New(gw).WithRegistry(reg).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ui/destinations/ag1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/ui/destinations/ag1: got %d, want 200", rec.Code)
	}
	var dests []registry.Destination
	if err := json.Unmarshal(rec.Body.Bytes(), &dests); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(dests) != 1 || dests[0].ChannelType != "slack" || dests[0].PlatformID != "C999" {
		t.Errorf("destinations = %+v, want one slack/C999", dests)
	}

	// An agent group with no destinations returns an empty (non-null) array.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ui/destinations/nobody", nil))
	if rec.Code != http.StatusOK || rec.Body.String() == "null\n" {
		t.Errorf("empty destinations: code=%d body=%q, want 200 with []", rec.Code, rec.Body.String())
	}
}

func TestUIChannelsRequireToken(t *testing.T) {
	reg, gw, mgID := newChannelsTestEnv(t)
	h := New(gw).WithRegistry(reg).WithToken("s3cret").Handler()
	for _, path := range []string{"/v1/ui/channels/" + string(mgID), "/v1/ui/destinations/ag1"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without token: got %d, want 401", path, rec.Code)
		}
	}
}
