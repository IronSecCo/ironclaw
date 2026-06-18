package sweep

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/queue"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
)

// This file binds the sweep's interfaces to the live control-plane dependencies,
// replacing the log-only placeholders the daemon shipped with:
//
//   - Prober / Killer / Waker are satisfied by the session.Manager,
//     which owns the live sandbox handles, heartbeats, and claims.
//   - DueSource and EnqueueFunc — the parts that read and write the per-session
//     inbound queues directly — live here, bound to the queue.Factory.
//
// Scheduling carries ONLY a prompt: a due message is re-enqueued as an ordinary
// future inbound message and nothing is ever executed off this path (no script
// field → no RCE), consistent with host/scheduling's security note.

// KeySource resolves a session's encrypted-queue key. It is satisfied by
// *keys.Custodian (host/keys) via Get, kept as a narrow interface so this package
// need not import host/keys.
type KeySource interface {
	Get(contract.SessionID) (contract.SessionKey, bool)
}

// QueueDueSource implements DueSource by reading every registry session's inbound
// queue (read-only, via the factory's sandbox-inbound opener) for scheduled
// messages whose process_after has come due. It is the live replacement for the
// daemon's empty due-source.
//
// A session whose key or encrypted files are not yet provisioned is skipped (not
// an error) so one un-provisioned session never stalls the whole sweep.
//
// NOTE (bounded follow-up): a due scheduled row stays in the inbound queue until
// the host advances its status after the sandbox completes it; until that
// host-side status advancement lands, DueMessages re-reports a lingering due row
// each pass. Waking is idempotent (a no-op for a running sandbox) and recurrence
// re-enqueue is deduped (see InboundEnqueue), so this is harmless repetition, not
// a storm.
type QueueDueSource struct {
	reg     registry.Registry
	factory *queue.Factory
	keys    KeySource
	logger  *log.Logger
}

// NewQueueDueSource constructs a QueueDueSource over the registry, queue factory,
// and key source.
func NewQueueDueSource(reg registry.Registry, factory *queue.Factory, keys KeySource) *QueueDueSource {
	return &QueueDueSource{reg: reg, factory: factory, keys: keys, logger: log.Default()}
}

// WithLogger overrides the diagnostic logger. Returns q for chaining.
func (q *QueueDueSource) WithLogger(l *log.Logger) *QueueDueSource {
	if l != nil {
		q.logger = l
	}
	return q
}

// DueMessages returns every session's scheduled inbound messages that are due at
// now. Due-ness is decided in Go by parsing process_after (not by a lexicographic
// SQL string compare, which is unsafe for the trimmed RFC3339Nano encoding).
func (q *QueueDueSource) DueMessages(now time.Time) ([]DueMessage, error) {
	sessions, err := q.reg.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("host/sweep: list sessions: %w", err)
	}
	var out []DueMessage
	for _, sess := range sessions {
		due, err := q.dueForSession(sess.ID, now)
		if err != nil {
			// Skip an unreadable/un-provisioned session rather than failing the whole
			// sweep; log it so a persistent problem is visible.
			q.logger.Printf("host/sweep: skip due-scan for %s: %v", sess.ID, err)
			continue
		}
		out = append(out, due...)
	}
	return out, nil
}

// dueForSession reads one session's inbound queue read-only and returns its due
// scheduled messages.
func (q *QueueDueSource) dueForSession(id contract.SessionID, now time.Time) ([]DueMessage, error) {
	key, ok := q.keys.Get(id)
	if !ok {
		return nil, fmt.Errorf("no session key (not provisioned)")
	}
	db, err := q.factory.OpenSandboxInbound(string(id), key)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT id, content, process_after, recurrence
		FROM messages_in
		WHERE status = ?
		ORDER BY seq`, contract.StatusScheduled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var due []DueMessage
	for rows.Next() {
		var (
			mid          string
			content      string
			processAfter sql.NullString
			recurrence   sql.NullString
		)
		if err := rows.Scan(&mid, &content, &processAfter, &recurrence); err != nil {
			return nil, err
		}
		runAt, ok := parseDue(processAfter)
		if ok && runAt.After(now) {
			continue // scheduled but not yet due
		}
		dm := DueMessage{
			SessionID: id,
			MessageID: contract.MessageID(mid),
			Prompt:    content,
			RunAt:     runAt,
		}
		if recurrence.Valid {
			dm.Recurrence = recurrence.String
		}
		due = append(due, dm)
	}
	return due, rows.Err()
}

// parseDue parses a process_after TEXT value. The bool is false when the column is
// NULL (an always-due immediate message); a parse failure yields the zero time
// (treated as due) so a malformed row is woken rather than silently stuck.
func parseDue(ns sql.NullString) (time.Time, bool) {
	if !ns.Valid || ns.String == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, ns.String)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// InboundEnqueue implements EnqueueFunc: it re-enqueues the next occurrence of a
// recurring due message as a future inbound message (status=scheduled,
// process_after=runAt), via the host inbound writer. It carries ONLY a prompt —
// no script/command field — so nothing is executed; the sweep wakes the session
// when the message later comes due and the sandbox picks it up as ordinary input.
//
// Re-enqueues are deduped by (session, run time, prompt) so a lingering due row
// re-reported across sweeps enqueues its successor exactly once.
type InboundEnqueue struct {
	factory *queue.Factory
	keys    KeySource

	mu       sync.Mutex
	enqueued map[string]struct{}
	ctr      int64
}

// NewInboundEnqueue constructs an InboundEnqueue over the queue factory and key
// source.
func NewInboundEnqueue(factory *queue.Factory, keys KeySource) *InboundEnqueue {
	return &InboundEnqueue{factory: factory, keys: keys, enqueued: make(map[string]struct{})}
}

// Enqueue writes the next occurrence as a scheduled inbound message. Its signature
// matches EnqueueFunc, so it is wired as the sweep's enqueue hook.
func (e *InboundEnqueue) Enqueue(sessionID contract.SessionID, prompt string, runAt time.Time, recurrence string) error {
	runAt = runAt.UTC()
	dedupKey := string(sessionID) + "\x00" + runAt.Format(time.RFC3339Nano) + "\x00" + prompt
	e.mu.Lock()
	if _, done := e.enqueued[dedupKey]; done {
		e.mu.Unlock()
		return nil // this occurrence is already enqueued
	}
	e.mu.Unlock()

	key, ok := e.keys.Get(sessionID)
	if !ok {
		return fmt.Errorf("host/sweep: enqueue %s: no session key (not provisioned)", sessionID)
	}

	seq, err := e.nextEvenSeq(sessionID, key)
	if err != nil {
		return fmt.Errorf("host/sweep: enqueue %s: next seq: %w", sessionID, err)
	}

	w, err := e.factory.OpenHostInbound(string(sessionID), key)
	if err != nil {
		return fmt.Errorf("host/sweep: enqueue %s: open inbound: %w", sessionID, err)
	}
	defer w.Close()

	in := contract.MessageIn{
		ID:           e.nextID(sessionID),
		Seq:          seq,
		Kind:         contract.KindTask,
		Timestamp:    time.Now().UTC(),
		Status:       contract.StatusScheduled,
		ProcessAfter: &runAt,
		Content:      prompt,
	}
	if strings.TrimSpace(recurrence) != "" {
		rec := recurrence
		in.Recurrence = &rec
	}
	if err := w.WriteMessageIn(in); err != nil {
		return fmt.Errorf("host/sweep: enqueue %s: write: %w", sessionID, err)
	}

	e.mu.Lock()
	e.enqueued[dedupKey] = struct{}{}
	e.mu.Unlock()
	return nil
}

// nextEvenSeq returns the next EVEN inbound seq for the session (host parity),
// strictly greater than the current MAX(seq). Inbound is host-written only, so all
// existing seqs are even; an empty table starts at 2. Reading the current max
// keeps the assigned seq unique against the schema's UNIQUE(seq) constraint.
func (e *InboundEnqueue) nextEvenSeq(id contract.SessionID, key contract.SessionKey) (int64, error) {
	db, err := e.factory.OpenSandboxInbound(string(id), key)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var maxSeq sql.NullInt64
	if err := db.QueryRow(`SELECT MAX(seq) FROM messages_in`).Scan(&maxSeq); err != nil {
		return 0, err
	}
	if !maxSeq.Valid {
		return 2, nil
	}
	next := maxSeq.Int64 + 2
	if next%2 != 0 {
		next++
	}
	return next, nil
}

// nextID returns a process-unique id for an enqueued scheduled message.
func (e *InboundEnqueue) nextID(id contract.SessionID) contract.MessageID {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ctr++
	return contract.MessageID(fmt.Sprintf("recur_%s_%d", id, e.ctr))
}
