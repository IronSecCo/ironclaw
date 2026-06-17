// OWNER: AGENT2

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// CreateAgentToolName is the tool that requests creation of a NEW agent group
// (RFC-0004, T-086). It equals the host ChangeKind discriminator
// (contract.ChangeCreateAgent) so the host maps the forwarded action to the
// gateway.
const CreateAgentToolName = string(contract.ChangeCreateAgent) // "create_agent"

// CreateAgentTool lets the agent REQUEST a new agent group at runtime. Like the
// capability-change tools it is a HostForwarder: it performs no privileged action
// itself — it emits a contract.SystemAction the loop forwards to the host gateway,
// which ALWAYS requires human approval before a new agent (a new trust principal)
// is materialized. The new agent never inherits the creator's privileges.
type CreateAgentTool struct{}

// NewCreateAgentTool constructs the tool.
func NewCreateAgentTool() *CreateAgentTool { return &CreateAgentTool{} }

var _ HostForwarder = (*CreateAgentTool)(nil)

// Name identifies the tool.
func (t *CreateAgentTool) Name() string { return CreateAgentToolName }

// Description frames the tool's boundary for the model in-band.
func (t *CreateAgentTool) Description() string {
	return "Request creation of a NEW agent group. This does NOT create anything itself: it submits a " +
		"request to the host gateway, which requires human approval (a new agent is a new trust principal " +
		"and is never auto-approved). The new agent starts with a minimal capability set and never inherits " +
		"your privileges."
}

// JSONSchema returns the input schema.
func (t *CreateAgentTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string","description":"Required human-readable name for the new agent."},` +
		`"folder":{"type":"string","description":"Optional workspace folder; derived from the name if omitted. No path separators or \"..\"."},` +
		`"persona":{"type":"object","description":"Optional initial persona, e.g. {\"instructions\":\"...\"}."},` +
		`"enabled_tools":{"type":"array","items":{"type":"string"},"description":"Optional initial enabled tools."},` +
		`"reason":{"type":"string","description":"Why this agent should exist (shown to the human approver)."}` +
		`},"required":["name"],"additionalProperties":false}`)
}

type createAgentInput struct {
	Name         string          `json:"name"`
	Folder       string          `json:"folder"`
	Persona      json.RawMessage `json:"persona"`
	EnabledTools []string        `json:"enabled_tools"`
	Reason       string          `json:"reason"`
}

// Invoke validates input and returns the SystemAction wire body (action ==
// "create_agent", payload == the proposed agent config). The loop forwards it to
// the host, which re-validates (CreateAgentVerifier) and human-gates it. The
// validation here is early UX feedback; the host is the authority.
func (t *CreateAgentTool) Invoke(_ context.Context, input json.RawMessage) (string, error) {
	var in createAgentInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("%s: invalid input: %w", CreateAgentToolName, err)
	}
	if strings.TrimSpace(in.Name) == "" {
		return "", fmt.Errorf("%s: name is required", CreateAgentToolName)
	}
	if err := validateAgentIdentifier("name", in.Name); err != nil {
		return "", err
	}
	if in.Folder != "" {
		if err := validateAgentIdentifier("folder", in.Folder); err != nil {
			return "", err
		}
	}

	payload := map[string]any{"name": strings.TrimSpace(in.Name)}
	if in.Folder != "" {
		payload["folder"] = strings.TrimSpace(in.Folder)
	}
	if len(in.Persona) > 0 {
		payload["persona"] = in.Persona
	}
	if len(in.EnabledTools) > 0 {
		payload["enabled_tools"] = in.EnabledTools
	}
	p, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("%s: marshal payload: %w", CreateAgentToolName, err)
	}
	return contract.MarshalSystemAction(contract.SystemAction{
		Action:  CreateAgentToolName,
		Payload: p,
		Reason:  strings.TrimSpace(in.Reason),
	})
}

// ToHostAction implements HostForwarder: the Invoke output is already the
// system-action wire body, forwarded verbatim after a parse + discriminator check.
func (t *CreateAgentTool) ToHostAction(toolOutput string) (string, error) {
	a := contract.ParseSystemAction(toolOutput)
	if a.Action != CreateAgentToolName {
		return "", fmt.Errorf("%s: expected action %q, got %q", CreateAgentToolName, CreateAgentToolName, a.Action)
	}
	if len(a.Payload) == 0 {
		return "", fmt.Errorf("%s: payload required", CreateAgentToolName)
	}
	return toolOutput, nil
}

// validateAgentIdentifier rejects path traversal and separators in a proposed
// name/folder. The host CreateAgentVerifier re-checks; this is early feedback.
func validateAgentIdentifier(field, v string) error {
	s := strings.TrimSpace(v)
	if strings.Contains(s, "..") || strings.ContainsAny(s, `/\`) {
		return fmt.Errorf("%s: %s must not contain path separators or \"..\"", CreateAgentToolName, field)
	}
	return nil
}
