//go:build wsg_verify

package wsg

import (
	"context"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/keys"
	"github.com/IronSecCo/ironclaw/internal/host/queue"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
	"github.com/IronSecCo/ironclaw/internal/host/sweep"
)

// healthyProber reports every session as live so the sweep never kills it; we only
// want to exercise the scheduling (due-message) pass.
type healthyProber struct{}

func (healthyProber) Probe(contract.SessionID) (int64, int64, error) { return 100, 0, nil }

// noopKiller records nothing; a healthy prober means it is never called.
type noopKiller struct{}

func (noopKiller) Kill(contract.SessionID, sweep.StuckAction) error { return nil }

// recordingWaker counts how many times each session was woken — a woken session is
// the durable signal that its scheduled prompt fired.
type recordingWaker struct{ woke map[contract.SessionID]int }

func (w *recordingWaker) Wake(id contract.SessionID) error {
	if w.woke == nil {
		w.woke = map[contract.SessionID]int{}
	}
	w.woke[id]++
	return nil
}

// TestG8_ScheduledTask_FiresOnceAudited proves the scheduling row against the REAL
// queue-backed sweep: a near-term scheduled message comes due, the sweep fires it
// (wakes the session) and durably enqueues the next recurring occurrence; a second
// sweep is idempotent (the occurrence is deduped, never double-enqueued).
func TestG8_ScheduledTask_FiresOnceAudited(t *testing.T) {
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

	// Write one near-term, recurring scheduled message (host is the inbound writer).
	w, err := fac.OpenHostInbound(string(sess.ID), key)
	if err != nil {
		t.Fatalf("OpenHostInbound: %v", err)
	}
	due := time.Now().Add(-time.Minute).UTC()
	rec := "daily"
	if err := w.WriteMessageIn(contract.MessageIn{
		ID: "sched-1", Seq: 2, Status: contract.StatusScheduled,
		ProcessAfter: &due, Recurrence: &rec, Content: "nightly digest",
	}); err != nil {
		t.Fatalf("WriteMessageIn: %v", err)
	}
	w.Close()

	const content = "nightly digest"
	if got := countScheduled(t, fac, sess.ID, key, content); got != 1 {
		t.Fatalf("setup: expected 1 scheduled occurrence, got %d", got)
	}

	waker := &recordingWaker{}
	s := sweep.New(reg, healthyProber{}, noopKiller{}).
		WithScheduling(
			sweep.NewQueueDueSource(reg, fac, cust),
			waker,
			sweep.NewInboundEnqueue(fac, cust).Enqueue,
		)

	// Sweep #1: the due message fires once and the next occurrence is enqueued.
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("sweep run #1: %v", err)
	}
	if waker.woke[sess.ID] != 1 {
		t.Fatalf("expected the due task to fire once (one wake), got %d", waker.woke[sess.ID])
	}
	if got := countScheduled(t, fac, sess.ID, key, content); got != 2 {
		t.Fatalf("after sweep #1 expected 2 occurrences (original + next), got %d", got)
	}

	// Sweep #2: firing again must not double-enqueue the same next occurrence.
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("sweep run #2: %v", err)
	}
	if got := countScheduled(t, fac, sess.ID, key, content); got != 2 {
		t.Fatalf("after sweep #2 expected still 2 occurrences (deduped), got %d — fire is not idempotent", got)
	}
	t.Logf("G8 scheduling: near-term task fired once, next occurrence durably enqueued and deduped on re-sweep")
}

// countScheduled counts durable scheduled rows with the given content in the
// session's encrypted inbound queue (the audit trail for a fire).
func countScheduled(t *testing.T, fac *queue.Factory, id contract.SessionID, key contract.SessionKey, content string) int {
	t.Helper()
	db, err := fac.OpenSandboxInbound(string(id), key)
	if err != nil {
		t.Fatalf("OpenSandboxInbound: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(
		`SELECT count(*) FROM messages_in WHERE content = ? AND status = ?`,
		content, contract.StatusScheduled,
	).Scan(&n); err != nil {
		t.Fatalf("count scheduled: %v", err)
	}
	return n
}
