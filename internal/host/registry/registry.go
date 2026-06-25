// Package registry is the control-plane's own data model: agent groups, messaging
// groups, wirings, sessions, users, roles, and members. It is host-internal — the
// sandbox never sees it — so it is NOT part of the frozen contract.
//
// The Registry interface lets the router, delivery, and sweep packages run
// against an in-memory backend today (MemRegistry) and a durable, encrypted-store
// backend later, without code changes. Access precedence (owner > global-admin >
// scoped-admin > member) and session partitioning (shared / per-thread /
// agent-shared) are implemented here so they are exercised by tests independent of
// any database binding.
package registry

import (
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// AgentGroup is a configured agent (workspace, persona, container config). Only
// the host-facing identity fields are modeled here; persona/tooling live behind
// the gateway.
type AgentGroup struct {
	ID     contract.AgentGroupID
	Name   string
	Folder string
	// Provider and Model select this group's model backend. An empty
	// Provider selects the default Anthropic backend, so existing groups are
	// unaffected. They are consumed at sandbox launch (see
	// internal/host/session.Manager); the host model-proxy must have the chosen
	// provider's credential enabled for it to be reachable.
	Provider string
	Model    string
	// Project and Location are the Google Cloud project id and region for the
	// "vertex" provider; both ride in the Vertex AI request URL path. Ignored by
	// every other provider. Empty Location uses the Vertex default region.
	Project  string
	Location string
	// Persona is the group's legacy single-blob system-persona text, appended to the
	// sandbox system prompt at launch. It is set via a gateway-approved
	// ChangePersona change — never by the sandbox itself — and is read-only to the
	// agent. Superseded by PersonaDocs for new agents: ComposePersona prefers the
	// structured docs and falls back to this field when none are set, so existing
	// groups are unaffected.
	Persona string
	// PersonaDocs is the structured, multi-document persona (OpenClaw-style separation
	// of concerns): a section key ("identity"/"soul"/"instructions" — see
	// PersonaSectionKeys) → its markdown. ComposePersona renders the known sections, in
	// canonical order, into the single system-persona string the sandbox receives, so
	// nothing downstream (the --persona arg, the frozen contract) changes. Operator-set
	// via the registry write path / builder; read-only to the agent. Empty falls back to
	// the legacy Persona field above.
	PersonaDocs map[string]string
	// EnabledTools optionally restricts the group to a subset of the compiled sandbox
	// tools. Empty (the default) means ALL compiled tools, so existing groups are
	// unaffected. Set only via a gateway-approved ChangeEnabledTools; enforced at
	// launch (the mandatory request/ask tools are always kept).
	EnabledTools []string
	// InstalledSkills records the skills installed into this group via gateway-approved
	// installs. At launch each one's bundle is bound read-only at
	// /skills/<name>. Empty (the default) mounts nothing.
	InstalledSkills []InstalledSkill
	// GrantedMCP records the MCP (Model Context Protocol) servers this group may use,
	// each with the NAMED subset of that server's tools a human approved. It is
	// set only via a gateway-approved ChangeMCPAccess — never by the sandbox. At launch
	// the host MCP broker exposes exactly these servers' approved tools to the sandbox
	// over a per-session unix socket; empty (the default) gives the sandbox no MCP
	// surface at all.
	GrantedMCP []GrantedMCPServer
}

// InstalledSkill is one gateway-approved skill installed into an agent group. Its
// read-only assets are bound at /skills/<Name> from the curated source's
// <Name>/<Version> bundle directory.
type InstalledSkill struct {
	Name    string
	Version string
}

// GrantedMCPServer is one gateway-approved MCP-server access grant on an agent group:
// the server name (a key into the host MCP catalog) plus the named subset of its
// tools the human approved. An empty Tools means "all of the server's currently
// declared tools" — the human approved the server wholesale.
type GrantedMCPServer struct {
	Server string
	Tools  []string
}

// MessagingGroup is a single chat/channel on one platform. The triple
// (ChannelType, PlatformID, Instance) uniquely identifies it.
type MessagingGroup struct {
	ID                  contract.MessagingGroupID
	ChannelType         string
	PlatformID          string
	Instance            string
	IsGroup             bool
	UnknownSenderPolicy contract.UnknownSenderPolicy
}

// Wiring links a messaging group to an agent group with engage/session policy.
type Wiring struct {
	ID                   string
	MessagingGroupID     contract.MessagingGroupID
	AgentGroupID         contract.AgentGroupID
	EngageMode           contract.EngageMode
	EngagePattern        string
	SenderScope          contract.SenderScope
	IgnoredMessagePolicy contract.IgnoredMessagePolicy
	SessionMode          contract.SessionMode
	Priority             int
}

// Session is a per-(agent group, messaging group, thread) conversation with its
// own sandbox and queue pair.
type Session struct {
	ID               contract.SessionID
	AgentGroupID     contract.AgentGroupID
	MessagingGroupID contract.MessagingGroupID
	ThreadID         *string
	ContainerStatus  string
	LastActive       time.Time
}

// Destination is one (channel, platform) coordinate an agent group is allowed to
// send to. It is the structured form returned by ListDestinations.
type Destination struct {
	AgentGroupID contract.AgentGroupID `json:"agentGroupId"`
	ChannelType  string                `json:"channelType"`
	PlatformID   string                `json:"platformID"`
}

// User is a platform identity (id is "<channel>:<handle>").
type User struct {
	ID          contract.UserID
	Kind        string
	DisplayName string
}

// Role grants a user owner or admin privilege. A nil AgentGroupID is a global
// role; a non-nil one scopes the role to that agent group.
type Role struct {
	UserID       contract.UserID
	Role         string // "owner" | "admin"
	AgentGroupID *contract.AgentGroupID
}

// Member records unprivileged access of a user to an agent group.
type Member struct {
	UserID       contract.UserID
	AgentGroupID contract.AgentGroupID
}

// Role string constants.
const (
	RoleOwner = "owner"
	RoleAdmin = "admin"
)

// Registry is the control-plane data model. Implementations must be safe for
// concurrent use.
type Registry interface {
	// GetOrCreateMessagingGroup resolves the messaging group for the
	// (channelType, platformID, instance) triple, creating it (with the given
	// default unknown-sender policy and isGroup flag) if absent.
	GetOrCreateMessagingGroup(channelType, platformID, instance string, isGroup bool, policy contract.UnknownSenderPolicy) (MessagingGroup, error)

	// ListWirings returns the wirings for a messaging group, ordered by descending
	// Priority (highest first), ties broken by wiring ID for determinism.
	ListWirings(mgID contract.MessagingGroupID) ([]Wiring, error)

	// GetMessagingGroup returns a messaging group by ID; ok is false if absent.
	GetMessagingGroup(contract.MessagingGroupID) (MessagingGroup, bool)
	// ListMessagingGroups returns all messaging groups, ordered by ID. The read
	// counterpart used by the web console's channel pickers. Empty (never nil).
	ListMessagingGroups() []MessagingGroup

	// PutAgentGroup inserts or replaces an agent group by ID.
	PutAgentGroup(AgentGroup) error
	// GetAgentGroup returns the agent group by ID; ok is false if absent.
	GetAgentGroup(contract.AgentGroupID) (AgentGroup, bool)
	// ListAgentGroups returns all agent groups, ordered by ID for determinism.
	// The read counterpart of PutAgentGroup, used by admin/management surfaces
	// (the web console's agent picker + builder). Empty (never nil) when none.
	ListAgentGroups() []AgentGroup
	// PutWiring inserts or replaces a wiring by ID.
	PutWiring(Wiring) error

	// PutUser inserts or replaces a user by ID.
	PutUser(User) error
	// GetUser returns a user by ID; ok is false if absent.
	GetUser(contract.UserID) (User, bool)

	// GrantRole adds a role. Granting an identical role twice is a no-op.
	GrantRole(Role) error
	// RevokeRole removes a matching role (same user, role, and scope).
	RevokeRole(Role) error
	// AddMember adds an unprivileged member. Adding twice is a no-op.
	AddMember(Member) error
	// RemoveMember removes a member.
	RemoveMember(Member) error

	// ResolveSession returns (creating if needed) the session for the
	// (agentGroupID, messagingGroupID, threadID) tuple under the session mode.
	ResolveSession(agentGroupID contract.AgentGroupID, messagingGroupID contract.MessagingGroupID, threadID *string, mode contract.SessionMode) (Session, error)
	// ListSessions returns all sessions.
	ListSessions() ([]Session, error)
	// GetSession returns a session by ID; ok is false if absent.
	GetSession(contract.SessionID) (Session, bool)
	// FindSession returns an existing session for the tuple/mode without creating
	// one; ok is false if none exists. Used by sticky-engage continuation.
	FindSession(agentGroupID contract.AgentGroupID, messagingGroupID contract.MessagingGroupID, threadID *string, mode contract.SessionMode) (Session, bool)

	// AddDestination records that agentGroupID may send to (channelType,
	// platformID). Adding the same destination twice is a no-op.
	AddDestination(agentGroupID contract.AgentGroupID, channelType, platformID string) error
	// IsAllowedDestination reports whether agentGroupID may send to (channelType,
	// platformID). The session's own origin chat is handled by the caller and does
	// not require a destination row.
	IsAllowedDestination(agentGroupID contract.AgentGroupID, channelType, platformID string) bool
	// ListDestinations returns the destinations agentGroupID may send to — the read
	// counterpart of AddDestination, used by admin/management surfaces (the web
	// console). The result is empty (never nil) for an agent group with none.
	ListDestinations(agentGroupID contract.AgentGroupID) []Destination

	// CanAccess reports whether userID may access agentGroupID, with a reason. The
	// precedence is owner > global-admin > scoped-admin > member.
	CanAccess(userID contract.UserID, agentGroupID contract.AgentGroupID) (bool, string)
	// IsKnownSender reports whether userID is a known principal for agentGroupID
	// (any role, scoped or global, or membership).
	IsKnownSender(userID contract.UserID, agentGroupID contract.AgentGroupID) bool
}
