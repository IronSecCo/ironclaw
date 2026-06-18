// FROZEN CONTRACT — do not edit without a joint RFC (see docs/contract.md).

package contract

import (
	"context"
	"encoding/json"
	"time"
)

// ChangeRequest is the mandatory-mutation protocol payload. Every control-plane
// mutation (persona, enabled tools, packages, wiring, permissions, mounts) is one
// of these. Before/After are canonicalized JSON (sorted keys) so the diff and its
// hash are deterministic.
type ChangeRequest struct {
	ID           ChangeID
	Kind         ChangeKind
	AgentGroupID AgentGroupID
	RequestedBy  UserID
	Before       json.RawMessage
	After        json.RawMessage
	CreatedAt    time.Time
}

// Verifier is a single, DETERMINISTIC check in the gateway chain. It is never an
// LLM. Verdicts are pure functions of the ChangeRequest.
type Verifier interface {
	Name() string
	Verify(ctx context.Context, req ChangeRequest) (Verdict, string, error)
}

// Decision records the recorded outcome of the human (or future automated)
// approval step.
type Decision struct {
	Outcome   string
	DecidedBy UserID
	DecidedAt time.Time
}

// Approver obtains a Decision for a change request that the verifier chain held
// for human review.
type Approver interface {
	RequestDecision(ctx context.Context, req ChangeRequest, reason string) (Decision, error)
}

// Applier performs the mutation. It is idempotent, keyed by req.ID, and
// transactional.
type Applier interface {
	Apply(ctx context.Context, req ChangeRequest, d Decision) error
}

// ChangeStore persists the change lifecycle so it survives restarts.
type ChangeStore interface {
	Put(ChangeRequest) error
	SetDecision(ChangeID, Decision) error
	MarkApplied(ChangeID) error
	Pending() ([]ChangeRequest, error)
}
