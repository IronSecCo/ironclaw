// OWNER: AGENT1

// Package router performs inbound routing: messaging-group resolution, fan-out to
// wired agent groups, engage-mode evaluation, session resolution, and
// sender/access gating. It writes inbound via contract.InboundWriter.
//
// Identity is ALWAYS namespaced as userID = channelType + ":" + handle; the
// handle's own content is never trusted to carry a colon (this closes the
// identity-spoofing bug from the design plan).
package router

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// maxEngagePatternInput caps the length of text fed to a user-supplied regexp in
// EvaluateEngage. Without a cap, an adversarial sender could pair a
// catastrophic-backtracking pattern with a long message to stall the router
// (ReDoS). Go's regexp uses RE2 (linear time, no backtracking), so this cap is
// defense-in-depth rather than the sole mitigation.
const maxEngagePatternInput = 64 * 1024

// Router routes inbound platform messages into per-session inbound queues.
type Router struct{}

// New constructs a Router.
func New() *Router { return &Router{} }

// NamespaceUserID returns the canonical, spoofing-safe user identifier
// channelType + ":" + handle.
//
// The handle is never trusted to carry its own ":" — a malicious sender could
// otherwise pass handle="other:owner" to impersonate "other:owner" under a
// different channel. We strip everything from the first colon onward in the
// handle so the namespace separator is unambiguous and attacker-controlled colons
// cannot forge a different (channel, handle) pair.
func NamespaceUserID(channelType, handle string) contract.UserID {
	channelType = strings.TrimSpace(channelType)
	handle = strings.TrimSpace(handle)
	if i := strings.IndexByte(handle, ':'); i >= 0 {
		handle = handle[:i]
	}
	return contract.UserID(channelType + ":" + handle)
}

// EvaluateEngage decides whether a message engages a wiring under the given mode.
//
//   - EngagePattern: pattern is treated as a regexp matched against text. A
//     pattern of "." (the reference's match-all sentinel) matches anything. An
//     invalid regexp returns (false, nil) — never a panic. Input is capped at
//     maxEngagePatternInput bytes as ReDoS defense-in-depth.
//   - EngageMention: engages iff the agent was mentioned.
//   - EngageMentionSticky: engages iff mentioned. Note: stickiness (continuing to
//     engage follow-ups without a fresh mention) ALSO depends on an existing
//     session for the thread; that part is resolved by RouteInbound, which has the
//     session table. This pure function only evaluates the mention signal.
func EvaluateEngage(mode contract.EngageMode, pattern, text string, mentioned bool) (bool, error) {
	switch mode {
	case contract.EngagePattern:
		if pattern == "." {
			return true, nil
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return false, nil
		}
		if len(text) > maxEngagePatternInput {
			text = text[:maxEngagePatternInput]
		}
		return re.MatchString(text), nil
	case contract.EngageMention:
		return mentioned, nil
	case contract.EngageMentionSticky:
		// Sticky continuation is handled in RouteInbound against the session table.
		return mentioned, nil
	default:
		return false, errors.New("host/router: unknown engage mode")
	}
}

// RouteInbound processes pending inbound platform messages and writes them into
// the resolved sessions' inbound queues.
//
// Full flow (gated on the central-DB binding — see RFC-0001 in docs/contract.md
// for the inbound-DB write seam this needs):
//
//  1. Drain pending platform messages from the channel adapters.
//  2. For each, resolve the messaging group from (channelType, platformID).
//  3. Namespace the sender via NamespaceUserID and apply sender/access gating
//     (SenderScope, UnknownSenderPolicy, membership/role checks).
//  4. Fan out to every wired agent group; for each wiring run EvaluateEngage
//     (plus session-aware sticky continuation) to decide engagement.
//  5. Resolve or create the session per SessionMode (shared / per-thread /
//     agent-shared) and obtain its SessionKey from host/keys.
//  6. Open the inbound queue read/write (contract.OpenInboundRW — pending,
//     RFC-0001) and WriteMessageIn with an EVEN seq; UpsertDestinations.
//  7. Wake the sandbox (touch/launch via host/isolation).
//
// It returns a not-implemented error until the DB binding lands.
func (r *Router) RouteInbound(ctx context.Context) error {
	return errors.New("host/router: RouteInbound not implemented — gated on inbound-DB write seam (RFC-0001)")
}
