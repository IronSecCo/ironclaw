// FROZEN CONTRACT — do not edit without a joint RFC (see docs/contract.md).

package contract

// MessageKind classifies an inbound or outbound message.
type MessageKind string

const (
	KindChat    MessageKind = "chat"
	KindTask    MessageKind = "task"
	KindWebhook MessageKind = "webhook"
	KindSystem  MessageKind = "system"
)

// EngageMode controls when a wired agent engages with a messaging group.
type EngageMode string

const (
	EngagePattern       EngageMode = "pattern"
	EngageMention       EngageMode = "mention"
	EngageMentionSticky EngageMode = "mention-sticky"
)

// SenderScope gates which senders an agent will react to.
type SenderScope string

const (
	SenderAll   SenderScope = "all"
	SenderKnown SenderScope = "known"
)

// IgnoredMessagePolicy controls what happens to messages that do not engage.
type IgnoredMessagePolicy string

const (
	IgnoreDrop       IgnoredMessagePolicy = "drop"
	IgnoreAccumulate IgnoredMessagePolicy = "accumulate"
)

// UnknownSenderPolicy controls how messages from unregistered senders are handled.
type UnknownSenderPolicy string

const (
	UnknownStrict          UnknownSenderPolicy = "strict"
	UnknownRequestApproval UnknownSenderPolicy = "request_approval"
	UnknownPublic          UnknownSenderPolicy = "public"
)

// SessionMode controls how sessions are partitioned for a wiring.
type SessionMode string

const (
	SessionShared      SessionMode = "shared"
	SessionPerThread   SessionMode = "per-thread"
	SessionAgentShared SessionMode = "agent-shared"
)

// ChangeKind enumerates the control-plane mutations that flow through the gateway.
type ChangeKind string

const (
	ChangePersona      ChangeKind = "persona"
	ChangeEnabledTools ChangeKind = "enabled_tools"
	ChangePackages     ChangeKind = "packages"
	ChangeWiring       ChangeKind = "wiring"
	ChangePermissions  ChangeKind = "permissions"
	ChangeMounts       ChangeKind = "mounts"
	// ChangeCreateAgent provisions a NEW agent group (RFC-0004). Privileged:
	// always routed through the gateway's mandatory human-approval floor — a new
	// agent is a new trust principal and is never auto-approved. It rides the
	// existing SystemAction envelope (action == "create_agent"); the payload
	// describes the proposed agent group (see docs/contract.md). This is the only
	// frozen-contract change — a2a messaging needs none.
	ChangeCreateAgent ChangeKind = "create_agent"
	// ChangeMCPAccess grants an agent group access to a host-configured MCP (Model
	// Context Protocol) server and a NAMED subset of its tools (RFC-0005).
	// Privileged: it widens the agent's tool surface with externally-served tools, so
	// it always routes through the gateway's mandatory human-approval floor — the
	// human approves a named server AND named tools, never a blind "whatever the
	// server happens to expose" surface (the blind-MCP-approval gap IronClaw exists to
	// close). It rides the existing SystemAction envelope (action == "mcp_access");
	// the payload names the server + tools (see docs/contract.md). MCP servers run
	// HOST-SIDE — a stdio subprocess or a remote HTTP endpoint behind a per-session
	// broker unix socket — so the sandbox stays network=none and never speaks MCP
	// itself. This is the only frozen-contract change.
	ChangeMCPAccess ChangeKind = "mcp_access"
)

// Verdict is the deterministic result of a single verifier in the gateway chain.
type Verdict int

const (
	VerdictPass Verdict = iota
	VerdictReject
	VerdictRequireHuman
)
