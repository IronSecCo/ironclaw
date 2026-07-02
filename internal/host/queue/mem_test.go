package queue

import (
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

func TestMemInboundSeqParity(t *testing.T) {
	store := NewMemStore()
	in := NewMemInbound(store)
	// Explicit even seq accepted (host parity).
	if err := in.WriteMessageIn(contract.MessageIn{ID: "b", Seq: 2}); err != nil {
		t.Fatal(err)
	}
	// Odd seq rejected.
	if err := in.WriteMessageIn(contract.MessageIn{ID: "c", Seq: 1}); err == nil {
		t.Fatal("odd seq should be rejected on inbound (host writes even)")
	}
}

// TestMemInboundSeqAllocation asserts the writer is the authoritative even-seq
// allocator: Seq==0 callers each get a distinct even seq (MAX+2), so two
// independent host writers never collide (the IRO-278 root cause).
func TestMemInboundSeqAllocation(t *testing.T) {
	store := NewMemStore()
	in := NewMemInbound(store)
	if err := in.WriteMessageIn(contract.MessageIn{ID: "a", Seq: 0}); err != nil {
		t.Fatal(err)
	}
	if err := in.WriteMessageIn(contract.MessageIn{ID: "b", Seq: 0}); err != nil {
		t.Fatal(err)
	}
	got, _ := in.PendingMessages(true)
	if len(got) != 2 || got[0].Seq != 2 || got[1].Seq != 4 {
		t.Fatalf("auto-allocated seqs should be 2 then 4, got %+v", got)
	}
}

func TestMemOutboundSeqParity(t *testing.T) {
	store := NewMemStore()
	out := NewMemOutbound(store)
	// Odd seq accepted (sandbox parity).
	if err := out.WriteMessageOut(contract.MessageOut{ID: "a", Seq: 1, Content: "reply"}); err != nil {
		t.Fatalf("odd seq should be accepted: %v", err)
	}
	// Even seq rejected.
	if err := out.WriteMessageOut(contract.MessageOut{ID: "b", Seq: 2}); err == nil {
		t.Fatal("even seq should be rejected on outbound (sandbox writes odd)")
	}
}

func TestMemInboundRoundTrip(t *testing.T) {
	store := NewMemStore()
	in := NewMemInbound(store)
	// Explicit seqs, written out of order: the reader must order by seq, not by
	// insertion order.
	in.WriteMessageIn(contract.MessageIn{ID: "m4", Seq: 4, Content: "second"})
	in.WriteMessageIn(contract.MessageIn{ID: "m2", Seq: 2, Content: "first"})

	// Reader view (same shared store) sees both, ordered by seq.
	got, err := in.PendingMessages(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "m2" || got[1].ID != "m4" {
		t.Fatalf("round-trip order wrong: %+v", got)
	}
}

func TestMemInboundOnWakeGating(t *testing.T) {
	store := NewMemStore()
	in := NewMemInbound(store)
	in.WriteMessageIn(contract.MessageIn{ID: "normal", Seq: 2})
	in.WriteMessageIn(contract.MessageIn{ID: "wake", Seq: 4, OnWake: true})

	// Non-first poll hides on_wake messages.
	got, _ := in.PendingMessages(false)
	if len(got) != 1 || got[0].ID != "normal" {
		t.Fatalf("on_wake should be hidden on non-first poll: %+v", got)
	}
	// First poll sees both.
	got, _ = in.PendingMessages(true)
	if len(got) != 2 {
		t.Fatalf("first poll should see on_wake: %+v", got)
	}
}

func TestMemOutboundDueAndAcks(t *testing.T) {
	store := NewMemStore()
	out := NewMemOutbound(store)
	future := time.Now().Add(time.Hour)
	out.WriteMessageOut(contract.MessageOut{ID: "now", Seq: 1})
	out.WriteMessageOut(contract.MessageOut{ID: "later", Seq: 3, DeliverAfter: &future})

	due, _ := out.DueMessages()
	if len(due) != 1 || due[0].ID != "now" {
		t.Fatalf("only the due message should be returned: %+v", due)
	}

	out.MarkProcessing([]contract.MessageID{"now"})
	out.MarkCompleted([]contract.MessageID{"now"})
	acks, _ := out.ProcessingAcks()
	if len(acks) != 1 || acks[0].Status != "completed" {
		t.Fatalf("ack should be completed: %+v", acks)
	}
}

func TestMemSharedStoreHostSandboxAgree(t *testing.T) {
	// One shared store: a host-side inbound writer and a sandbox-side reader (both
	// MemInbound over the same store) must agree, mirroring the two real DB files.
	store := NewMemStore()
	hostView := NewMemInbound(store)
	sandboxView := NewMemInbound(store)
	hostView.WriteMessageIn(contract.MessageIn{ID: "x", Seq: 0, Content: "hello"})
	got, _ := sandboxView.PendingMessages(true)
	if len(got) != 1 || got[0].Content != "hello" {
		t.Fatalf("sandbox view should see host write: %+v", got)
	}
}
