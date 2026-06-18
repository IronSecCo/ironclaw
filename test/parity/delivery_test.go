package parity

import (
	"context"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/gateway"
)

// TestDeliveryDedup is the behavioral contract: an outbound message is delivered
// to its channel exactly once, even across repeated delivery polls — the host
// dedups by message id rather than re-sending.
func TestDeliveryDedup(t *testing.T) {
	d, _, adapter, w := newCapDelivery(t, gateway.VerifierChain{gateway.AlwaysRequireHuman{}})

	ct, pid := "fake", "C1"
	if err := w.WriteMessageOut(contract.MessageOut{
		ID: "o1", Seq: 1, Kind: contract.KindChat, ChannelType: &ct, PlatformID: &pid, Content: "hi",
	}); err != nil {
		t.Fatalf("sandbox write outbound: %v", err)
	}

	// Two polls must not double-send.
	for i := 0; i < 2; i++ {
		if err := d.Poll(context.Background()); err != nil {
			t.Fatalf("poll %d: %v", i, err)
		}
	}

	if got := adapter.Delivered(); len(got) != 1 {
		t.Fatalf("outbound must deliver exactly once across polls, delivered %d: %+v", len(got), got)
	}
	if d.DeliveredCount() != 1 {
		t.Fatalf("dedup set size = %d, want 1", d.DeliveredCount())
	}
}
