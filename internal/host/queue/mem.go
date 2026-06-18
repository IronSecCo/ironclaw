package queue

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// In-memory development queue backends.
//
// These let the full control-plane pipeline and its tests run WITHOUT the pending
// encrypted-SQLite binding (RFC-0001). They are NOT the production path — the
// SQLite-gated openInboundRW/OpenInbound/OpenOutbound above remain the real
// implementations and still return the pending-binding error until RFC-0001 lands.
//
// A single memStore backs one inbound view (MemInbound: InboundWriter +
// InboundReader) and one outbound view (MemOutbound: OutboundWriter +
// OutboundReader) so that a host writer and a (test) sandbox reader of the same
// session observe a consistent state, mirroring the two real DB files.
//
// Seq parity is enforced at the Write methods: the host writes EVEN seq numbers
// and the sandbox writes ODD ones, matching the frozen contract rule
// (contract/schema.go).

// memStore is the shared, mutex-guarded backing store for one session's
// in-memory inbound + outbound queues.
type memStore struct {
	mu sync.Mutex

	messagesIn   []contract.MessageIn
	destinations map[string]contract.Destination
	routing      *contract.SessionRouting
	delivered    map[contract.MessageID]string // message_out_id -> platform_message_id

	messagesOut    []contract.MessageOut
	processingAcks map[contract.MessageID]contract.ProcessingAck
	sessionState   map[string]contract.SessionState
}

// NewMemStore constructs an empty shared store.
func NewMemStore() *memStore {
	return &memStore{
		destinations:   make(map[string]contract.Destination),
		delivered:      make(map[contract.MessageID]string),
		processingAcks: make(map[contract.MessageID]contract.ProcessingAck),
		sessionState:   make(map[string]contract.SessionState),
	}
}

// MemInbound is the in-memory inbound queue. It implements BOTH
// contract.InboundWriter (host side) and contract.InboundReader (the sandbox view
// used by tests), backed by a shared memStore.
type MemInbound struct {
	store *memStore
}

// NewMemInbound returns an inbound view over store.
func NewMemInbound(store *memStore) *MemInbound { return &MemInbound{store: store} }

// Compile-time assertions: MemInbound satisfies both inbound interfaces.
var (
	_ contract.InboundWriter = (*MemInbound)(nil)
	_ contract.InboundReader = (*MemInbound)(nil)
)

// WriteMessageIn inserts a message. The host is the inbound writer, so the seq
// MUST be even; an odd seq is rejected to preserve seq parity.
func (m *MemInbound) WriteMessageIn(msg contract.MessageIn) error {
	if msg.Seq%2 != 0 {
		return fmt.Errorf("host/queue: MemInbound.WriteMessageIn requires an EVEN seq (host parity), got %d", msg.Seq)
	}
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	for _, existing := range m.store.messagesIn {
		if existing.ID == msg.ID {
			return fmt.Errorf("host/queue: duplicate message_in id %q", msg.ID)
		}
		if existing.Seq == msg.Seq {
			return fmt.Errorf("host/queue: duplicate message_in seq %d", msg.Seq)
		}
	}
	m.store.messagesIn = append(m.store.messagesIn, msg)
	return nil
}

// UpsertDestinations inserts/replaces destinations by name.
func (m *MemInbound) UpsertDestinations(ds []contract.Destination) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	for _, d := range ds {
		m.store.destinations[d.Name] = d
	}
	return nil
}

// MarkDelivered records a host-side dedup entry (the host never writes outbound).
func (m *MemInbound) MarkDelivered(id contract.MessageID, platformMsgID *string) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	pid := ""
	if platformMsgID != nil {
		pid = *platformMsgID
	}
	m.store.delivered[id] = pid
	return nil
}

// PendingMessages returns inbound messages ordered by seq. firstPoll mirrors the
// real reader's signature; in-memory we return all rows regardless (on_wake
// gating is the host's responsibility on write).
func (m *MemInbound) PendingMessages(firstPoll bool) ([]contract.MessageIn, error) {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	out := make([]contract.MessageIn, 0, len(m.store.messagesIn))
	for _, msg := range m.store.messagesIn {
		if msg.OnWake && !firstPoll {
			continue
		}
		out = append(out, msg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, nil
}

// Destinations returns the destination set.
func (m *MemInbound) Destinations() ([]contract.Destination, error) {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	out := make([]contract.Destination, 0, len(m.store.destinations))
	for _, d := range m.store.destinations {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// SessionRouting returns the session's platform coordinates.
func (m *MemInbound) SessionRouting() (contract.SessionRouting, error) {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	if m.store.routing == nil {
		return contract.SessionRouting{}, nil
	}
	return *m.store.routing, nil
}

// SetSessionRouting is a host-side helper to seed routing (not part of either
// contract interface; used when wiring up a session).
func (m *MemInbound) SetSessionRouting(r contract.SessionRouting) {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	rc := r
	m.store.routing = &rc
}

// Delivered returns a copy of the dedup set (test/inspection helper).
func (m *MemInbound) Delivered() map[contract.MessageID]string {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	out := make(map[contract.MessageID]string, len(m.store.delivered))
	for k, v := range m.store.delivered {
		out[k] = v
	}
	return out
}

// Close is a no-op for the in-memory store.
func (m *MemInbound) Close() error { return nil }

// MemOutbound is the in-memory outbound queue. It implements BOTH
// contract.OutboundWriter (the sandbox view used by tests) and
// contract.OutboundReader (host side), backed by a shared memStore.
type MemOutbound struct {
	store *memStore
}

// NewMemOutbound returns an outbound view over store.
func NewMemOutbound(store *memStore) *MemOutbound { return &MemOutbound{store: store} }

// Compile-time assertions: MemOutbound satisfies both outbound interfaces.
var (
	_ contract.OutboundWriter = (*MemOutbound)(nil)
	_ contract.OutboundReader = (*MemOutbound)(nil)
)

// WriteMessageOut inserts an outbound message. The sandbox is the outbound writer,
// so the seq MUST be odd; an even seq is rejected to preserve seq parity.
func (m *MemOutbound) WriteMessageOut(msg contract.MessageOut) error {
	if msg.Seq%2 == 0 {
		return fmt.Errorf("host/queue: MemOutbound.WriteMessageOut requires an ODD seq (sandbox parity), got %d", msg.Seq)
	}
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	for _, existing := range m.store.messagesOut {
		if existing.ID == msg.ID {
			return fmt.Errorf("host/queue: duplicate message_out id %q", msg.ID)
		}
		if existing.Seq == msg.Seq {
			return fmt.Errorf("host/queue: duplicate message_out seq %d", msg.Seq)
		}
	}
	m.store.messagesOut = append(m.store.messagesOut, msg)
	return nil
}

// MarkProcessing records a processing ack for each id.
func (m *MemOutbound) MarkProcessing(ids []contract.MessageID) error {
	return m.markAck(ids, contract.StatusProcessing)
}

// MarkCompleted records a completed ack for each id.
func (m *MemOutbound) MarkCompleted(ids []contract.MessageID) error {
	return m.markAck(ids, contract.StatusCompleted)
}

func (m *MemOutbound) markAck(ids []contract.MessageID, status string) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	now := time.Now().UTC()
	for _, id := range ids {
		m.store.processingAcks[id] = contract.ProcessingAck{MessageID: id, Status: status, StatusChanged: now}
	}
	return nil
}

// PutSessionState writes a key/value entry of per-session state.
func (m *MemOutbound) PutSessionState(key, value string) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	m.store.sessionState[key] = contract.SessionState{Key: key, Value: value, UpdatedAt: time.Now().UTC()}
	return nil
}

// DueMessages returns outbound messages whose deliver_after is nil or in the
// past, ordered by seq.
func (m *MemOutbound) DueMessages() ([]contract.MessageOut, error) {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	now := time.Now().UTC()
	var out []contract.MessageOut
	for _, msg := range m.store.messagesOut {
		if msg.DeliverAfter == nil || !msg.DeliverAfter.After(now) {
			out = append(out, msg)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, nil
}

// ProcessingAcks returns the sandbox's progress reports.
func (m *MemOutbound) ProcessingAcks() ([]contract.ProcessingAck, error) {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	out := make([]contract.ProcessingAck, 0, len(m.store.processingAcks))
	for _, a := range m.store.processingAcks {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MessageID < out[j].MessageID })
	return out, nil
}

// SessionState returns a copy of the per-session state map (test helper).
func (m *MemOutbound) SessionState() map[string]contract.SessionState {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	out := make(map[string]contract.SessionState, len(m.store.sessionState))
	for k, v := range m.store.sessionState {
		out[k] = v
	}
	return out
}

// Close is a no-op for the in-memory store.
func (m *MemOutbound) Close() error { return nil }
