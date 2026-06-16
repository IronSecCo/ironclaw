// OWNER: AGENT1

// Package delivery polls the outbound queue via contract.OutboundReader, delivers
// messages through channel adapters, and dedups in an in-memory delivered set
// (the host never writes outbound). System actions are re-authorized host-side —
// no blind trust — and there is no unapproved script/RCE path: any privileged
// system action routes through the gateway.
package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/channels"
	"github.com/nivardsec/ironclaw/internal/host/gateway"
	"github.com/nivardsec/ironclaw/internal/host/registry"
)

// OutboundReaderFactory returns the host's read-only outbound view for a session.
// Tests inject a fake (host/queue.MemOutbound); production wires it to
// host/queue.OpenOutbound once the encrypted-SQLite binding lands.
type OutboundReaderFactory func(contract.SessionID) (contract.OutboundReader, error)

// Delivery polls outbound queues and delivers via channel adapters. It dedups
// delivered messages in memory (mirrored in the inbound `delivered` table once
// persistence lands) and re-authorizes any privilege-bearing system action the
// sandbox emits through the gateway.
type Delivery struct {
	registry  *channels.Registry
	gw        *gateway.Gateway
	reg       registry.Registry
	newReader OutboundReaderFactory

	mu        sync.Mutex
	delivered map[contract.MessageID]struct{}
}

// New constructs a Delivery.
//
// reg (the control-plane registry) and newReader (the per-session outbound reader
// factory) are required by Poll; channelReg and gw drive delivery and system
// re-authorization respectively.
func New(channelReg *channels.Registry, gw *gateway.Gateway, reg registry.Registry, newReader OutboundReaderFactory) *Delivery {
	return &Delivery{
		registry:  channelReg,
		gw:        gw,
		reg:       reg,
		newReader: newReader,
		delivered: make(map[contract.MessageID]struct{}),
	}
}

// Poll reads due outbound messages for every active session and delivers them.
//
//   - DEDUP: a message already in the in-memory delivered set is skipped (no
//     double-send). This set is mirrored in the inbound `delivered` table once the
//     persistence binding lands.
//   - RE-AUTHORIZE: a KindSystem message is never trusted blindly.
//     authorizeSystemAction decides whether it maps to a privileged change; if so,
//     it is NOT executed here — it is routed to the gateway as a ChangeRequest
//     (human-gated). There is no unapproved script/RCE path.
//   - DELIVER: a non-system message is delivered via the channel adapter for its
//     channel, after enforcing destination permission against the registry (the
//     origin chat is always allowed; any other destination must be a known
//     destination of the agent group).
//
// Poll returns the first error it hits; partial progress (already-delivered
// messages) is retained in the dedup set.
func (d *Delivery) Poll(ctx context.Context) error {
	if d.reg == nil || d.newReader == nil {
		return fmt.Errorf("host/delivery: Poll requires a registry and an outbound-reader factory")
	}
	sessions, err := d.reg.ListSessions()
	if err != nil {
		return fmt.Errorf("host/delivery: list sessions: %w", err)
	}
	for _, sess := range sessions {
		if err := d.pollSession(ctx, sess); err != nil {
			return err
		}
	}
	return nil
}

// pollSession delivers the due messages for one session.
func (d *Delivery) pollSession(ctx context.Context, sess registry.Session) error {
	reader, err := d.newReader(sess.ID)
	if err != nil {
		return fmt.Errorf("host/delivery: open outbound reader for %s: %w", sess.ID, err)
	}
	defer reader.Close()

	due, err := reader.DueMessages()
	if err != nil {
		return fmt.Errorf("host/delivery: due messages for %s: %w", sess.ID, err)
	}
	for _, msg := range due {
		if d.isDelivered(msg.ID) {
			continue
		}
		if err := d.handle(ctx, sess, msg); err != nil {
			return err
		}
		d.markDelivered(msg.ID)
	}
	return nil
}

// handle delivers one message or re-authorizes a system action.
func (d *Delivery) handle(ctx context.Context, sess registry.Session, msg contract.MessageOut) error {
	if msg.Kind == contract.KindSystem {
		return d.handleSystem(ctx, sess, msg)
	}
	return d.deliver(ctx, sess, msg)
}

// handleSystem re-authorizes a system action host-side. A privileged action is
// turned into a gateway ChangeRequest and is NOT executed by delivery; a
// non-privileged informational action delivers like a normal message.
func (d *Delivery) handleSystem(ctx context.Context, sess registry.Session, msg contract.MessageOut) error {
	action := parseSystemAction(msg.Content)
	kind, privileged := authorizeSystemAction(action)
	if !privileged {
		// Informational system message — deliver it like a normal reply.
		return d.deliver(ctx, sess, msg)
	}
	if d.gw == nil {
		// No gateway wired: the safe default is to refuse, never to execute.
		return fmt.Errorf("host/delivery: privileged system action %q refused (no gateway)", action)
	}
	// Route through the gateway. Submit blocks on a human; do it in the background
	// so a single stuck approval does not stall the whole delivery loop. The action
	// is NOT executed here regardless of the outcome — the gateway's Applier owns
	// any mutation.
	after, _ := json.Marshal(msg.Content)
	req := contract.ChangeRequest{
		Kind:         kind,
		AgentGroupID: sess.AgentGroupID,
		RequestedBy:  contract.UserID("sandbox:" + string(sess.ID)),
		After:        after,
	}
	go func() { _, _ = d.gw.Submit(context.Background(), req) }()
	return nil
}

// deliver sends a message through the channel adapter for its channel, after a
// destination-permission check.
func (d *Delivery) deliver(ctx context.Context, sess registry.Session, msg contract.MessageOut) error {
	if !d.destinationAllowed(sess, msg) {
		return fmt.Errorf("host/delivery: destination not permitted for session %s (channel=%v platform=%v)",
			sess.ID, deref(msg.ChannelType), deref(msg.PlatformID))
	}
	channel := deref(msg.ChannelType)
	if channel == "" {
		return fmt.Errorf("host/delivery: message %s has no channel_type", msg.ID)
	}
	adapter, ok := d.registry.Get(channel)
	if !ok {
		return fmt.Errorf("host/delivery: no adapter registered for channel %q", channel)
	}
	if _, err := adapter.Deliver(ctx, msg); err != nil {
		return fmt.Errorf("host/delivery: adapter %q deliver: %w", channel, err)
	}
	return nil
}

// destinationAllowed enforces that a message goes to a permitted place. The origin
// chat (the session's own messaging-group coordinates) is always allowed; any
// other destination must be a registered destination of the agent group.
func (d *Delivery) destinationAllowed(sess registry.Session, msg contract.MessageOut) bool {
	// A message with no explicit channel/platform targets its own session origin —
	// always allowed.
	if msg.ChannelType == nil || msg.PlatformID == nil {
		return true
	}
	// Origin chat: the message targets the session's own messaging group.
	if mg, ok := d.reg.GetMessagingGroup(sess.MessagingGroupID); ok {
		if mg.ChannelType == *msg.ChannelType && mg.PlatformID == *msg.PlatformID {
			return true
		}
	}
	// Any other destination must be explicitly allowed for the agent group.
	return d.reg.IsAllowedDestination(sess.AgentGroupID, *msg.ChannelType, *msg.PlatformID)
}

// --- dedup set ---

func (d *Delivery) isDelivered(id contract.MessageID) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.delivered[id]
	return ok
}

func (d *Delivery) markDelivered(id contract.MessageID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.delivered[id] = struct{}{}
}

// DeliveredCount returns how many distinct messages have been delivered (test
// helper).
func (d *Delivery) DeliveredCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.delivered)
}

// --- system-action authorization ---

// authorizeSystemAction is a pure function mapping a system-action name to the
// gateway ChangeKind it implies and whether it is privileged. A privileged action
// MUST go through the gateway (human-gated) and is never executed by delivery.
//
// This is the deterministic re-authorization choke point: the sandbox cannot
// smuggle a privileged change through a system message because delivery refuses to
// act on it and instead emits a gateway ChangeRequest. The set is conservative —
// anything unrecognized that looks privilege-bearing is treated as privileged.
func authorizeSystemAction(action string) (contract.ChangeKind, bool) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "set_persona", "persona":
		return contract.ChangePersona, true
	case "set_enabled_tools", "enabled_tools":
		return contract.ChangeEnabledTools, true
	case "install_packages", "packages", "add_package":
		return contract.ChangePackages, true
	case "set_wiring", "wiring":
		return contract.ChangeWiring, true
	case "set_permissions", "permissions", "grant_role", "add_member":
		return contract.ChangePermissions, true
	case "add_mount", "mounts", "set_mounts":
		return contract.ChangeMounts, true
	case "script", "exec", "run", "shell":
		// An unapproved script/RCE path: there is NO direct execution. Map it to the
		// most privileged change kind so it is always human-gated, never run inline.
		return contract.ChangePermissions, true
	case "typing", "presence", "ack", "noop", "":
		// Informational, non-privileged: safe to deliver directly.
		return "", false
	default:
		// Unknown action: refuse to execute, treat as privileged so it is gated.
		return contract.ChangePermissions, true
	}
}

// parseSystemAction extracts the action name from a system message body. The body
// may be a bare action string or a JSON object {"action": "..."}. Parsing is
// best-effort; an unparseable body yields the trimmed raw content.
func parseSystemAction(content string) string {
	c := strings.TrimSpace(content)
	if strings.HasPrefix(c, "{") {
		var obj struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal([]byte(c), &obj); err == nil && obj.Action != "" {
			return obj.Action
		}
	}
	return c
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
