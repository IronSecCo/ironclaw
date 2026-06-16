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
