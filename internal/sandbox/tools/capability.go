package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// CapabilityChangeToolName is the name of the gateway-bound capability-change
// request tool. The loop recognizes a tool result from this tool and forwards
// the envelope to the outbound queue as a system message for the host gateway.
const CapabilityChangeToolName = "request_capability_change"

// CapabilityChange is the structured request a sandbox emits when it wants a
// control-plane capability changed. The sandbox NEVER applies such a change; it
// emits this envelope, the loop writes it to the outbound queue as a system
// message, and the host gateway turns it into a contract.ChangeRequest that goes
// through the verifier chain and a mandatory human approval.
type CapabilityChange struct {
	Kind    contract.ChangeKind `json:"kind"`
	Payload json.RawMessage     `json:"payload"`
	Reason  string              `json:"reason,omitempty"`
}

// validChangeKinds is the set of control-plane mutations the gateway accepts,
// mirrored from the contract enum so an invalid kind is rejected in-sandbox
// before it ever reaches the queue.
var validChangeKinds = map[contract.ChangeKind]struct{}{
	contract.ChangePersona:      {},
	contract.ChangeEnabledTools: {},
	contract.ChangePackages:     {},
	contract.ChangeWiring:       {},
	contract.ChangePermissions:  {},
	contract.ChangeMounts:       {},
	contract.ChangeMCPAccess:    {},
	contract.ChangeSkillInstall: {},
}

type requestCapabilityChangeInput struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
	Reason  string          `json:"reason"`
}

// RequestCapabilityChangeTool lets the agent ASK for a capability change. It is
// the sanctioned, gateway-bound alternative to the forbidden direct tools
// (install_packages, add_mcp_server, self-edit). It performs no privileged
// action: it validates the request and returns a CapabilityChange envelope for
// the loop to forward to the host gateway.
type RequestCapabilityChangeTool struct{}

// NewRequestCapabilityChangeTool constructs the capability-change request tool.
func NewRequestCapabilityChangeTool() *RequestCapabilityChangeTool {
	return &RequestCapabilityChangeTool{}
}

func (t *RequestCapabilityChangeTool) Name() string { return CapabilityChangeToolName }

func (t *RequestCapabilityChangeTool) Description() string {
	return "Request a control-plane capability change. This does NOT apply the change: it submits a request to " +
		"the host gateway, which a human must approve before it takes effect (after approval it takes effect on " +
		"your next message). Use this — never claim you cannot do something without first offering to request it. " +
		"Common requests and their payloads:\n" +
		"- Enable a built-in tool you don't currently have (e.g. web_search): kind \"enabled_tools\", payload " +
		"{\"add\": [\"web_search\"]}. The \"add\" form unions the tool into your set without disturbing the rest.\n" +
		"- Reach a new external API/website: prefer the dedicated request_api_access tool. (Equivalent here: kind " +
		"\"wiring\", payload {\"egress\": [\"api.example.com\"]}.)\n" +
		"- Use a host-configured MCP (Model Context Protocol) server's tools: kind \"mcp_access\", payload " +
		"{\"server\": \"<name>\", \"tools\": [\"<tool>\", ...]} (omit \"tools\" to request all of the server's tools). " +
		"You can only name a server an operator has already configured; the human approves the named server and tools.\n" +
		"- Add/install a Skill: kind \"skill_install\", payload {\"skill\": \"<name>\", \"version\": \"<version>\"}. You can only " +
		"NAME a skill the operator has curated and signed — you cannot author skill content. The host resolves and " +
		"signature-verifies the named skill, the human approves the exact persona/tools/egress it grants, and it then " +
		"mounts and takes effect on your next message (same session).\n" +
		"Also supports persona, packages, permissions, and mounts. Always include a clear reason for the human approver."
}

func (t *RequestCapabilityChangeTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"kind":{"type":"string","enum":["persona","enabled_tools","packages","wiring","permissions","mounts","mcp_access","skill_install"],"description":"The kind of control-plane change requested."},` +
		`"payload":{"type":"object","description":"The proposed new configuration for this kind."},` +
		`"reason":{"type":"string","description":"Why the change is needed (shown to the human approver)."}` +
		`},"required":["kind","payload"],"additionalProperties":false}`)
}

// Invoke validates the requested change and returns the JSON-encoded
// CapabilityChange envelope. It deliberately does not mutate anything.
func (t *RequestCapabilityChangeTool) Invoke(_ context.Context, input json.RawMessage) (string, error) {
	var in requestCapabilityChangeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("request_capability_change: invalid input: %w", err)
	}
	kind := contract.ChangeKind(in.Kind)
	if _, ok := validChangeKinds[kind]; !ok {
		return "", fmt.Errorf("request_capability_change: unknown change kind %q", in.Kind)
	}
	if len(in.Payload) == 0 {
		return "", fmt.Errorf("request_capability_change: payload is required")
	}
	if !json.Valid(in.Payload) {
		return "", fmt.Errorf("request_capability_change: payload is not valid JSON")
	}

	envelope := CapabilityChange{Kind: kind, Payload: in.Payload, Reason: in.Reason}
	out, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("request_capability_change: marshal envelope: %w", err)
	}
	return string(out), nil
}

// ToHostAction implements HostForwarder: it re-renders the tool's CapabilityChange
// output into the host's system-action wire format so host delivery parses it and
// routes it through the mandatory gateway.
func (t *RequestCapabilityChangeTool) ToHostAction(toolOutput string) (string, error) {
	cc, err := ParseCapabilityChange(toolOutput)
	if err != nil {
		return "", err
	}
	return cc.SystemActionJSON()
}

// ParseCapabilityChange decodes a CapabilityChange envelope produced by
// RequestCapabilityChangeTool.Invoke and validates its kind.
func ParseCapabilityChange(s string) (CapabilityChange, error) {
	var cc CapabilityChange
	if err := json.Unmarshal([]byte(s), &cc); err != nil {
		return CapabilityChange{}, fmt.Errorf("parse capability change: %w", err)
	}
	if _, ok := validChangeKinds[cc.Kind]; !ok {
		return CapabilityChange{}, fmt.Errorf("parse capability change: unknown kind %q", cc.Kind)
	}
	return cc, nil
}

// SystemActionJSON renders the capability change in the frozen system-action wire
// format (contract.SystemAction, keyed on "action"). The loop writes this as the
// Content of a KindSystem outbound message; host delivery re-authorizes it through
// the mandatory gateway. The ChangeKind string is the action discriminator, so it
// maps 1:1 to the host's recognized capability actions.
func (c CapabilityChange) SystemActionJSON() (string, error) {
	s, err := contract.MarshalSystemAction(contract.SystemAction{
		Action:  string(c.Kind),
		Payload: c.Payload,
		Reason:  c.Reason,
	})
	if err != nil {
		return "", fmt.Errorf("render system action: %w", err)
	}
	return s, nil
}
