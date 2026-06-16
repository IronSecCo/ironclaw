// OWNER: T-013b

package parity

import (
	"context"
	"testing"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/gateway"
)

// TestGatewayMandatoryApproval is the behavioral contract: under the v1
// AlwaysRequireHuman floor, every control-plane mutation is held pending a human
// decision — nothing applies until a human approves, and then it applies exactly
// once.
func TestGatewayMandatoryApproval(t *testing.T) {
	approver := gateway.NewManualApprover()
	applier := gateway.NewLogApplier()
	store := gateway.NewMemoryStore()
	gw := gateway.New(gateway.VerifierChain{gateway.AlwaysRequireHuman{}}, approver, applier, store)

	// Submit blocks until the change is decided; run it off the test goroutine.
	done := make(chan error, 1)
	go func() {
		_, err := gw.Submit(context.Background(), contract.ChangeRequest{ID: "c1", Kind: contract.ChangeWiring})
		done <- err
	}()

	// Every mutation lands pending a human first.
	pending := waitCapPending(t, gw, 1)
	if pending[0].ID != "c1" {
		t.Fatalf("submitted change should be the pending one: %+v", pending)
	}
	// Nothing applies while it waits.
	if applied := applier.Applied(); len(applied) != 0 {
		t.Fatalf("no change may apply before a human decision, applied=%v", applied)
	}

	// Approve it: Submit returns and the change applies exactly once.
	if err := gw.Decide("c1", contract.Decision{Outcome: gateway.OutcomeApprove, DecidedBy: "admin", DecidedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("decide: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("submit returned error: %v", err)
	}
	if applied := applier.Applied(); len(applied) != 1 || applied[0] != "c1" {
		t.Fatalf("approved change should apply exactly once, applied=%v", applied)
	}
	// No longer pending once applied.
	if p, _ := gw.Pending(); len(p) != 0 {
		t.Fatalf("applied change should no longer be pending: %+v", p)
	}
}
