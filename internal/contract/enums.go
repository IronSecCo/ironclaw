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
)

// Verdict is the deterministic result of a single verifier in the gateway chain.
type Verdict int

const (
	VerdictPass Verdict = iota
	VerdictReject
	VerdictRequireHuman
)
