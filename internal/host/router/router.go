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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
	"github.com/IronSecCo/ironclaw/internal/host/types"
)

// maxEngagePatternInput caps the length of text fed to a user-supplied regexp in
// EvaluateEngage. Without a cap, an adversarial sender could pair a
// catastrophic-backtracking pattern with a long message to stall the router
// (ReDoS). Go's regexp uses RE2 (linear time, no backtracking), so this cap is
// defense-in-depth rather than the sole mitigation.
const maxEngagePatternInput = 64 * 1024

// InboundWriterFactory returns the host's inbound writer for a session. Tests
// inject a fake; production wires it to host/queue.OpenInbound once RFC-0001
// lands. The Router closes the returned writer after each session's write.
type InboundWriterFactory func(contract.SessionID) (contract.InboundWriter, error)

// Waker wakes (launches/signals) the sandbox for a session after a triggering
// message is enqueued. Tests inject a fake; production wires it to host/isolation.
type Waker interface {
	Wake(contract.SessionID) error
}

// WakerFunc adapts a function to the Waker interface.
type WakerFunc func(contract.SessionID) error

// Wake calls f.
func (f WakerFunc) Wake(id contract.SessionID) error { return f(id) }

// Router routes inbound platform messages into per-session inbound queues. It
// fans an InboundEvent out to every wired agent group, gates on engage-mode,
// access, and sender scope, resolves the session, writes a MessageIn, and wakes
// the sandbox when the message triggers.
type Router struct {
	reg       registry.Registry
	newWriter InboundWriterFactory
	waker     Waker
}

// New constructs a Router over the given registry, inbound-writer factory, and
// waker. Any of newWriter/waker may be nil for the pure helper functions
// (NamespaceUserID/EvaluateEngage), but RouteInbound requires all three.
func New(reg registry.Registry, newWriter InboundWriterFactory, waker Waker) *Router {
	return &Router{reg: reg, newWriter: newWriter, waker: waker}
}

// randSuffix returns a short random hex suffix for message IDs.
func randSuffix() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "x"
	}
	return hex.EncodeToString(b[:])
}

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

// RouteInbound routes one normalized platform event to every wired agent group
// and returns one RoutingOutcome per wiring considered.
//
// Flow:
//
//  1. Resolve (get-or-create) the messaging group from (ChannelType, PlatformID,
//     Instance).
//  2. Namespace the sender via NamespaceUserID — never trusting an embedded colon.
//  3. List the wired agent groups for the messaging group.
//  4. For each wiring, in priority order:
//     a. EvaluateEngage(mode, pattern, text, mentioned); for mention-sticky, an
//     existing session also keeps the wiring engaged without a fresh mention.
//     b. registry.CanAccess — deny short-circuits this wiring.
//     c. Sender-scope gate: if SenderScope==known and the sender is not known,
//     skip.
//     d. If engaged: resolve the session, write a MessageIn with trigger=1 (EVEN
//     host seq), and wake the sandbox.
//     e. If not engaged and IgnoredMessagePolicy==accumulate: resolve the session
//     and write a MessageIn with trigger=0 (no wake).
//     f. Otherwise (drop): record a skipped outcome.
//
// The Registry/writer/waker are injected, so tests run without any DB binding.
func (r *Router) RouteInbound(ctx context.Context, ev types.InboundEvent) ([]types.RoutingOutcome, error) {
	if r.reg == nil || r.newWriter == nil || r.waker == nil {
		return nil, errors.New("host/router: RouteInbound requires a registry, inbound-writer factory, and waker")
	}

	mg, err := r.reg.GetOrCreateMessagingGroup(ev.ChannelType, ev.PlatformID, ev.Instance, false, contract.UnknownStrict)
	if err != nil {
		return nil, fmt.Errorf("host/router: resolve messaging group: %w", err)
	}

	sender := NamespaceUserID(ev.ChannelType, ev.SenderHandle)

	wirings, err := r.reg.ListWirings(mg.ID)
	if err != nil {
		return nil, fmt.Errorf("host/router: list wirings: %w", err)
	}

	var outcomes []types.RoutingOutcome
	for _, w := range wirings {
		outcome, err := r.routeOne(ctx, ev, mg, sender, w)
		if err != nil {
			return outcomes, err
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, nil
}

// routeOne evaluates a single wiring and, when appropriate, enqueues a MessageIn.
func (r *Router) routeOne(ctx context.Context, ev types.InboundEvent, mg registry.MessagingGroup, sender contract.UserID, w registry.Wiring) (types.RoutingOutcome, error) {
	out := types.RoutingOutcome{AgentGroupID: w.AgentGroupID}

	engaged, err := EvaluateEngage(w.EngageMode, w.EngagePattern, ev.Text, ev.Mentioned)
	if err != nil {
		out.Reason = "engage-eval-error: " + err.Error()
		return out, nil
	}
	// Sticky continuation: an active session for this thread keeps the wiring
	// engaged on follow-ups even without a fresh mention.
	if !engaged && w.EngageMode == contract.EngageMentionSticky {
		if _, ok := r.reg.FindSession(w.AgentGroupID, mg.ID, ev.ThreadID, w.SessionMode); ok {
			engaged = true
		}
	}

	// Access gate (applies whether engaged or accumulating).
	if allowed, reason := r.reg.CanAccess(sender, w.AgentGroupID); !allowed {
		out.Reason = "access-denied: " + reason
		return out, nil
	}

	// Sender-scope gate: known-only wirings ignore unknown senders.
	if w.SenderScope == contract.SenderKnown && !r.reg.IsKnownSender(sender, w.AgentGroupID) {
		out.Reason = "sender-scope: unknown sender for known-only wiring"
		return out, nil
	}

	if !engaged {
		if w.IgnoredMessagePolicy != contract.IgnoreAccumulate {
			out.Reason = "not-engaged: dropped"
			return out, nil
		}
		// Accumulate: enqueue with trigger=0, no wake.
		sess, err := r.enqueue(ev, mg, w, 0)
		if err != nil {
			return out, err
		}
		out.SessionID = sess.ID
		out.Engaged = false
		out.Reason = "accumulated (trigger=0)"
		return out, nil
	}

	// Engaged: enqueue with trigger=1 and wake the sandbox.
	sess, err := r.enqueue(ev, mg, w, 1)
	if err != nil {
		return out, err
	}
	if err := r.waker.Wake(sess.ID); err != nil {
		return out, fmt.Errorf("host/router: wake %s: %w", sess.ID, err)
	}
	out.SessionID = sess.ID
	out.Engaged = true
	out.Reason = "engaged (trigger=1)"
	return out, nil
}

// enqueue resolves the session and writes a MessageIn with the given trigger.
func (r *Router) enqueue(ev types.InboundEvent, mg registry.MessagingGroup, w registry.Wiring, trigger int) (registry.Session, error) {
	sess, err := r.reg.ResolveSession(w.AgentGroupID, mg.ID, ev.ThreadID, w.SessionMode)
	if err != nil {
		return registry.Session{}, fmt.Errorf("host/router: resolve session: %w", err)
	}
	writer, err := r.newWriter(sess.ID)
	if err != nil {
		return registry.Session{}, fmt.Errorf("host/router: open inbound writer: %w", err)
	}
	defer writer.Close()

	ct := ev.ChannelType
	pid := ev.PlatformID
	msg := contract.MessageIn{
		ID: contract.MessageID(fmt.Sprintf("in_%d_%s", time.Now().UnixNano(), randSuffix())),
		// Seq==0: the inbound writer allocates the next EVEN seq atomically within the
		// INSERT, coordinated with the persisted queue (IRO-278). The router must not
		// mint seqs itself — it shares the messages_in table with delivery and sweep.
		Seq:         0,
		Kind:        contract.KindChat,
		Timestamp:   time.Now().UTC(),
		Status:      "queued",
		Trigger:     trigger,
		PlatformID:  &pid,
		ChannelType: &ct,
		ThreadID:    ev.ThreadID,
		Content:     ev.Text,
	}
	if err := writer.WriteMessageIn(msg); err != nil {
		return registry.Session{}, fmt.Errorf("host/router: write message_in: %w", err)
	}
	return sess, nil
}
