// OWNER: AGENT2

package parity

import (
	"path/filepath"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
	hostqueue "github.com/nivardsec/ironclaw/internal/host/queue"
	sandboxqueue "github.com/nivardsec/ironclaw/internal/sandbox/queue"
)

// These specs are black-box over the outbound queue DB: the sandbox (AGENT2) writes
// through its OutboundWriter and the host (AGENT1) reads through its OutboundReader
// over the SAME encrypted file, so they validate the cross-agent contract end to
// end rather than one side in isolation. They run in the normal suite now that the
// encrypted-SQLite binding is wired (RFC-0001).

// parityKey returns a deterministic, non-zero SessionKey for tests.
func parityKey(seed byte) contract.SessionKey {
	var k contract.SessionKey
	for i := range k {
		k[i] = seed + byte(i)
	}
	return k
}

// TestSandboxOutboundSeqParity asserts the frozen seq-parity contract: every
// message the sandbox writes to the outbound queue is assigned an ODD seq (the host
// writes EVEN), monotonically increasing, so the two sides never collide without
// coordinating a counter (contract/schema.go).
func TestSandboxOutboundSeqParity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbound.db")
	k := parityKey(5)

	w, err := sandboxqueue.OpenOutbound(path, k)
	if err != nil {
		t.Fatalf("sandbox OpenOutbound: %v", err)
	}
	for _, c := range []string{"a", "b", "c"} {
		if err := w.WriteMessageOut(contract.MessageOut{ID: contract.MessageID(c), Kind: contract.KindChat, Content: c}); err != nil {
			t.Fatalf("WriteMessageOut(%s): %v", c, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	r, err := hostqueue.OpenOutbound(path, k)
	if err != nil {
		t.Fatalf("host OpenOutbound: %v", err)
	}
	defer r.Close()
	due, err := r.DueMessages()
	if err != nil {
		t.Fatalf("DueMessages: %v", err)
	}
	if len(due) != 3 {
		t.Fatalf("host read %d outbound messages, want 3", len(due))
	}
	var prev int64
	for i, m := range due {
		if m.Seq%2 == 0 {
			t.Fatalf("message %d has even seq %d; the sandbox must write odd seq", i, m.Seq)
		}
		if m.Seq <= prev {
			t.Fatalf("seq not monotonic: %d followed %d", m.Seq, prev)
		}
		prev = m.Seq
	}
}

// TestSandboxAcksProcessing asserts the ack contract: when the sandbox engages a
// set of inbound messages it records a processing ack and, on completion, a
// completed ack in the outbound processing_ack table — the host's signal to advance
// inbound status. The host reads them back through its OutboundReader, reopening per
// read (the reopen-per-poll discipline) so it always observes the latest status.
func TestSandboxAcksProcessing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbound.db")
	k := parityKey(6)
	ids := []contract.MessageID{"m1", "m2"}

	w, err := sandboxqueue.OpenOutbound(path, k)
	if err != nil {
		t.Fatalf("sandbox OpenOutbound: %v", err)
	}
	defer w.Close()

	if err := w.MarkProcessing(ids); err != nil {
		t.Fatalf("MarkProcessing: %v", err)
	}
	assertAckStatus(t, path, k, map[contract.MessageID]string{"m1": contract.StatusProcessing, "m2": contract.StatusProcessing})

	if err := w.MarkCompleted(ids); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	assertAckStatus(t, path, k, map[contract.MessageID]string{"m1": contract.StatusCompleted, "m2": contract.StatusCompleted})
}

// assertAckStatus opens a fresh host reader over the encrypted outbound file and
// checks the processing_ack rows match want.
func assertAckStatus(t *testing.T, path string, k contract.SessionKey, want map[contract.MessageID]string) {
	t.Helper()
	r, err := hostqueue.OpenOutbound(path, k)
	if err != nil {
		t.Fatalf("host OpenOutbound: %v", err)
	}
	defer r.Close()
	acks, err := r.ProcessingAcks()
	if err != nil {
		t.Fatalf("ProcessingAcks: %v", err)
	}
	got := make(map[contract.MessageID]string, len(acks))
	for _, a := range acks {
		got[a.MessageID] = a.Status
	}
	if len(got) != len(want) {
		t.Fatalf("got %d acks, want %d (%v)", len(got), len(want), got)
	}
	for id, status := range want {
		if got[id] != status {
			t.Fatalf("ack %s status = %q, want %q", id, got[id], status)
		}
	}
}
