// OWNER: AGENT1

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/gateway"
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

	// /healthz is exempt -> 200 with no token.
	resp, _ = http.Get(srv.URL + "/healthz")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200 (must be exempt)", resp.StatusCode)
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
