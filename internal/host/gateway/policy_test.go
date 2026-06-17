// OWNER: AGENT1

package gateway

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// scriptedApprover returns a queued sequence of decisions on successive
// RequestDecision calls, erroring when exhausted. It lets PolicyApprover RBAC be
// tested without concurrency.
type scriptedApprover struct {
	decisions []contract.Decision
	i         int
}

func (s *scriptedApprover) RequestDecision(_ context.Context, _ contract.ChangeRequest, _ string) (contract.Decision, error) {
	if s.i >= len(s.decisions) {
		return contract.Decision{}, errors.New("scriptedApprover: no more decisions")
	}
	d := s.decisions[s.i]
	s.i++
	return d, nil
}

// TestPolicyVerifierEmptyIsFloor asserts the engine ships inert: with no
// auto-approve kinds it requires a human for every kind, exactly like the
// AlwaysRequireHuman floor it replaces.
func TestPolicyVerifierEmptyIsFloor(t *testing.T) {
	v := NewPolicyVerifier(NewPolicy(PolicyConfig{}))
	for _, k := range []contract.ChangeKind{contract.ChangePersona, contract.ChangeMounts, contract.ChangePermissions} {
		verdict, _, err := v.Verify(context.Background(), contract.ChangeRequest{Kind: k})
		if err != nil {
			t.Fatalf("Verify(%s): %v", k, err)
		}
		if verdict != contract.VerdictRequireHuman {
			t.Fatalf("empty policy on %s = %v, want require-human (floor)", k, verdict)
		}
	}
}

// TestPolicyVerifierAutoApprovesConfiguredKind asserts only enumerated kinds pass.
func TestPolicyVerifierAutoApprovesConfiguredKind(t *testing.T) {
	v := NewPolicyVerifier(NewPolicy(PolicyConfig{AutoApprove: []contract.ChangeKind{contract.ChangePersona}}))

	verdict, _, err := v.Verify(context.Background(), contract.ChangeRequest{Kind: contract.ChangePersona})
	if err != nil {
		t.Fatal(err)
	}
	if verdict != contract.VerdictPass {
		t.Fatalf("persona verdict = %v, want pass (auto-approve)", verdict)
	}

	verdict, _, err = v.Verify(context.Background(), contract.ChangeRequest{Kind: contract.ChangeMounts})
	if err != nil {
		t.Fatal(err)
	}
	if verdict != contract.VerdictRequireHuman {
		t.Fatalf("mounts verdict = %v, want require-human (not in policy)", verdict)
	}
}

// TestPolicyVerifierAutoApprovesEndToEnd asserts that, used as the chain's floor,
// the PolicyVerifier makes an enumerated kind apply without a human while a
// non-enumerated kind still blocks for approval.
func TestPolicyVerifierAutoApprovesEndToEnd(t *testing.T) {
	policy := NewPolicy(PolicyConfig{AutoApprove: []contract.ChangeKind{contract.ChangePersona}})
	applier := NewLogApplier()
	store := NewMemoryStore()
	gw := New(VerifierChain{NewPolicyVerifier(policy)}, NewManualApprover(), applier, store)

	// Persona is auto-approved: Submit returns applied with no human.
	id, err := gw.Submit(context.Background(), contract.ChangeRequest{ID: "p1", Kind: contract.ChangePersona})
	if err != nil {
		t.Fatalf("Submit persona: %v", err)
	}
	if st, _ := store.Status(id); st != string(statusApplied) {
		t.Fatalf("persona status = %q, want applied (auto)", st)
	}
	if len(applier.Applied()) != 1 {
		t.Fatalf("applied = %d, want 1", len(applier.Applied()))
	}

	// Mounts is NOT auto-approved: it blocks for a human (stays pending). Use a
	// cancellable context so the blocked Submit goroutine unwinds at test end.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, _ = gw.Submit(ctx, contract.ChangeRequest{ID: "m1", Kind: contract.ChangeMounts}) }()
	waitPending(t, store, "m1")
	if st, _ := store.Status("m1"); st != string(statusPending) {
		t.Fatalf("mounts status = %q, want pending (require-human)", st)
	}
}

// TestRejectVerifierVetoesAutoApprovedKind asserts a deterministic reject-verifier
// running ahead of the policy still vetoes an otherwise auto-approvable kind — the
// auto-approve set never overrides a hard reject.
func TestRejectVerifierVetoesAutoApprovedKind(t *testing.T) {
	policy := NewPolicy(PolicyConfig{AutoApprove: []contract.ChangeKind{contract.ChangeMounts}})
	applier := NewLogApplier()
	store := NewMemoryStore()
	chain := VerifierChain{MountAllowlistVerifier{AllowedPrefixes: []string{"/srv"}}, NewPolicyVerifier(policy)}
	gw := New(chain, NewManualApprover(), applier, store)

	id, err := gw.Submit(context.Background(), contract.ChangeRequest{
		ID:    "m1",
		Kind:  contract.ChangeMounts,
		After: []byte(`[{"source":"/etc/../root"}]`),
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if st, _ := store.Status(id); st != string(statusRejected) {
		t.Fatalf("status = %q, want rejected (reject-verifier vetoes auto-approve)", st)
	}
	if len(applier.Applied()) != 0 {
		t.Fatalf("applied = %d, want 0 (rejected change must not apply)", len(applier.Applied()))
	}
}

// TestMayApprove covers the RBAC predicate: restricted kinds need a permitted
// role; unrestricted kinds are open.
func TestMayApprove(t *testing.T) {
	policy := NewPolicy(PolicyConfig{
		ApproverRoles: map[contract.ChangeKind][]Role{
			contract.ChangePermissions: {"admin"},
		},
		Principals: map[contract.UserID][]Role{
			"alice": {"admin"},
			"bob":   {"operator"},
		},
	})
	if !policy.MayApprove("alice", contract.ChangePermissions) {
		t.Fatal("alice (admin) should approve permissions")
	}
	if policy.MayApprove("bob", contract.ChangePermissions) {
		t.Fatal("bob (operator) should NOT approve permissions")
	}
	if policy.MayApprove("carol", contract.ChangePermissions) {
		t.Fatal("carol (no roles) should NOT approve permissions")
	}
	if !policy.MayApprove("bob", contract.ChangePersona) {
		t.Fatal("unrestricted kind should be approvable by anyone")
	}
}

// TestPolicyApproverIgnoresUnauthorizedApprove asserts an approval from a
// principal without a permitted role is skipped and the next decision is awaited.
func TestPolicyApproverIgnoresUnauthorizedApprove(t *testing.T) {
	policy := NewPolicy(PolicyConfig{
		ApproverRoles: map[contract.ChangeKind][]Role{contract.ChangePermissions: {"admin"}},
		Principals:    map[contract.UserID][]Role{"alice": {"admin"}, "bob": {"operator"}},
	})
	inner := &scriptedApprover{decisions: []contract.Decision{
		{Outcome: OutcomeApprove, DecidedBy: "bob", DecidedAt: time.Now()},   // unauthorized — ignored
		{Outcome: OutcomeApprove, DecidedBy: "alice", DecidedAt: time.Now()}, // authorized — honored
	}}
	approver := NewPolicyApprover(policy, inner).WithLogger(log.New(io.Discard, "", 0))

	d, err := approver.RequestDecision(context.Background(), contract.ChangeRequest{ID: "c1", Kind: contract.ChangePermissions}, "reason")
	if err != nil {
		t.Fatalf("RequestDecision: %v", err)
	}
	if d.Outcome != OutcomeApprove || d.DecidedBy != "alice" {
		t.Fatalf("decision = %+v, want approve by alice", d)
	}
	if inner.i != 2 {
		t.Fatalf("inner called %d times, want 2 (skipped bob, took alice)", inner.i)
	}
}

// TestPolicyApproverHonorsRejectFromAnyone asserts a veto is always honored,
// regardless of the rejecter's role.
func TestPolicyApproverHonorsRejectFromAnyone(t *testing.T) {
	policy := NewPolicy(PolicyConfig{
		ApproverRoles: map[contract.ChangeKind][]Role{contract.ChangePermissions: {"admin"}},
		Principals:    map[contract.UserID][]Role{"bob": {"operator"}},
	})
	inner := &scriptedApprover{decisions: []contract.Decision{
		{Outcome: OutcomeReject, DecidedBy: "bob", DecidedAt: time.Now()},
	}}
	approver := NewPolicyApprover(policy, inner)

	d, err := approver.RequestDecision(context.Background(), contract.ChangeRequest{ID: "c1", Kind: contract.ChangePermissions}, "reason")
	if err != nil {
		t.Fatalf("RequestDecision: %v", err)
	}
	if d.Outcome != OutcomeReject || d.DecidedBy != "bob" {
		t.Fatalf("decision = %+v, want reject by bob (vetoes never need a role)", d)
	}
}

// TestPolicyApproverPassThroughWhenUnrestricted asserts the wrapper is transparent
// when the policy restricts no kind.
func TestPolicyApproverPassThroughWhenUnrestricted(t *testing.T) {
	inner := &scriptedApprover{decisions: []contract.Decision{
		{Outcome: OutcomeApprove, DecidedBy: "bob", DecidedAt: time.Now()},
	}}
	approver := NewPolicyApprover(NewPolicy(PolicyConfig{}), inner)

	d, err := approver.RequestDecision(context.Background(), contract.ChangeRequest{ID: "c1", Kind: contract.ChangePersona}, "reason")
	if err != nil {
		t.Fatalf("RequestDecision: %v", err)
	}
	if d.Outcome != OutcomeApprove || d.DecidedBy != "bob" {
		t.Fatalf("decision = %+v, want approve by bob (no restriction)", d)
	}
}
