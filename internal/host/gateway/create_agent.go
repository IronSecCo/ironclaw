package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// This file implements the host-internal verifier + applier for the create_agent
// control-plane action (RFC-0004). create_agent rides the existing gateway
// machinery: the deterministic CreateAgentVerifier validates the request and ALWAYS
// holds it for a human (a new agent is a new trust principal — never auto-approved,
// even if create_agent were mistakenly added to an auto-approve policy,
// because require-human wins over pass in the chain). On approval the
// CreateAgentApplier materializes the agent via a wired creator func.
//
// The gateway package stays free of a registry dependency: the registry slices it
// needs (existence check, materialize) are passed as small function seams the
// daemon wires.

// agentNameRe permits a human-readable agent name: alphanumerics plus spaces and a
// little punctuation. No path separators, no shell metacharacters.
var agentNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]*$`)

// agentFolderRe is the stricter rule for a workspace folder: no spaces either.
var agentFolderRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// createAgentSpec is the minimal shape the verifier/applier read from a
// ChangeCreateAgent payload (== ChangeRequest.After). Extra fields (persona,
// enabled_tools, …) are carried opaquely and applied by the materialize func.
type createAgentSpec struct {
	Name   string `json:"name"`
	Folder string `json:"folder"`
}

// parseCreateAgent decodes a ChangeCreateAgent payload.
func parseCreateAgent(after json.RawMessage) (createAgentSpec, error) {
	var s createAgentSpec
	if len(after) == 0 {
		return s, errors.New("empty payload")
	}
	if err := json.Unmarshal(after, &s); err != nil {
		return s, err
	}
	return s, nil
}

// validateCreateAgent enforces a safe name/folder: present, no traversal, no path
// separators or shell metacharacters.
func validateCreateAgent(s createAgentSpec) error {
	name := strings.TrimSpace(s.Name)
	if name == "" {
		return errors.New("create_agent: name is required")
	}
	if strings.Contains(name, "..") || !agentNameRe.MatchString(name) {
		return fmt.Errorf("create_agent: unsafe name %q", s.Name)
	}
	if f := strings.TrimSpace(s.Folder); f != "" {
		if strings.Contains(f, "..") || !agentFolderRe.MatchString(f) {
			return fmt.Errorf("create_agent: unsafe folder %q", s.Folder)
		}
	}
	return nil
}

// DeriveAgentGroupID derives a deterministic agent-group id from the spec: the
// folder if set, else a slug of the name. Verifier and applier agree on it so the
// existence check and the materialization key the same id.
func DeriveAgentGroupID(after json.RawMessage) (contract.AgentGroupID, error) {
	s, err := parseCreateAgent(after)
	if err != nil {
		return "", err
	}
	return deriveAgentGroupID(s), nil
}

func deriveAgentGroupID(s createAgentSpec) contract.AgentGroupID {
	base := strings.TrimSpace(s.Folder)
	if base == "" {
		base = strings.TrimSpace(s.Name)
	}
	base = strings.ToLower(strings.ReplaceAll(base, " ", "-"))
	return contract.AgentGroupID(base)
}

// AgentExistsFunc reports whether an agent-group id already exists. The daemon
// wires it to registry.GetAgentGroup; a func seam keeps gateway registry-free. A
// nil func is treated as "nothing exists yet".
type AgentExistsFunc func(contract.AgentGroupID) bool

// CreateAgentVerifier validates a ChangeCreateAgent and holds it for a human.
// Other change kinds pass through untouched (like the other deterministic
// verifiers), so it is safe to include in the chain for all kinds.
type CreateAgentVerifier struct {
	exists AgentExistsFunc
}

// NewCreateAgentVerifier constructs the verifier. exists may be nil (skips the
// uniqueness check).
func NewCreateAgentVerifier(exists AgentExistsFunc) CreateAgentVerifier {
	return CreateAgentVerifier{exists: exists}
}

// Name identifies the verifier.
func (CreateAgentVerifier) Name() string { return "create-agent" }

// Verify rejects a malformed/duplicate create_agent and otherwise requires a
// human. It applies ONLY to ChangeCreateAgent; other kinds pass through.
func (v CreateAgentVerifier) Verify(ctx context.Context, req contract.ChangeRequest) (contract.Verdict, string, error) {
	if req.Kind != contract.ChangeCreateAgent {
		return contract.VerdictPass, "", nil
	}
	spec, err := parseCreateAgent(req.After)
	if err != nil {
		return contract.VerdictReject, "create_agent: unparseable payload: " + err.Error(), nil
	}
	if err := validateCreateAgent(spec); err != nil {
		return contract.VerdictReject, err.Error(), nil
	}
	if v.exists != nil && v.exists(deriveAgentGroupID(spec)) {
		return contract.VerdictReject, fmt.Sprintf("create_agent: agent group %q already exists", deriveAgentGroupID(spec)), nil
	}
	return contract.VerdictRequireHuman, "create_agent: a new agent is a new trust principal — human approval required", nil
}

// CreateAgentFunc materializes an approved new agent group. The daemon wires it to
// registry.PutAgentGroup (+ any initial wirings/members); a func seam keeps gateway
// registry-free.
type CreateAgentFunc func(id contract.AgentGroupID, name, folder string) error

// CreateAgentApplier materializes ChangeCreateAgent requests via a wired creator
// func and delegates every other kind to a fallback Applier. The daemon composes
// it as the gateway's single applier so an approved create_agent provisions the
// agent while other kinds apply as before.
type CreateAgentApplier struct {
	create CreateAgentFunc
	next   contract.Applier
}

// NewCreateAgentApplier wraps next so ChangeCreateAgent is materialized by create
// and all other kinds delegate to next. next may be nil (other kinds become a
// no-op). create must be non-nil to actually provision agents.
func NewCreateAgentApplier(create CreateAgentFunc, next contract.Applier) *CreateAgentApplier {
	return &CreateAgentApplier{create: create, next: next}
}

// Apply materializes create_agent or delegates.
func (a *CreateAgentApplier) Apply(ctx context.Context, req contract.ChangeRequest, d contract.Decision) error {
	if req.Kind != contract.ChangeCreateAgent {
		if a.next != nil {
			return a.next.Apply(ctx, req, d)
		}
		return nil
	}
	spec, err := parseCreateAgent(req.After)
	if err != nil {
		return fmt.Errorf("create_agent apply: %w", err)
	}
	if err := validateCreateAgent(spec); err != nil {
		return fmt.Errorf("create_agent apply: %w", err)
	}
	if a.create == nil {
		return errors.New("create_agent apply: no creator func wired")
	}
	id := deriveAgentGroupID(spec)
	folder := strings.TrimSpace(spec.Folder)
	if folder == "" {
		folder = string(id)
	}
	return a.create(id, strings.TrimSpace(spec.Name), folder)
}
