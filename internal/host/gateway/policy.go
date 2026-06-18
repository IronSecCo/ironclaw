package gateway

import (
	"context"
	"fmt"
	"log"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// This file adds an OPT-IN policy engine on top of the mandatory-mutation
// gateway. It does two orthogonal things, both off by default:
//
//   1. Auto-approval — a PolicyVerifier may return VerdictPass for an enumerated
//      set of low-risk ChangeKinds so they apply without a human. With an EMPTY
//      set it returns VerdictRequireHuman for every kind, which is behaviorally
//      identical to AlwaysRequireHuman. It is a drop-in REPLACEMENT for the floor
//      verifier (you build the chain with one or the other, never both, because
//      AlwaysRequireHuman would always elevate the aggregate back to
//      require-human). Deterministic reject-verifiers (mount/package) still run
//      ahead of it and can veto an otherwise-auto-approvable kind.
//
//   2. Approver RBAC — a PolicyApprover wraps the human approver and only honors
//      an APPROVE decision from a principal holding a role permitted for that
//      ChangeKind; an unauthorized approve is ignored (the change stays pending).
//      A REJECT from anyone is always honored — vetoing never requires a role.
//      With no per-kind role restrictions configured it is a transparent
//      pass-through.
//
// The zero-config Policy (NewPolicy(PolicyConfig{})) auto-approves NOTHING and
// restricts NO approver, so wiring the engine in changes nothing until an
// operator opts a kind in. The AlwaysRequireHuman floor remains the default the
// daemon composes; this engine is the opt-in alternative.

// Role is a host-internal authorization role used to scope who may approve a
// change. It is intentionally NOT part of the frozen contract — roles are a
// control-plane concept the gateway owns, invisible to the sandbox seam.
type Role string

// PolicyConfig is the declarative, opt-in policy. Every field empty yields the
// AlwaysRequireHuman floor: nothing auto-approves and no approver is restricted.
type PolicyConfig struct {
	// AutoApprove lists the ChangeKinds a PolicyVerifier may pass without a human.
	// Operators should only list genuinely low-risk kinds (e.g. ChangePersona,
	// ChangeEnabledTools). Empty (the default) preserves the human floor.
	AutoApprove []contract.ChangeKind

	// ApproverRoles restricts, per ChangeKind, the roles permitted to APPROVE that
	// kind at the human gate. A kind with no entry may be approved by anyone (no
	// RBAC). A kind mapped to an empty role list can be approved by no one (locked
	// to humans but un-approvable until the policy is widened) — a deliberate,
	// if strict, configuration.
	ApproverRoles map[contract.ChangeKind][]Role

	// Principals assigns roles to approver identities (matched against a
	// Decision.DecidedBy). An identity absent here holds no roles.
	Principals map[contract.UserID][]Role
}

// Policy is the compiled, lookup-optimized form of a PolicyConfig.
type Policy struct {
	autoApprove   map[contract.ChangeKind]struct{}
	approverRoles map[contract.ChangeKind]map[Role]struct{}
	principals    map[contract.UserID]map[Role]struct{}
}

// NewPolicy compiles a PolicyConfig into a Policy. A nil/zero config is valid and
// yields the floor-preserving empty policy.
func NewPolicy(cfg PolicyConfig) *Policy {
	p := &Policy{
		autoApprove:   make(map[contract.ChangeKind]struct{}, len(cfg.AutoApprove)),
		approverRoles: make(map[contract.ChangeKind]map[Role]struct{}, len(cfg.ApproverRoles)),
		principals:    make(map[contract.UserID]map[Role]struct{}, len(cfg.Principals)),
	}
	for _, k := range cfg.AutoApprove {
		p.autoApprove[k] = struct{}{}
	}
	for k, roles := range cfg.ApproverRoles {
		set := make(map[Role]struct{}, len(roles))
		for _, r := range roles {
			set[r] = struct{}{}
		}
		p.approverRoles[k] = set
	}
	for u, roles := range cfg.Principals {
		set := make(map[Role]struct{}, len(roles))
		for _, r := range roles {
			set[r] = struct{}{}
		}
		p.principals[u] = set
	}
	return p
}

// AutoApproves reports whether kind is in the auto-approve allowlist. A nil
// Policy auto-approves nothing.
func (p *Policy) AutoApproves(kind contract.ChangeKind) bool {
	if p == nil {
		return false
	}
	_, ok := p.autoApprove[kind]
	return ok
}

// HasRole reports whether the principal holds role.
func (p *Policy) HasRole(user contract.UserID, role Role) bool {
	if p == nil {
		return false
	}
	roles, ok := p.principals[user]
	if !ok {
		return false
	}
	_, ok = roles[role]
	return ok
}

// MayApprove reports whether user is permitted to approve a change of kind. A
// kind with no role restriction is approvable by anyone (RBAC is opt-in per
// kind); a restricted kind requires user to hold at least one permitted role. A
// nil Policy imposes no restriction (pass-through).
func (p *Policy) MayApprove(user contract.UserID, kind contract.ChangeKind) bool {
	if p == nil {
		return true
	}
	allowed, restricted := p.approverRoles[kind]
	if !restricted {
		return true
	}
	held, ok := p.principals[user]
	if !ok {
		return false
	}
	for r := range held {
		if _, permitted := allowed[r]; permitted {
			return true
		}
	}
	return false
}

// PolicyVerifier is the opt-in floor verifier: VerdictPass for kinds in the
// policy's auto-approve set, VerdictRequireHuman for everything else. Use it in
// place of AlwaysRequireHuman. With an empty policy it is equivalent to the
// floor.
type PolicyVerifier struct {
	policy *Policy
}

// NewPolicyVerifier constructs a PolicyVerifier over policy. A nil policy is
// treated as the empty (floor-preserving) policy.
func NewPolicyVerifier(policy *Policy) PolicyVerifier {
	return PolicyVerifier{policy: policy}
}

// Name identifies the verifier.
func (PolicyVerifier) Name() string { return "policy-auto-approve" }

// Verify passes kinds the policy auto-approves and requires a human for the rest.
// It never rejects — rejection stays the job of the deterministic reject-verifiers
// that run ahead of it.
func (v PolicyVerifier) Verify(ctx context.Context, req contract.ChangeRequest) (contract.Verdict, string, error) {
	if v.policy.AutoApproves(req.Kind) {
		return contract.VerdictPass, fmt.Sprintf("policy auto-approves kind %q", req.Kind), nil
	}
	return contract.VerdictRequireHuman, fmt.Sprintf("kind %q is not in the auto-approve policy; human approval required", req.Kind), nil
}

// PolicyApprover wraps a human approver and enforces approver-role scoping
// (RBAC). An APPROVE decision is only honored if its DecidedBy principal holds a
// role permitted for the change's kind; otherwise the decision is ignored and the
// approver is polled again, leaving the change pending for a properly-roled
// approver. REJECT decisions are always honored. With a policy that restricts no
// kind, it is a transparent pass-through.
type PolicyApprover struct {
	policy *Policy
	inner  contract.Approver
	logger *log.Logger
}

// NewPolicyApprover wraps inner with RBAC enforcement from policy. inner must be
// non-nil. A nil policy imposes no restriction.
func NewPolicyApprover(policy *Policy, inner contract.Approver) *PolicyApprover {
	return &PolicyApprover{policy: policy, inner: inner}
}

// WithLogger sets the logger used to record refused (unauthorized) approvals. A
// nil logger uses the standard library default. Returns the approver for chaining.
func (a *PolicyApprover) WithLogger(l *log.Logger) *PolicyApprover {
	a.logger = l
	return a
}

func (a *PolicyApprover) logf(format string, args ...any) {
	if a.logger != nil {
		a.logger.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// RequestDecision obtains a decision from the wrapped approver, enforcing that an
// approval comes from a principal authorized for req.Kind. An unauthorized
// approval is logged and skipped; the next decision is awaited.
func (a *PolicyApprover) RequestDecision(ctx context.Context, req contract.ChangeRequest, reason string) (contract.Decision, error) {
	for {
		d, err := a.inner.RequestDecision(ctx, req, reason)
		if err != nil {
			return contract.Decision{}, err
		}
		if d.Outcome == OutcomeApprove && !a.policy.MayApprove(d.DecidedBy, req.Kind) {
			a.logf("host/gateway: refused approval of change id=%s kind=%s by=%s: principal lacks a role permitted to approve this kind",
				req.ID, req.Kind, d.DecidedBy)
			continue
		}
		return d, nil
	}
}
