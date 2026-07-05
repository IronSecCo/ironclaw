package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/gateway"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
)

func newTestServer() (*httptest.Server, *gateway.MemoryStore) {
	store := gateway.NewMemoryStore()
	gw := gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		store,
	)
	srv := httptest.NewServer(New(gw).Handler())
	return srv, store
}

func TestBearerTokenAuth(t *testing.T) {
	store := gateway.NewMemoryStore()
	gw := gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		store,
	)
	srv := httptest.NewServer(New(gw).WithToken("s3cret").Handler())
	defer srv.Close()

	// No token -> 401 on a protected route.
	resp, err := http.Get(srv.URL + "/v1/changes/pending")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-token status = %d, want 401", resp.StatusCode)
	}

	// Wrong token -> 401.
	req, _ := http.NewRequest("GET", srv.URL+"/v1/changes/pending", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong-token status = %d, want 401", resp.StatusCode)
	}

	// Correct token -> 200.
	req, _ = http.NewRequest("GET", srv.URL+"/v1/changes/pending", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("correct-token status = %d, want 200", resp.StatusCode)
	}

	// /healthz is exempt -> 200 with no token, and carries the public build version
	// (consumed by the live-containment --share receipt; IRO-367).
	resp, _ = http.Get(srv.URL + "/healthz")
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("healthz status = %d, want 200 (must be exempt)", resp.StatusCode)
	}
	var health map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		resp.Body.Close()
		t.Fatalf("healthz body decode: %v", err)
	}
	resp.Body.Close()
	if health["status"] != "ok" {
		t.Fatalf("healthz status field = %q, want ok", health["status"])
	}
	if health["version"] == "" {
		t.Fatalf("healthz must expose a non-empty version field")
	}
}

func TestSubmitPendingApproveFlow(t *testing.T) {
	srv, store := newTestServer()
	defer srv.Close()

	// Submit a change.
	body, _ := json.Marshal(contract.ChangeRequest{Kind: contract.ChangePersona, AgentGroupID: "g1", RequestedBy: "slack:alice"})
	resp, err := http.Post(srv.URL+"/v1/changes", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("submit status = %d, want 202", resp.StatusCode)
	}
	var sr submitResponse
	_ = json.NewDecoder(resp.Body).Decode(&sr)
	resp.Body.Close()
	if sr.ID == "" {
		t.Fatal("no change id returned")
	}

	// It should appear in pending.
	waitPendingCount(t, srv.URL, 1)

	// Approve it.
	dec, _ := json.Marshal(decisionRequest{Outcome: "approve", DecidedBy: "slack:admin"})
	dresp, err := http.Post(srv.URL+"/v1/changes/"+string(sr.ID)+"/decision", "application/json", bytes.NewReader(dec))
	if err != nil {
		t.Fatal(err)
	}
	if dresp.StatusCode != http.StatusOK {
		t.Fatalf("decision status = %d, want 200", dresp.StatusCode)
	}
	dresp.Body.Close()

	// It should leave pending and become applied.
	waitPendingCount(t, srv.URL, 0)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st, ok := store.Status(sr.ID); ok && st == "applied" {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	st, _ := store.Status(sr.ID)
	t.Fatalf("status = %q, want applied", st)
}

// TestSubmitRejectsUnknownGroup verifies the API-boundary input validation from
// IRO-246: when a registry is attached, a change aimed at an agent group that is
// not registered is rejected with 400 instead of silently parked at 202. A change
// aimed at a known group still succeeds, and create_agent (which provisions a new
// group) is exempt.
func TestSubmitRejectsUnknownGroup(t *testing.T) {
	store := gateway.NewMemoryStore()
	gw := gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		store,
	)
	reg := registry.NewMemRegistry()
	if err := reg.PutAgentGroup(registry.AgentGroup{ID: "dev-agent", Name: "Dev Agent", Folder: "dev-agent"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(gw).WithRegistry(reg).Handler())
	defer srv.Close()

	// Unknown group -> 400.
	body, _ := json.Marshal(contract.ChangeRequest{Kind: contract.ChangePersona, AgentGroupID: "nope", RequestedBy: "cli:you"})
	resp, err := http.Post(srv.URL+"/v1/changes", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown-group submit status = %d, want 400", resp.StatusCode)
	}

	// Known group -> 202.
	body, _ = json.Marshal(contract.ChangeRequest{Kind: contract.ChangePersona, AgentGroupID: "dev-agent", RequestedBy: "cli:you"})
	resp, err = http.Post(srv.URL+"/v1/changes", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("known-group submit status = %d, want 202", resp.StatusCode)
	}

	// create_agent is exempt: it provisions a NEW group, so an as-yet-unknown
	// target must not be rejected here.
	body, _ = json.Marshal(contract.ChangeRequest{Kind: contract.ChangeCreateAgent, AgentGroupID: "brand-new", RequestedBy: "cli:you"})
	resp, err = http.Post(srv.URL+"/v1/changes", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("create_agent submit status = %d, want 202 (exempt from group check)", resp.StatusCode)
	}
}

func TestHistoryEndpoint(t *testing.T) {
	// A FileStore-backed gateway exposes change history.
	dir := t.TempDir()
	store, err := gateway.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		store,
	)
	srv := httptest.NewServer(New(gw).WithHistory(store).Handler())
	defer srv.Close()

	// Empty history initially.
	if n := historyCount(t, srv.URL); n != 0 {
		t.Fatalf("history should start empty, got %d", n)
	}

	// Submit + approve a change so it moves to applied (and thus into history).
	body, _ := json.Marshal(contract.ChangeRequest{ID: "chg_hist", Kind: contract.ChangePersona, AgentGroupID: "g1", RequestedBy: "slack:alice"})
	resp, err := http.Post(srv.URL+"/v1/changes", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	waitPendingCount(t, srv.URL, 1)

	dec, _ := json.Marshal(decisionRequest{Outcome: "approve", DecidedBy: "slack:admin"})
	dresp, err := http.Post(srv.URL+"/v1/changes/chg_hist/decision", "application/json", bytes.NewReader(dec))
	if err != nil {
		t.Fatal(err)
	}
	dresp.Body.Close()
	waitPendingCount(t, srv.URL, 0)

	// History now contains the change. Wait for the terminal "applied" state, not
	// merely its appearance in history. The gateway applies in a background
	// goroutine (handleSubmit), and a change shows up in History() as soon as it
	// leaves "pending" — i.e. at "approved", BEFORE the apply step's final
	// MarkApplied persist. Returning then lets that last FileStore write race
	// t.TempDir()'s RemoveAll cleanup ("directory not empty"). FileStore
	// serializes every write under its mutex and MarkApplied is the terminal one,
	// so once Status reports "applied" the temp dir is settled and cleanup is safe.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st, ok := store.Status("chg_hist"); ok && st == "applied" {
			if n := historyCount(t, srv.URL); n != 1 {
				t.Fatalf("history = %d, want 1", n)
			}
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	st, _ := store.Status("chg_hist")
	t.Fatalf("change never reached applied (status=%q); history=%d", st, historyCount(t, srv.URL))
}

func historyCount(t *testing.T, base string) int {
	t.Helper()
	resp, err := http.Get(base + "/v1/changes/history")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var hist []gateway.HistoryEntry
	_ = json.NewDecoder(resp.Body).Decode(&hist)
	return len(hist)
}

func TestAuditEndpointEmptyWithoutPath(t *testing.T) {
	srv, _ := newTestServer()
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/v1/audit")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit status = %d, want 200", resp.StatusCode)
	}
	var entries []gateway.AuditEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("audit should be empty without a path, got %d", len(entries))
	}
}

func TestDecisionBadOutcome(t *testing.T) {
	srv, _ := newTestServer()
	defer srv.Close()
	dec, _ := json.Marshal(map[string]string{"outcome": "maybe"})
	resp, err := http.Post(srv.URL+"/v1/changes/x/decision", "application/json", bytes.NewReader(dec))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func waitPendingCount(t *testing.T, base string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/v1/changes/pending")
		if err != nil {
			t.Fatal(err)
		}
		var pending []contract.ChangeRequest
		_ = json.NewDecoder(resp.Body).Decode(&pending)
		resp.Body.Close()
		if len(pending) == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("pending count never reached %d", want)
}
