// Package queue provides the host-side queue implementations: a
// contract.InboundWriter (the host is the sole writer of inbound) and a
// contract.OutboundReader (the host reads outbound read-only, with the
// reopen-per-poll discipline).
//
// The SQL below is real and parameterized. As of RFC-0001 the contract exposes
// OpenInboundRW (host is the sole inbound writer), so these openers now back onto
// the live encrypted-SQLite binding (SQLite3/SQLCipher via CGo).
package queue

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// openInboundRW opens the inbound DB read/write for the host (the sole inbound
// writer), via the contract's encrypted opener (RFC-0001).
func openInboundRW(path string, k contract.SessionKey) (*sql.DB, error) {
	return contract.OpenInboundRW(path, k)
}

// hostInbound is the host's write implementation of the inbound queue. The host
// uses EVEN seq numbers (sandbox uses odd) per the frozen seq-parity rule.
type hostInbound struct {
	db *sql.DB
}

// OpenInbound opens the inbound queue for writing (host side). It returns the
// pending-binding error until RFC-0001 lands.
func OpenInbound(path string, k contract.SessionKey) (contract.InboundWriter, error) {
	db, err := openInboundRW(path, k)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(contract.InboundSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("host/queue: apply inbound schema: %w", err)
	}
	return &hostInbound{db: db}, nil
}

// WriteMessageIn inserts a row into messages_in. Times are stored RFC3339Nano in
// UTC; pointer fields map to SQL NULL when nil.
func (h *hostInbound) WriteMessageIn(m contract.MessageIn) error {
	_, err := h.db.Exec(`
        INSERT INTO messages_in
            (id, seq, kind, timestamp, status, process_after, recurrence, series_id,
             tries, trigger, platform_id, channel_type, thread_id, content,
             source_session_id, on_wake)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		string(m.ID), m.Seq, string(m.Kind), tsString(m.Timestamp), m.Status,
		tsPtr(m.ProcessAfter), strPtr(m.Recurrence), strPtr(m.SeriesID),
		m.Tries, m.Trigger, strPtr(m.PlatformID), strPtr(m.ChannelType),
		strPtr(m.ThreadID), m.Content, strPtr(m.SourceSessionID), boolInt(m.OnWake),
	)
	return err
}

// UpsertDestinations replaces/inserts destination rows by name.
func (h *hostInbound) UpsertDestinations(ds []contract.Destination) error {
	tx, err := h.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`
        INSERT INTO destinations (name, display_name, type, channel_type, platform_id, agent_group_id)
        VALUES (?,?,?,?,?,?)
        ON CONFLICT(name) DO UPDATE SET
            display_name=excluded.display_name,
            type=excluded.type,
            channel_type=excluded.channel_type,
            platform_id=excluded.platform_id,
            agent_group_id=excluded.agent_group_id`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, d := range ds {
		var agid *string
		if d.AgentGroupID != nil {
			s := string(*d.AgentGroupID)
			agid = &s
		}
		if _, err := stmt.Exec(d.Name, strPtr(d.DisplayName), d.Type, strPtr(d.ChannelType), strPtr(d.PlatformID), agid); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// MarkDelivered records a delivery in the inbound `delivered` table (host-side
// dedup of outbound messages — the host never writes the outbound DB).
func (h *hostInbound) MarkDelivered(id contract.MessageID, platformMsgID *string) error {
	_, err := h.db.Exec(`
        INSERT INTO delivered (message_out_id, platform_message_id, status, delivered_at)
        VALUES (?,?,?,?)
        ON CONFLICT(message_out_id) DO UPDATE SET
            platform_message_id=excluded.platform_message_id,
            status=excluded.status,
            delivered_at=excluded.delivered_at`,
		string(id), strPtr(platformMsgID), contract.StatusDelivered, tsString(time.Now().UTC()))
	return err
}

// Close closes the underlying handle.
func (h *hostInbound) Close() error {
	if h.db == nil {
		return nil
	}
	return h.db.Close()
}

// hostOutbound is the host's read implementation of the outbound queue.
type hostOutbound struct {
	db *sql.DB
}

// OpenOutbound opens the outbound queue for reading (host side). It returns the
// pending-binding error until the encrypted-SQLite binding lands.
func OpenOutbound(path string, k contract.SessionKey) (contract.OutboundReader, error) {
	db, err := contract.OpenOutboundRO(path, k)
	if err != nil {
		return nil, err
	}
	return &hostOutbound{db: db}, nil
}

// DueMessages returns outbound messages whose deliver_after is null or in the
// past, ordered by seq.
func (h *hostOutbound) DueMessages() ([]contract.MessageOut, error) {
	now := tsString(time.Now().UTC())
	rows, err := h.db.Query(`
        SELECT id, seq, in_reply_to, timestamp, deliver_after, recurrence, kind,
               platform_id, channel_type, thread_id, content
        FROM messages_out
        WHERE deliver_after IS NULL OR deliver_after <= ?
        ORDER BY seq`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []contract.MessageOut
	for rows.Next() {
		var (
			m                                   contract.MessageOut
			inReplyTo, deliverAfter, recurrence sql.NullString
			platformID, channelType, threadID   sql.NullString
			ts                                  string
			kind                                string
		)
		if err := rows.Scan(&m.ID, &m.Seq, &inReplyTo, &ts, &deliverAfter, &recurrence,
			&kind, &platformID, &channelType, &threadID, &m.Content); err != nil {
			return nil, err
		}
		m.Kind = contract.MessageKind(kind)
		m.Timestamp = parseTS(ts)
		if inReplyTo.Valid {
			id := contract.MessageID(inReplyTo.String)
			m.InReplyTo = &id
		}
		m.DeliverAfter = parseTSPtr(deliverAfter)
		m.Recurrence = nullStrPtr(recurrence)
		m.PlatformID = nullStrPtr(platformID)
		m.ChannelType = nullStrPtr(channelType)
		m.ThreadID = nullStrPtr(threadID)
		out = append(out, m)
	}
	return out, rows.Err()
}

// ProcessingAcks returns the sandbox's per-message progress reports.
func (h *hostOutbound) ProcessingAcks() ([]contract.ProcessingAck, error) {
	rows, err := h.db.Query(`SELECT message_id, status, status_changed FROM processing_ack`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []contract.ProcessingAck
	for rows.Next() {
		var a contract.ProcessingAck
		var changed string
		if err := rows.Scan(&a.MessageID, &a.Status, &changed); err != nil {
			return nil, err
		}
		a.StatusChanged = parseTS(changed)
		out = append(out, a)
	}
	return out, rows.Err()
}

// Close closes the underlying handle.
func (h *hostOutbound) Close() error {
	if h.db == nil {
		return nil
	}
	return h.db.Close()
}

// --- small scan/bind helpers ---

func tsString(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func tsPtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return tsString(*t)
}

func parseTS(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func parseTSPtr(ns sql.NullString) *time.Time {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	t := parseTS(ns.String)
	return &t
}

func strPtr(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func nullStrPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
