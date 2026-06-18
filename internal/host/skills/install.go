package skills

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// MountPrefix is where a skill's read-only assets are mounted in the sandbox
// (the mount enforces nosuid,nodev,noexec). One skill → one mount at /skills/<name>.
const MountPrefix = "/skills/"

// SkillInstall is the payload (ChangeRequest.After) of a skill-install change. It
// is the human-readable bundle the approver sees: exactly which persona text,
// tools, egress hosts, and read-only asset mount the skill wants — and NOTHING
// else. There is deliberately no command/script/rootfs field: a skill grants
// already-compiled capabilities, it never introduces code (the sealed-runtime
// invariant), so an approved install can only ever touch config.
type SkillInstall struct {
	Skill   string   `json:"skill"`
	Version string   `json:"version"`
	Persona string   `json:"persona,omitempty"`
	Tools   []string `json:"tools,omitempty"`
	Egress  []string `json:"egress,omitempty"`
	Mount   string   `json:"mount,omitempty"`
	Assets  []string `json:"assets,omitempty"`
}

// BuildChangeRequest synthesizes the ONE gateway ChangeRequest that installs a
// (already validated, see Load/Parse) skill manifest into an agent group. The
// grants are bundled into a single capability-change payload so the human
// approver decides the whole skill at once.
//
// Kind is ChangePermissions: a skill install is a bundle of capability grants,
// and the frozen contract has no dedicated skill kind (adding one would need an
// RFC). ChangePermissions has no special verifier/applier that would
// mis-interpret the payload, so the change rides the gateway's AlwaysRequireHuman
// floor exactly like any other capability change — no auto-approval, ever. The ID
// is left empty for the gateway/submit path to assign.
func BuildChangeRequest(m *Manifest, agentGroupID contract.AgentGroupID, requestedBy contract.UserID) (contract.ChangeRequest, error) {
	if m == nil {
		return contract.ChangeRequest{}, fmt.Errorf("skills: nil manifest")
	}
	if !validName(m.Name) {
		return contract.ChangeRequest{}, fmt.Errorf("skills: manifest must be validated before install (bad name %q)", m.Name)
	}
	if agentGroupID == "" {
		return contract.ChangeRequest{}, fmt.Errorf("skills: install requires a target agent group id")
	}

	payload := SkillInstall{
		Skill:   m.Name,
		Version: m.Version,
		Persona: m.Grants.Persona,
		Tools:   m.Grants.Tools,
		Egress:  m.Grants.Egress,
		Assets:  m.Grants.Assets,
	}
	if len(m.Grants.Assets) > 0 {
		payload.Mount = MountPrefix + m.Name
	}

	after, err := json.Marshal(payload)
	if err != nil {
		return contract.ChangeRequest{}, fmt.Errorf("skills: marshal install payload: %w", err)
	}

	return contract.ChangeRequest{
		Kind:         contract.ChangePermissions,
		AgentGroupID: agentGroupID,
		RequestedBy:  requestedBy,
		After:        after,
		CreatedAt:    time.Now().UTC(),
	}, nil
}

// InstallChange resolves name@version from a curated, signature-verifying Source
// (the Resolver) and maps the resulting validated manifest to its install
// ChangeRequest. This is the host-side flow `ironctl skill add` drives:
// fetch + verify + validate happen inside Resolve, so an unsigned or out-of-policy
// skill never reaches the gateway. It never executes skill content.
func InstallChange(resolver *Resolver, name, version string, agentGroupID contract.AgentGroupID, requestedBy contract.UserID) (contract.ChangeRequest, error) {
	if resolver == nil {
		return contract.ChangeRequest{}, fmt.Errorf("skills: nil resolver")
	}
	m, err := resolver.Resolve(name, version)
	if err != nil {
		return contract.ChangeRequest{}, err
	}
	return BuildChangeRequest(m, agentGroupID, requestedBy)
}
