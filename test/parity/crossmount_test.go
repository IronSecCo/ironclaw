package parity

import (
	"path/filepath"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
	sandboxqueue "github.com/IronSecCo/ironclaw/internal/sandbox/queue"
)

// TestCrossMountLivePoll asserts the load-bearing behavioral contract: an inbound
// write made AFTER the sandbox has begun polling is observed by the sandbox within
// one poll. It validates the encrypted + DELETE-journal + mmap_size=0 +
// reopen-per-poll discipline end to end — the host writes
// through contract.OpenInboundRW and the sandbox reads through its reopen-per-poll
// InboundReader over the same encrypted file. A stale guest page cache or a WAL
// -shm mmap that did not refresh across the mount would make the second poll miss
// the write; reopen-per-poll + mmap_size=0 + DELETE journal are what prevent that.
func TestCrossMountLivePoll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inbound.db")
	k := parityKey(9)

	// Host opens the encrypted inbound DB read/write (OpenInboundRW ensures the
	// schema) and holds it open as the sole inbound writer.
	hostDB, err := contract.OpenInboundRW(path, k)
	if err != nil {
		t.Fatalf("host OpenInboundRW: %v", err)
	}
	defer hostDB.Close()

	// Sandbox starts polling: nothing is queued yet.
	r, err := sandboxqueue.OpenInbound(path, k)
	if err != nil {
		t.Fatalf("sandbox OpenInbound: %v", err)
	}
	defer r.Close()
	if msgs, err := r.PendingMessages(true); err != nil {
		t.Fatalf("first PendingMessages: %v", err)
	} else if len(msgs) != 0 {
		t.Fatalf("expected no pending messages before the host write, got %d", len(msgs))
	}

	// Host writes a new inbound message AFTER the sandbox began polling. seq is EVEN
	// (host parity); status is the pinned StatusQueued. An autocommit INSERT releases
	// the write lock immediately.
	if _, err := hostDB.Exec(
		`INSERT INTO messages_in (id, seq, kind, status, content) VALUES (?, ?, ?, ?, ?)`,
		"h1", 2, string(contract.KindChat), contract.StatusQueued, "hello from host",
	); err != nil {
		t.Fatalf("host insert: %v", err)
	}

	// The very next poll must observe it: reopen-per-poll defeats the guest page
	// cache, and mmap_size=0 + DELETE journal make the fresh handle see the commit.
	msgs, err := r.PendingMessages(false)
	if err != nil {
		t.Fatalf("second PendingMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("post-start write not observed within one poll: got %d messages, want 1", len(msgs))
	}
	if msgs[0].Content != "hello from host" {
		t.Fatalf("observed content = %q, want %q", msgs[0].Content, "hello from host")
	}
	if msgs[0].Seq != 2 {
		t.Fatalf("host seq = %d, want 2 (even)", msgs[0].Seq)
	}
}
