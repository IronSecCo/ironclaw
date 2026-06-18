package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/host/gateway"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
)

func newRegServer(t *testing.T, reg registry.Registry, token string) *httptest.Server {
	t.Helper()
	gw := gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		gateway.NewMemoryStore(),
	)
	s := New(gw).WithToken(token)
	if reg != nil {
		s = s.WithRegistry(reg)
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, srv *httptest.Server, method, path, body, token string) (*http.Response, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

func TestRegistryAdminAgentGroupAndUserRoundTrip(t *testing.T) {
	reg := registry.NewMemRegistry()
	srv := newRegServer(t, reg, "")

	// Upsert an agent group, then read it back.
	resp, body := do(t, srv, http.MethodPut, "/v1/registry/agent-groups/ag1", `{"name":"Alpha","folder":"/a"}`, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT agent-group = %d (%s)", resp.StatusCode, body)
	}
	resp, body = do(t, srv, http.MethodGet, "/v1/registry/agent-groups/ag1", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET agent-group = %d", resp.StatusCode)
	}
	var g registry.AgentGroup
	if err := json.Unmarshal(body, &g); err != nil {
		t.Fatal(err)
	}
	if g.ID != "ag1" || g.Name != "Alpha" || g.Folder != "/a" {
		t.Fatalf("agent group round-trip wrong: %+v", g)
	}

	// 404 for a missing one.
	if resp, _ := do(t, srv, http.MethodGet, "/v1/registry/agent-groups/nope", "", ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing agent group = %d, want 404", resp.StatusCode)
	}

	// User upsert + get.
	resp, _ = do(t, srv, http.MethodPut, "/v1/registry/users/slack:u1", `{"kind":"user","displayName":"One"}`, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT user = %d", resp.StatusCode)
	}
	resp, body = do(t, srv, http.MethodGet, "/v1/registry/users/slack:u1", "", "")
	var u registry.User
	json.Unmarshal(body, &u)
	if resp.StatusCode != http.StatusOK || u.ID != "slack:u1" || u.DisplayName != "One" {
		t.Fatalf("user round-trip wrong: %d %+v", resp.StatusCode, u)
	}
}

func TestRegistryAdminMessagingGroupAndWiring(t *testing.T) {
	reg := registry.NewMemRegistry()
	srv := newRegServer(t, reg, "")

	resp, body := do(t, srv, http.MethodPost, "/v1/registry/messaging-groups",
		`{"channelType":"slack","platformID":"C1","isGroup":true}`, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create messaging-group = %d (%s)", resp.StatusCode, body)
	}
	var mg registry.MessagingGroup
	json.Unmarshal(body, &mg)
	if mg.ID == "" || mg.ChannelType != "slack" {
		t.Fatalf("messaging group wrong: %+v", mg)
	}

	// Create a wiring on it; the server assigns an id and returns it.
	wbody := `{"messagingGroupID":"` + string(mg.ID) + `","agentGroupID":"ag1","engageMode":"mention","sessionMode":"shared","priority":3}`
	resp, body = do(t, srv, http.MethodPost, "/v1/registry/wirings", wbody, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create wiring = %d (%s)", resp.StatusCode, body)
	}
	var wr registry.Wiring
	json.Unmarshal(body, &wr)
	if wr.ID == "" {
		t.Fatal("wiring should get a server-assigned id")
	}

	// List wirings for the messaging group.
	resp, body = do(t, srv, http.MethodGet, "/v1/registry/messaging-groups/"+string(mg.ID)+"/wirings", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list wirings = %d", resp.StatusCode)
	}
	var ws []registry.Wiring
	json.Unmarshal(body, &ws)
	if len(ws) != 1 || ws[0].ID != wr.ID {
		t.Fatalf("list wirings wrong: %+v", ws)
	}
}

func TestRegistryAdminRolesMembersAccess(t *testing.T) {
	reg := registry.NewMemRegistry()
	srv := newRegServer(t, reg, "")

	access := func() (bool, string) {
		_, body := do(t, srv, http.MethodGet, "/v1/registry/access?userID=slack:u1&agentGroupID=ag1", "", "")
		var out struct {
			Allowed bool   `json:"allowed"`
			Reason  string `json:"reason"`
		}
		json.Unmarshal(body, &out)
		return out.Allowed, out.Reason
	}

	if ok, _ := access(); ok {
		t.Fatal("no access before any grant")
	}

	// Grant a scoped admin role → access allowed.
	resp, body := do(t, srv, http.MethodPost, "/v1/registry/roles",
		`{"userID":"slack:u1","role":"admin","agentGroupID":"ag1"}`, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("grant role = %d (%s)", resp.StatusCode, body)
	}
	if ok, reason := access(); !ok || reason != "scoped-admin" {
		t.Fatalf("after grant: ok=%v reason=%q", ok, reason)
	}

	// Revoke it → access denied.
	resp, _ = do(t, srv, http.MethodPost, "/v1/registry/roles/revoke",
		`{"userID":"slack:u1","role":"admin","agentGroupID":"ag1"}`, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke role = %d", resp.StatusCode)
	}
	if ok, _ := access(); ok {
		t.Fatal("access should be gone after revoke")
	}

	// Membership also grants access; removal revokes it.
	do(t, srv, http.MethodPost, "/v1/registry/members", `{"userID":"slack:u1","agentGroupID":"ag1"}`, "")
	if ok, reason := access(); !ok || reason != "member" {
		t.Fatalf("after add member: ok=%v reason=%q", ok, reason)
	}
	do(t, srv, http.MethodPost, "/v1/registry/members/remove", `{"userID":"slack:u1","agentGroupID":"ag1"}`, "")
	if ok, _ := access(); ok {
		t.Fatal("access should be gone after removing member")
	}
}

func TestRegistryAdminDestinations(t *testing.T) {
	reg := registry.NewMemRegistry()
	srv := newRegServer(t, reg, "")

	check := func() bool {
		_, body := do(t, srv, http.MethodGet, "/v1/registry/destinations/check?agentGroupID=ag1&channelType=slack&platformID=C2", "", "")
		var out struct {
			Allowed bool `json:"allowed"`
		}
		json.Unmarshal(body, &out)
		return out.Allowed
	}
	if check() {
		t.Fatal("destination not allowed before adding")
	}
	resp, _ := do(t, srv, http.MethodPost, "/v1/registry/destinations",
		`{"agentGroupID":"ag1","channelType":"slack","platformID":"C2"}`, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add destination = %d", resp.StatusCode)
	}
	if !check() {
		t.Fatal("destination should be allowed after adding")
	}
}

func TestRegistryAdminUnconfiguredReturns503(t *testing.T) {
	srv := newRegServer(t, nil, "") // no registry attached
	resp, _ := do(t, srv, http.MethodGet, "/v1/registry/sessions", "", "")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("registry endpoint without registry = %d, want 503", resp.StatusCode)
	}
}

func TestRegistryAdminRequiresAuth(t *testing.T) {
	reg := registry.NewMemRegistry()
	srv := newRegServer(t, reg, "secret-token")

	// No bearer → 401.
	if resp, _ := do(t, srv, http.MethodGet, "/v1/registry/sessions", "", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", resp.StatusCode)
	}
	// Correct bearer → 200.
	if resp, _ := do(t, srv, http.MethodGet, "/v1/registry/sessions", "", "secret-token"); resp.StatusCode != http.StatusOK {
		t.Fatalf("with token = %d, want 200", resp.StatusCode)
	}
}
