// OWNER: AGENT2

// Package queue provides the sandbox-side queue implementations: an
// contract.InboundReader over contract.OpenInboundRO (read-only) and an
// contract.OutboundWriter over contract.OpenOutboundRW.
//
// The inbound handle is reopened every poll (mmap_size=0, query_only) to defeat
// the guest page cache; a corruption streak exits the process so the host
// respawns with a fresh mount. No method here writes inbound — that is enforced
// at the type level by InboundReader.
//
// CONTRACT: read-only import of github.com/nivardsec/ironclaw/internal/contract.
package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// ErrCorruptionStreak is returned once the inbound queue has failed to open or
// read with a corruption-class error CorruptionStreakThreshold times in a row.
// The sandbox loop treats this as fatal and exits so the host can respawn the
// sandbox with a fresh, re-mounted inbound database.
var ErrCorruptionStreak = errors.New("sandbox/queue: inbound corruption streak exceeded; exiting for host respawn")

// CorruptionStreakThreshold is the number of consecutive corruption-class
// failures on the inbound handle that trips ErrCorruptionStreak.
const CorruptionStreakThreshold = 3

// queryTimeout bounds a single reopen+read or write cycle.
const queryTimeout = 5 * time.Second

// timeLayout is the textual encoding used for every TEXT timestamp column in the
// frozen schema. The contract pins the column types but not their encoding, so
// host and sandbox must agree on this layout out of band; RFC3339 with
// nanoseconds in UTC is the convention. It is centralized here so a future
// contract RFC that pins the format only has to change one constant. (Verified to
// match the host: internal/host/queue tsString uses time.RFC3339Nano UTC.)
const timeLayout = time.RFC3339Nano

// Inbound status values the host writes for messages the sandbox should process,
// now pinned in the frozen contract (RFC-0002) so host and sandbox can never
// drift: the router writes StatusQueued for immediate messages; delivery writes
// StatusScheduled for schedule_task messages, which become processable once their
// process_after is reached. Both are read here; due-ness is then governed by
// process_after.
const (
	statusQueued    = contract.StatusQueued
	statusScheduled = contract.StatusScheduled
)

// sandboxInbound is the sandbox's read-only implementation of the inbound queue.
//
// It holds no live *sql.DB: the handle is reopened on every read so that writes
// the host makes across the bind mount are always observed (the guest page cache
// is bypassed via the mmap_size=0 + reopen discipline centralized in
// contract.OpenInboundRO).
type sandboxInbound struct {
	path string
	key  contract.SessionKey

	mu     sync.Mutex
	streak int // consecutive corruption-class failures
}

// OpenInbound opens the inbound queue read-only (sandbox side).
//
// It does not hold a connection open; it validates that a fresh read-only handle
// can be obtained and then closes it, reopening per poll thereafter. Until the
// encrypted SQLite binding is wired in, the underlying open returns
// contract.ErrCryptoBindingPending, which is propagated unchanged.
func OpenInbound(path string, k contract.SessionKey) (contract.InboundReader, error) {
	r := &sandboxInbound{path: path, key: k}
	// Probe once so a misconfigured path/key surfaces at construction rather
	// than on the first poll. A pending crypto binding is not an error here.
	if err := r.withConn(func(*sql.DB) error { return nil }); err != nil &&
		!errors.Is(err, contract.ErrCryptoBindingPending) {
		return nil, err
	}
	return r, nil
}

// withConn opens a fresh read-only inbound handle, runs fn against it, closes it,
// and maintains the corruption streak. A pending crypto binding is passed through
// without affecting the streak. Reaching CorruptionStreakThreshold consecutive
// corruption-class failures returns ErrCorruptionStreak.
func (s *sandboxInbound) withConn(fn func(*sql.DB) error) error {
	db, err := contract.OpenInboundRO(s.path, s.key)
	if err == nil {
		defer db.Close()
		err = fn(db)
	}

	if errors.Is(err, contract.ErrCryptoBindingPending) {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil && isCorruption(err) {
		s.streak++
		if s.streak >= CorruptionStreakThreshold {
			return fmt.Errorf("%w: last error: %v", ErrCorruptionStreak, err)
		}
		return err
	}
	if err == nil {
		s.streak = 0
	}
	return err
}

// PendingMessages returns inbound rows the sandbox should process: those with a
// host-written ready status ("queued" or "scheduled") that are due, ordered by
// seq (the monotonic key the host assigns; host=even, sandbox=odd).
//
// Due-ness is governed by process_after, not by the poll index: an immediate
// "queued" message (process_after NULL) is always due; a "scheduled" message is
// withheld until its process_after is reached — even on a cold start, so a future
// schedule never fires early. firstPoll is accepted for the contract signature
// and reserved for cold-start engagement, which the loop applies.
func (s *sandboxInbound) PendingMessages(firstPoll bool) ([]contract.MessageIn, error) {
	_ = firstPoll // due-ness comes from process_after; see doc comment
	var out []contract.MessageIn
	err := s.withConn(func(db *sql.DB) error {
		ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
		defer cancel()
		rows, err := db.QueryContext(ctx, `
			SELECT id, seq, kind, timestamp, status, process_after, recurrence,
			       series_id, tries, "trigger", platform_id, channel_type,
			       thread_id, content, source_session_id, on_wake
			FROM messages_in
			WHERE status IN (?, ?)
			ORDER BY seq ASC`, statusQueued, statusScheduled)
		if err != nil {
			return err
		}
		defer rows.Close()

		now := time.Now().UTC()
		for rows.Next() {
			m, err := scanMessageIn(rows)
			if err != nil {
				return err
			}
			if !isDue(m.ProcessAfter, now) {
				continue // scheduled but not yet due
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// isDue reports whether a message with the given process_after is due at now. A
// nil process_after (an immediate "queued" message) is always due; a scheduled
// message is due once now has reached its process_after.
func isDue(processAfter *time.Time, now time.Time) bool {
	return processAfter == nil || !processAfter.After(now)
}

// Destinations returns the places this agent group is allowed to send to.
func (s *sandboxInbound) Destinations() ([]contract.Destination, error) {
	var out []contract.Destination
	err := s.withConn(func(db *sql.DB) error {
		ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
		defer cancel()
		rows, err := db.QueryContext(ctx, `
			SELECT name, display_name, type, channel_type, platform_id, agent_group_id
			FROM destinations
			ORDER BY name ASC`)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var (
				name                                            string
				displayName, typ, channelType, platformID, agid sql.NullString
			)
			if err := rows.Scan(&name, &displayName, &typ, &channelType, &platformID, &agid); err != nil {
				return err
			}
			d := contract.Destination{
				Name:        name,
				DisplayName: nullStr(displayName),
				Type:        typ.String,
				ChannelType: nullStr(channelType),
				PlatformID:  nullStr(platformID),
			}
			if agid.Valid {
				id := contract.AgentGroupID(agid.String)
				d.AgentGroupID = &id
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SessionRouting returns the platform coordinates of this session. If the host
// has not written a routing row yet, the zero value is returned with a nil error.
func (s *sandboxInbound) SessionRouting() (contract.SessionRouting, error) {
	var sr contract.SessionRouting
	err := s.withConn(func(db *sql.DB) error {
		ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
		defer cancel()
		var channelType, platformID, threadID sql.NullString
		row := db.QueryRowContext(ctx, `
			SELECT channel_type, platform_id, thread_id
			FROM session_routing WHERE id = 1`)
		switch err := row.Scan(&channelType, &platformID, &threadID); {
		case errors.Is(err, sql.ErrNoRows):
			return nil // not configured yet
		case err != nil:
			return err
		}
		sr.ChannelType = channelType.String
		sr.PlatformID = platformID.String
		sr.ThreadID = nullStr(threadID)
		return nil
	})
	if err != nil {
		return contract.SessionRouting{}, err
	}
	return sr, nil
}

// Close releases inbound resources. The reader holds no persistent handle, so
// this is a no-op kept for interface symmetry.
func (s *sandboxInbound) Close() error { return nil }

// scanMessageIn maps one messages_in row onto a contract.MessageIn.
func scanMessageIn(rows *sql.Rows) (contract.MessageIn, error) {
	var (
		id                                                          string
		seq, tries, trigger, onWake                                 sql.NullInt64
		kind, ts, status, processAfter, recurrence, seriesID        sql.NullString
		platformID, channelType, threadID, content, sourceSessionID sql.NullString
	)
	if err := rows.Scan(&id, &seq, &kind, &ts, &status, &processAfter, &recurrence,
		&seriesID, &tries, &trigger, &platformID, &channelType, &threadID,
		&content, &sourceSessionID, &onWake); err != nil {
		return contract.MessageIn{}, err
	}

	tsParsed, err := parseTime(ts)
	if err != nil {
		return contract.MessageIn{}, fmt.Errorf("messages_in %q: timestamp: %w", id, err)
	}
	pa, err := parseNullTime(processAfter)
	if err != nil {
		return contract.MessageIn{}, fmt.Errorf("messages_in %q: process_after: %w", id, err)
	}

	return contract.MessageIn{
		ID:              contract.MessageID(id),
		Seq:             seq.Int64,
		Kind:            contract.MessageKind(kind.String),
		Timestamp:       tsParsed,
		Status:          status.String,
		ProcessAfter:    pa,
		Recurrence:      nullStr(recurrence),
		SeriesID:        nullStr(seriesID),
		Tries:           int(tries.Int64),
		Trigger:         int(trigger.Int64),
		PlatformID:      nullStr(platformID),
		ChannelType:     nullStr(channelType),
		ThreadID:        nullStr(threadID),
		Content:         content.String,
		SourceSessionID: nullStr(sourceSessionID),
		OnWake:          onWake.Int64 != 0,
	}, nil
}

// sandboxOutbound is the sandbox's write implementation of the outbound queue.
//
// The sandbox is the sole writer of outbound, so the handle is held open for the
// life of the session. Writes are serialized by mu so seq assignment stays
// monotonic and the contract's host=even/sandbox=odd parity is preserved.
type sandboxOutbound struct {
	db *sql.DB
	mu sync.Mutex
}

// OpenOutbound opens the outbound queue read/write (sandbox side, sole writer)
// and ensures the outbound schema exists. Until the encrypted SQLite binding is
// wired in, the underlying open returns contract.ErrCryptoBindingPending.
func OpenOutbound(path string, k contract.SessionKey) (contract.OutboundWriter, error) {
	db, err := contract.OpenOutboundRW(path, k)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(contract.OutboundSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("sandbox/queue: ensure outbound schema: %w", err)
	}
	return &sandboxOutbound{db: db}, nil
}

// WriteMessageOut appends an outbound message, assigning the next odd seq so the
// host=even/sandbox=odd parity holds. Any Seq set by the caller is overwritten.
func (s *sandboxOutbound) WriteMessageOut(m contract.MessageOut) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var maxSeq sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(seq) FROM messages_out`).Scan(&maxSeq); err != nil {
		return err
	}
	m.Seq = nextOddSeq(maxSeq.Int64, maxSeq.Valid)

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages_out
			(id, seq, in_reply_to, timestamp, deliver_after, recurrence, kind,
			 platform_id, channel_type, thread_id, content)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(m.ID), m.Seq, idArg(m.InReplyTo), formatTime(m.Timestamp),
		deliverAfterArg(m.DeliverAfter), m.Recurrence, string(m.Kind),
		m.PlatformID, m.ChannelType, m.ThreadID, m.Content,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkProcessing records that the given inbound messages are being processed,
// for the host to read back via OutboundReader.ProcessingAcks.
func (s *sandboxOutbound) MarkProcessing(ids []contract.MessageID) error {
	return s.markAck(ids, contract.StatusProcessing)
}

// MarkCompleted records that the given inbound messages are done.
func (s *sandboxOutbound) MarkCompleted(ids []contract.MessageID) error {
	return s.markAck(ids, contract.StatusCompleted)
}

func (s *sandboxOutbound) markAck(ids []contract.MessageID, status string) error {
	if len(ids) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := formatTime(time.Now().UTC())
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO processing_ack (message_id, status, status_changed)
			VALUES (?, ?, ?)
			ON CONFLICT(message_id) DO UPDATE SET
				status = excluded.status,
				status_changed = excluded.status_changed`,
			string(id), status, now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PutSessionState upserts one key/value entry of durable per-session state.
func (s *sandboxOutbound) PutSessionState(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO session_state (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at`,
		key, value, formatTime(time.Now().UTC()))
	return err
}

// LoadSessionState returns every durable key/value pair previously written via
// PutSessionState. The sandbox is the sole writer of the outbound DB and holds
// it open for the session, so reading its own session_state table back on
// startup restores per-session loop state (accumulated and deduped message ids)
// that would otherwise be lost when the process exits and the host respawns it.
//
// This read method is deliberately NOT part of the frozen contract.OutboundWriter
// interface (which stays write-only by design); it is a sandbox-internal
// capability the poll loop discovers by type assertion (T-114).
func (s *sandboxOutbound) LoadSessionState() (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM session_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var k string
		var v sql.NullString
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v.String
	}
	return out, rows.Err()
}

// Close closes the outbound handle.
func (s *sandboxOutbound) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// nextOddSeq returns the smallest odd seq strictly greater than the current max.
// With an empty table it returns 1. The sandbox always writes odd seq values so
// it never collides with the host's even ones (frozen seq parity).
func nextOddSeq(maxSeq int64, present bool) int64 {
	if !present {
		return 1
	}
	next := maxSeq + 1
	if next%2 == 0 {
		next++
	}
	return next
}

// isCorruption reports whether err looks like an encrypted-SQLite corruption or
// wrong-key failure — the signal to reopen against a fresh mount. The encrypted
// binding is not yet wired, so detection is by message substring for now; once
// the SQLite3MC driver lands this can switch to its typed error codes
// (SQLITE_NOTADB=26, SQLITE_CORRUPT=11).
func isCorruption(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, sig := range []string{
		"not a database",
		"file is encrypted",
		"disk image is malformed",
		"malformed",
		"database corruption",
	} {
		if strings.Contains(msg, sig) {
			return true
		}
	}
	return false
}

// formatTime encodes t for a TEXT timestamp column (UTC, RFC3339Nano).
func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

// deliverAfterArg encodes an optional timestamp as a sql.NullString so the
// column is written as SQL NULL when absent.
func deliverAfterArg(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: formatTime(*t), Valid: true}
}

// parseTime parses a required TEXT timestamp; an absent value yields the zero time.
func parseTime(ns sql.NullString) (time.Time, error) {
	if !ns.Valid || ns.String == "" {
		return time.Time{}, nil
	}
	return time.Parse(timeLayout, ns.String)
}

// parseNullTime parses an optional TEXT timestamp into a *time.Time.
func parseNullTime(ns sql.NullString) (*time.Time, error) {
	if !ns.Valid || ns.String == "" {
		return nil, nil
	}
	t, err := time.Parse(timeLayout, ns.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// nullStr converts a sql.NullString to a *string (nil when not valid).
func nullStr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

// idArg renders an optional MessageID as a sql.NullString for a nullable TEXT
// column.
func idArg(id *contract.MessageID) sql.NullString {
	if id == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: string(*id), Valid: true}
}
