// OWNER: AGENT1

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

	"github.com/nivardsec/ironclaw/internal/contract"
)

// AgentGroup is a configured agent (workspace, persona, container config). Only
// the host-facing identity fields are modeled here; persona/tooling live behind
// the gateway.
type AgentGroup struct {
	ID     contract.AgentGroupID
	Name   string
	Folder string
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

	// PutAgentGroup inserts or replaces an agent group by ID.
	PutAgentGroup(AgentGroup) error
	// GetAgentGroup returns the agent group by ID; ok is false if absent.
	GetAgentGroup(contract.AgentGroupID) (AgentGroup, bool)
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

	// CanAccess reports whether userID may access agentGroupID, with a reason. The
	// precedence is owner > global-admin > scoped-admin > member.
	CanAccess(userID contract.UserID, agentGroupID contract.AgentGroupID) (bool, string)
	// IsKnownSender reports whether userID is a known principal for agentGroupID
	// (any role, scoped or global, or membership).
	IsKnownSender(userID contract.UserID, agentGroupID contract.AgentGroupID) bool
}
