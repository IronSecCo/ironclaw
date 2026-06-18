package sweep

import (
	"io"
	"log"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/keys"
	"github.com/IronSecCo/ironclaw/internal/host/queue"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
)

// provisionedSession creates a registry session, generates+custodies its key, and
// provisions its encrypted queue files, returning the pieces a test needs.
func provisionedSession(t *testing.T) (*registry.MemRegistry, *queue.Factory, *keys.Custodian, contract.SessionID) {
	t.Helper()
	reg := registry.NewMemRegistry()
	sess, err := reg.ResolveSession("ag1", "mg1", nil, contract.SessionShared)
	if err != nil {
		t.Fatalf("ResolveSession: %v", err)
	}
	cust, err := keys.New([32]byte{})
	if err != nil {
		t.Fatalf("keys.New: %v", err)
	}
	key, err := cust.Generate(sess.ID)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	fac := queue.NewFactory(t.TempDir())
	if err := fac.Provision(string(sess.ID), key); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	return reg, fac, cust, sess.ID
}

func TestQueueDueSourceReportsOnlyDueScheduled(t *testing.T) {
	reg, fac, cust, id := provisionedSession(t)
	key, _ := cust.Get(id)

	w, err := fac.OpenHostInbound(string(id), key)
	if err != nil {
		t.Fatalf("OpenHostInbound: %v", err)
	}
	past := time.Now().Add(-time.Minute).UTC()
	future := time.Now().Add(time.Hour).UTC()
	rec := "daily"
	mustWrite(t, w, contract.MessageIn{ID: "due", Seq: 2, Status: contract.StatusScheduled, ProcessAfter: &past, Recurrence: &rec, Content: "ping"})
	mustWrite(t, w, contract.MessageIn{ID: "later", Seq: 4, Status: contract.StatusScheduled, ProcessAfter: &future, Content: "soon"})
	mustWrite(t, w, contract.MessageIn{ID: "chat", Seq: 6, Status: contract.StatusQueued, Content: "hi"})
	w.Close()

	ds := NewQueueDueSource(reg, fac, cust)
	due, err := ds.DueMessages(time.Now())
	if err != nil {
		t.Fatalf("DueMessages: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("expected exactly 1 due scheduled message, got %d: %+v", len(due), due)
	}
	got := due[0]
	if got.SessionID != id || got.MessageID != "due" || got.Prompt != "ping" || got.Recurrence != "daily" {
		t.Fatalf("unexpected due message: %+v", got)
	}
	if got.RunAt.IsZero() {
		t.Fatal("expected RunAt populated from process_after")
	}
}

func TestQueueDueSourceSkipsUnprovisioned(t *testing.T) {
	reg := registry.NewMemRegistry()
	// A session exists in the registry but has no key and no provisioned files.
	if _, err := reg.ResolveSession("ag1", "mg1", nil, contract.SessionShared); err != nil {
		t.Fatalf("ResolveSession: %v", err)
	}
	cust, _ := keys.New([32]byte{})
	fac := queue.NewFactory(t.TempDir())

	ds := NewQueueDueSource(reg, fac, cust).WithLogger(log.New(io.Discard, "", 0))
	due, err := ds.DueMessages(time.Now())
	if err != nil {
		t.Fatalf("DueMessages should skip un-provisioned sessions, got error: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("expected no due messages, got %+v", due)
	}
}

func TestInboundEnqueueWritesScheduledAndDedups(t *testing.T) {
	_, fac, cust, id := provisionedSession(t)
	key, _ := cust.Get(id)

	enq := NewInboundEnqueue(fac, cust)
	runAt := time.Now().Add(time.Hour).UTC()

	if err := enq.Enqueue(id, "do thing", runAt, "daily"); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	// Re-enqueueing the same occurrence is a deduped no-op.
	if err := enq.Enqueue(id, "do thing", runAt, "daily"); err != nil {
		t.Fatalf("Enqueue dedup: %v", err)
	}

	// Read inbound RO and verify exactly one scheduled "do thing" with an even seq.
	db, err := fac.OpenSandboxInbound(string(id), key)
	if err != nil {
		t.Fatalf("OpenSandboxInbound: %v", err)
	}
	defer db.Close()
	var n int
	var seq int64
	if err := db.QueryRow(`SELECT count(*), COALESCE(MAX(seq),0) FROM messages_in WHERE content = ? AND status = ?`,
		"do thing", contract.StatusScheduled).Scan(&n, &seq); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 enqueued occurrence (deduped), got %d", n)
	}
	if seq%2 != 0 {
		t.Fatalf("enqueued seq must be even (host parity), got %d", seq)
	}

	// A different run time is a distinct occurrence and writes a second row.
	if err := enq.Enqueue(id, "do thing", runAt.Add(24*time.Hour), "daily"); err != nil {
		t.Fatalf("Enqueue next occurrence: %v", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM messages_in WHERE content = ?`, "do thing").Scan(&n); err != nil {
		t.Fatalf("recount: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 distinct occurrences, got %d", n)
	}
}

func mustWrite(t *testing.T, w contract.InboundWriter, m contract.MessageIn) {
	t.Helper()
	if err := w.WriteMessageIn(m); err != nil {
		t.Fatalf("WriteMessageIn(%s): %v", m.ID, err)
	}
}
