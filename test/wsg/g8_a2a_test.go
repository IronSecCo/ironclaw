//go:build wsg_verify

package wsg

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/gateway"
)

// TestG8_CreateAgent_SpawnsGatedChild proves the a2a row: create_agent rides the
// real gateway, is HELD for a human (a new agent is a new trust principal), and on
// approval the wired creator materializes the child agent group. A malformed
// request is rejected outright and never materializes.
func TestG8_CreateAgent_SpawnsGatedChild(t *testing.T) {
	created := map[contract.AgentGroupID]string{} // id -> folder, populated only on apply
	createFn := func(id contract.AgentGroupID, name, folder string) error {
		created[id] = folder
		return nil
	}

	store := gateway.NewMemoryStore()
	gw := gateway.New(
		gateway.VerifierChain{
			gateway.NewCreateAgentVerifier(func(contract.AgentGroupID) bool { return false }),
			gateway.AlwaysRequireHuman{},
		},
		gateway.NewManualApprover(),
		gateway.NewCreateAgentApplier(createFn, gateway.NewLogApplier()),
		store,
	)

	// --- Negative: a malformed create_agent is rejected, never materialized. ---
	bad, _ := json.Marshal(map[string]string{"name": "../escape"})
	if _, err := gw.Submit(context.Background(), contract.ChangeRequest{
		Kind: contract.ChangeCreateAgent, After: bad,
	}); err != nil {
		t.Fatalf("Submit(malformed) returned transport error: %v", err)
	}
	if len(created) != 0 {
		t.Fatalf("unsafe create_agent materialized an agent: %v", created)
	}

	// --- Positive: a valid create_agent is gated, then spawns on approval. ---
	good, _ := json.Marshal(map[string]string{"name": "Researcher", "folder": "research"})
	done := make(chan error, 1)
	go func() {
		_, e := gw.Submit(context.Background(), contract.ChangeRequest{
			Kind: contract.ChangeCreateAgent, After: good,
		})
		done <- e
	}()

	id := waitForPending(t, store)
	if len(created) != 0 {
		t.Fatalf("child spawned before approval: %v — create_agent must be gated", created)
	}

	if err := gw.Decide(id, contract.Decision{
		Outcome:   gateway.OutcomeApprove,
		DecidedBy: "board:omer",
		DecidedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Submit after approval: %v", err)
	}

	if created["research"] != "research" {
		t.Fatalf("approved create_agent did not spawn the child group: %v", created)
	}
	t.Logf("G8 a2a: create_agent held for human then spawned gated child group %q", "research")
}
