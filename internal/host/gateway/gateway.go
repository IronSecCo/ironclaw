// Package gateway is the single choke point through which every control-plane
// mutation flows (persona, enabled tools, packages, wiring, permissions, mounts).
// There is no file-edit path. A deterministic verifier chain runs first, then a
// human approval step, then an idempotent apply. The v1 floor is one verifier,
// AlwaysRequireHuman, so every mutation hits a human.
package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// Decision outcomes recorded on a Decision.Outcome. These are the canonical
// string values the API and CLI exchange.
const (
	OutcomeApprove = "approve"
	OutcomeReject  = "reject"
)

// VerifierChain is an ordered list of deterministic verifiers. Run aggregates
// their verdicts: any reject short-circuits to reject; otherwise any
// require-human elevates to require-human; otherwise all-pass.
type VerifierChain []contract.Verifier

// Run executes the chain against req and returns the aggregate verdict plus a
// human-readable reason. The first verifier to reject short-circuits the chain
// (no later verifier runs). If no verifier rejects but at least one requires a
// human, the aggregate is VerdictRequireHuman. If every verifier passes the
// aggregate is VerdictPass.
func (vc VerifierChain) Run(ctx context.Context, req contract.ChangeRequest) (contract.Verdict, string, error) {
	agg := contract.VerdictPass
	reason := "all verifiers passed"
	for _, v := range vc {
		verdict, why, err := v.Verify(ctx, req)
		if err != nil {
			return contract.VerdictReject, fmt.Sprintf("%s: error: %v", v.Name(), err), err
		}
		switch verdict {
		case contract.VerdictReject:
			// Short-circuit: reject is terminal.
			return contract.VerdictReject, fmt.Sprintf("%s: %s", v.Name(), why), nil
		case contract.VerdictRequireHuman:
			if agg != contract.VerdictRequireHuman {
				agg = contract.VerdictRequireHuman
				reason = fmt.Sprintf("%s: %s", v.Name(), why)
			}
		case contract.VerdictPass:
			// keep going
		}
	}
	return agg, reason, nil
}

// AlwaysRequireHuman is the v1 floor verifier: it never rejects and never passes
// outright — every change is held for a human decision.
type AlwaysRequireHuman struct{}

// Name identifies the verifier.
func (AlwaysRequireHuman) Name() string { return "always-require-human" }

// Verify always returns VerdictRequireHuman.
func (AlwaysRequireHuman) Verify(ctx context.Context, req contract.ChangeRequest) (contract.Verdict, string, error) {
	return contract.VerdictRequireHuman, "v1 floor: all mutations require human approval", nil
}

// changeStatus tracks the lifecycle state of a stored change.
type changeStatus string

const (
	statusPending  changeStatus = "pending"
	statusApproved changeStatus = "approved"
	statusRejected changeStatus = "rejected"
	statusApplied  changeStatus = "applied"
)

// storedChange is the MemoryStore's per-change record.
type storedChange struct {
	req      contract.ChangeRequest
	status   changeStatus
	decision *contract.Decision
}

// MemoryStore is an in-memory contract.ChangeStore. It is mutex-guarded and safe
// for concurrent use. It is the v1 implementation; a durable store (survives
// restart) replaces it without touching the gateway logic.
type MemoryStore struct {
	mu      sync.Mutex
	changes map[contract.ChangeID]*storedChange
}

// NewMemoryStore constructs an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{changes: make(map[contract.ChangeID]*storedChange)}
}

// Put inserts a change as pending. An existing change with the same ID is left
// untouched (idempotent submit).
func (s *MemoryStore) Put(req contract.ChangeRequest) error {
	if req.ID == "" {
		return errors.New("host/gateway: MemoryStore.Put requires a non-empty ChangeID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.changes[req.ID]; ok {
		return nil
	}
	s.changes[req.ID] = &storedChange{req: req, status: statusPending}
	return nil
}

// SetDecision records a decision and moves the change to approved or rejected.
func (s *MemoryStore) SetDecision(id contract.ChangeID, d contract.Decision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.changes[id]
	if !ok {
		return fmt.Errorf("host/gateway: unknown change %q", id)
	}
	dc := d
	c.decision = &dc
	switch d.Outcome {
	case OutcomeApprove:
		c.status = statusApproved
	case OutcomeReject:
		c.status = statusRejected
	default:
		return fmt.Errorf("host/gateway: unknown decision outcome %q", d.Outcome)
	}
	return nil
}

// MarkApplied marks a change applied (terminal success state).
func (s *MemoryStore) MarkApplied(id contract.ChangeID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.changes[id]
	if !ok {
		return fmt.Errorf("host/gateway: unknown change %q", id)
	}
	c.status = statusApplied
	return nil
}

// Pending returns all changes still awaiting a decision.
func (s *MemoryStore) Pending() ([]contract.ChangeRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []contract.ChangeRequest
	for _, c := range s.changes {
		if c.status == statusPending {
			out = append(out, c.req)
		}
	}
	return out, nil
}

// Status returns the lifecycle status string for a change (test/inspection
// helper). The second return is false if the change is unknown.
func (s *MemoryStore) Status(id contract.ChangeID) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.changes[id]
	if !ok {
		return "", false
	}
	return string(c.status), true
}

// ManualApprover blocks RequestDecision until a decision for that ChangeID is
// delivered via Decide, or until the context is cancelled. Decisions are routed
// over per-ID channels guarded by a mutex.
type ManualApprover struct {
	mu      sync.Mutex
	waiters map[contract.ChangeID]chan contract.Decision
}

// NewManualApprover constructs a ManualApprover.
func NewManualApprover() *ManualApprover {
	return &ManualApprover{waiters: make(map[contract.ChangeID]chan contract.Decision)}
}

// channel returns (creating if needed) the per-ID buffered delivery channel.
func (m *ManualApprover) channel(id contract.ChangeID) chan contract.Decision {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch, ok := m.waiters[id]
	if !ok {
		// Buffered so a Decide that races ahead of RequestDecision is not lost.
		ch = make(chan contract.Decision, 1)
		m.waiters[id] = ch
	}
	return ch
}

// RequestDecision blocks until a decision for req.ID arrives via Decide or ctx is
// cancelled.
func (m *ManualApprover) RequestDecision(ctx context.Context, req contract.ChangeRequest, reason string) (contract.Decision, error) {
	ch := m.channel(req.ID)
	defer func() {
		m.mu.Lock()
		delete(m.waiters, req.ID)
		m.mu.Unlock()
	}()
	select {
	case d := <-ch:
		return d, nil
	case <-ctx.Done():
		return contract.Decision{}, ctx.Err()
	}
}

// Decide delivers a decision to whoever is (or will be) waiting on that ChangeID.
func (m *ManualApprover) Decide(id contract.ChangeID, d contract.Decision) error {
	ch := m.channel(id)
	select {
	case ch <- d:
		return nil
	default:
		return fmt.Errorf("host/gateway: a decision is already pending delivery for %q", id)
	}
}

// LogApplier is the v1 contract.Applier: it logs the applied change via the
// standard library log package and records it in memory. It stores nothing
// external. A real applier performs the transactional DB mutation.
type LogApplier struct {
	mu      sync.Mutex
	applied []contract.ChangeID
}

// NewLogApplier constructs a LogApplier.
func NewLogApplier() *LogApplier { return &LogApplier{} }

// Apply records and logs the change. It is idempotent: applying the same ID twice
// is a no-op on the second call.
func (a *LogApplier) Apply(ctx context.Context, req contract.ChangeRequest, d contract.Decision) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, id := range a.applied {
		if id == req.ID {
			return nil
		}
	}
	a.applied = append(a.applied, req.ID)
	log.Printf("host/gateway: applied change id=%s kind=%s group=%s by=%s decided-by=%s",
		req.ID, req.Kind, req.AgentGroupID, req.RequestedBy, d.DecidedBy)
	return nil
}

// Applied returns the list of applied change IDs (test/inspection helper).
func (a *LogApplier) Applied() []contract.ChangeID {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]contract.ChangeID, len(a.applied))
	copy(out, a.applied)
	return out
}

// DecisionRecorder is the metrics sink for gateway decisions. The daemon passes
// *metrics.Metrics, which records ironclaw_gateway_decisions_total by outcome via
// GatewayDecision. A tiny interface so the gateway does not import the metrics
// package and tests can assert against a fake.
type DecisionRecorder interface {
	// GatewayDecision records one terminal decision; approved selects the series.
	GatewayDecision(approved bool)
}

// Gateway implements the mandatory-mutation protocol over the contract types.
type Gateway struct {
	chain    VerifierChain
	approver contract.Approver
	applier  contract.Applier
	store    contract.ChangeStore
	audit    *AuditLog        // nil = audit disabled (Append is a safe no-op)
	metrics  DecisionRecorder // nil = decisions not metered
}

// New constructs a Gateway from its collaborators. The chain runs first
// (deterministic), then the approver (human gate), then the applier.
func New(chain VerifierChain, approver contract.Approver, applier contract.Applier, store contract.ChangeStore) *Gateway {
	return &Gateway{chain: chain, approver: approver, applier: applier, store: store}
}

// SetAudit attaches an append-only audit log. A nil log disables auditing. It
// returns the gateway for chaining.
func (g *Gateway) SetAudit(a *AuditLog) *Gateway {
	g.audit = a
	return g
}

// SetMetrics attaches the decision recorder. A nil recorder leaves decisions
// unmetered. It returns the gateway for chaining. Every terminal decision —
// verifier reject, human approve/reject, and auto-approve — is recorded by outcome.
func (g *Gateway) SetMetrics(r DecisionRecorder) *Gateway {
	g.metrics = r
	return g
}

// recordDecision meters one terminal decision. Nil-safe.
func (g *Gateway) recordDecision(approved bool) {
	if g.metrics != nil {
		g.metrics.GatewayDecision(approved)
	}
}

// Submit runs the full mandatory-mutation flow for req:
//
//  1. Assign an ID (if empty) and CreatedAt (if zero); Put it pending.
//  2. Run the verifier chain.
//  3. On reject: record a reject decision and return (no apply).
//  4. On require-human: block on the approver. On approve: apply + mark applied +
//     record decision. On reject: record decision (no apply).
//  5. On all-pass (no human required): apply + mark applied + record an
//     auto-approve decision.
//
// It returns the assigned ChangeID. A non-nil error means the flow could not be
// completed (e.g. the approval context was cancelled); the change remains in
// whatever state the store last recorded.
func (g *Gateway) Submit(ctx context.Context, req contract.ChangeRequest) (contract.ChangeID, error) {
	if req.ID == "" {
		id, err := newChangeID()
		if err != nil {
			return "", err
		}
		req.ID = id
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	}
	if err := g.store.Put(req); err != nil {
		return req.ID, err
	}
	_ = g.audit.Append(AuditEntry{Stage: AuditSubmit, ChangeID: req.ID, Kind: req.Kind, Detail: string(req.RequestedBy)})

	verdict, reason, err := g.chain.Run(ctx, req)
	if err != nil {
		// A verifier errored; treat as reject and record it.
		d := contract.Decision{Outcome: OutcomeReject, DecidedBy: "verifier", DecidedAt: time.Now().UTC()}
		_ = g.store.SetDecision(req.ID, d)
		g.recordDecision(false)
		_ = g.audit.Append(AuditEntry{Stage: AuditVerdict, ChangeID: req.ID, Kind: req.Kind, Detail: "error: " + err.Error()})
		return req.ID, err
	}
	_ = g.audit.Append(AuditEntry{Stage: AuditVerdict, ChangeID: req.ID, Kind: req.Kind, Detail: verdictString(verdict) + ": " + reason})

	switch verdict {
	case contract.VerdictReject:
		d := contract.Decision{Outcome: OutcomeReject, DecidedBy: "verifier", DecidedAt: time.Now().UTC()}
		g.recordDecision(false)
		_ = g.audit.Append(AuditEntry{Stage: AuditDecision, ChangeID: req.ID, Kind: req.Kind, Detail: "reject (verifier): " + reason})
		return req.ID, g.store.SetDecision(req.ID, d)

	case contract.VerdictRequireHuman:
		d, err := g.approver.RequestDecision(ctx, req, reason)
		if err != nil {
			return req.ID, err
		}
		if err := g.store.SetDecision(req.ID, d); err != nil {
			return req.ID, err
		}
		g.recordDecision(d.Outcome == OutcomeApprove)
		_ = g.audit.Append(AuditEntry{Stage: AuditDecision, ChangeID: req.ID, Kind: req.Kind, Detail: d.Outcome + " by " + string(d.DecidedBy)})
		if d.Outcome != OutcomeApprove {
			return req.ID, nil
		}
		if err := g.applier.Apply(ctx, req, d); err != nil {
			return req.ID, err
		}
		_ = g.audit.Append(AuditEntry{Stage: AuditApply, ChangeID: req.ID, Kind: req.Kind})
		return req.ID, g.store.MarkApplied(req.ID)

	default: // VerdictPass
		d := contract.Decision{Outcome: OutcomeApprove, DecidedBy: "auto", DecidedAt: time.Now().UTC()}
		if err := g.store.SetDecision(req.ID, d); err != nil {
			return req.ID, err
		}
		g.recordDecision(true)
		_ = g.audit.Append(AuditEntry{Stage: AuditDecision, ChangeID: req.ID, Kind: req.Kind, Detail: "auto-approve (all verifiers passed)"})
		if err := g.applier.Apply(ctx, req, d); err != nil {
			return req.ID, err
		}
		_ = g.audit.Append(AuditEntry{Stage: AuditApply, ChangeID: req.ID, Kind: req.Kind})
		return req.ID, g.store.MarkApplied(req.ID)
	}
}

// verdictString renders a verdict for the audit log.
func verdictString(v contract.Verdict) string {
	switch v {
	case contract.VerdictPass:
		return "pass"
	case contract.VerdictReject:
		return "reject"
	case contract.VerdictRequireHuman:
		return "require-human"
	default:
		return "unknown"
	}
}

// Pending passes through to the store's pending list.
func (g *Gateway) Pending() ([]contract.ChangeRequest, error) {
	return g.store.Pending()
}

// Decide passes a decision through to the ManualApprover. It errors if the
// gateway's approver is not a *ManualApprover.
func (g *Gateway) Decide(id contract.ChangeID, d contract.Decision) error {
	ma, ok := g.approver.(*ManualApprover)
	if !ok {
		return errors.New("host/gateway: approver does not support Decide (not a ManualApprover)")
	}
	return ma.Decide(id, d)
}

// newChangeID returns a random hex change identifier.
func newChangeID() (contract.ChangeID, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return contract.ChangeID("chg_" + hex.EncodeToString(b[:])), nil
}
