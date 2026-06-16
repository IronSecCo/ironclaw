// OWNER: AGENT1

// Package types holds the small shared host-internal structs that flow between
// the router, delivery, and sweep packages. They are control-plane-internal and
// are NOT part of the frozen contract — the sandbox never sees them.
package types

import (
	"encoding/json"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// InboundEvent is a normalized platform message handed to the router. It is
// produced by a channel adapter from a raw platform payload.
type InboundEvent struct {
	// ChannelType identifies the platform (e.g. "slack", "discord").
	ChannelType string
	// PlatformID is the platform's chat/channel identifier.
	PlatformID string
	// Instance is the adapter-instance name; it defaults to ChannelType when the
	// adapter does not run multiple instances.
	Instance string
	// ThreadID, when non-nil, scopes the event to a thread within the chat.
	ThreadID *string
	// SenderHandle is the platform-native sender handle. The router namespaces it
	// via router.NamespaceUserID — it is NEVER trusted to carry its own ":".
	SenderHandle string
	// Text is the message body.
	Text string
	// Mentioned reports whether the agent was mentioned in the message.
	Mentioned bool
	// Raw is the untouched platform payload, retained for adapters/debugging.
	Raw json.RawMessage
}

// RoutingOutcome is the per-wiring result of routing one InboundEvent. The router
// returns one of these for every wired agent group it considered.
type RoutingOutcome struct {
	// AgentGroupID is the wired agent group this outcome concerns.
	AgentGroupID contract.AgentGroupID
	// SessionID is the resolved session, set when the event engaged (or was
	// accumulated into) a session; empty when the event was skipped before session
	// resolution.
	SessionID contract.SessionID
	// Engaged reports whether the event triggered the agent (trigger=1). A false
	// value with a non-empty SessionID means the message was accumulated
	// (trigger=0) under an accumulate policy.
	Engaged bool
	// Reason is a short human-readable explanation of the outcome.
	Reason string
}
