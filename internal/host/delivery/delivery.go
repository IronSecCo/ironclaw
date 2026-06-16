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
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/channels"
	"github.com/nivardsec/ironclaw/internal/host/gateway"
	"github.com/nivardsec/ironclaw/internal/host/registry"
	"github.com/nivardsec/ironclaw/internal/host/scheduling"
)

// OutboundReaderFactory returns the host's read-only outbound view for a session.
// Tests inject a fake (host/queue.MemOutbound); production wires it to
// host/queue.OpenOutbound once the encrypted-SQLite binding lands.
type OutboundReaderFactory func(contract.SessionID) (contract.OutboundReader, error)

// InboundWriterFactory returns the host's inbound writer for a session. Used by
// the schedule_task system action to enqueue a future inbound prompt. Tests inject
// a fake (host/queue.MemInbound); production wires it to host/queue.OpenInbound
// once the encrypted-SQLite binding lands. May be nil if scheduling is not wired —
// in that case schedule_task actions are refused rather than executed.
type InboundWriterFactory func(contract.SessionID) (contract.InboundWriter, error)

// Delivery polls outbound queues and delivers via channel adapters. It dedups
// delivered messages in memory (mirrored in the inbound `delivered` table once
// persistence lands) and re-authorizes any privilege-bearing system action the
// sandbox emits through the gateway.
type Delivery struct {
	registry  *channels.Registry
	gw        *gateway.Gateway
	reg       registry.Registry
	newReader OutboundReaderFactory
	newWriter InboundWriterFactory // optional; enables schedule_task

	mu        sync.Mutex
	delivered map[contract.MessageID]struct{}
	// seqCtr generates EVEN host seq numbers for scheduled inbound messages,
	// matching the frozen host-parity rule. Process-local and monotonic.
	seqCtr int64
	// scheduleCtr disambiguates generated scheduled-message IDs within a process.
	scheduleCtr int64
}

// New constructs a Delivery.
//
// reg (the control-plane registry) and newReader (the per-session outbound reader
// factory) are required by Poll; channelReg and gw drive delivery and system
// re-authorization respectively. The inbound-writer factory for schedule_task is
// optional; set it with WithInboundWriter.
func New(channelReg *channels.Registry, gw *gateway.Gateway, reg registry.Registry, newReader OutboundReaderFactory) *Delivery {
	return &Delivery{
		registry:  channelReg,
		gw:        gw,
		reg:       reg,
		newReader: newReader,
		delivered: make(map[contract.MessageID]struct{}),
	}
}

// WithInboundWriter wires the inbound-writer factory used by the schedule_task
// system action to enqueue a future inbound prompt. Returns d for chaining.
func (d *Delivery) WithInboundWriter(f InboundWriterFactory) *Delivery {
	d.newWriter = f
	return d
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
	// schedule_task is an allowed, NON-privileged host action: it only enqueues a
	// future inbound prompt (no execution path, no RCE). Handle it before the
	// privilege routing.
	if strings.EqualFold(strings.TrimSpace(action), "schedule_task") {
		return d.handleScheduleTask(sess, msg)
	}
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

// scheduleTaskPayload is the body of a schedule_task system message. It carries
// ONLY a prompt plus timing — there is no script/command field, by design.
type scheduleTaskPayload struct {
	Action     string `json:"action"`
	Prompt     string `json:"prompt"`
	RunAt      string `json:"run_at"` // RFC3339; empty/"" means "now"
	Recurrence string `json:"recurrence"`
}

// handleScheduleTask enqueues a future inbound prompt for the session. It NEVER
// executes anything: it validates the request via scheduling.Validate and writes a
// single MessageIn with ProcessAfter=RunAt (and Recurrence carried so the sweep
// can re-enqueue it). The sweep wakes the session when the message comes due.
func (d *Delivery) handleScheduleTask(sess registry.Session, msg contract.MessageOut) error {
	if d.newWriter == nil {
		return fmt.Errorf("host/delivery: schedule_task refused for session %s (no inbound-writer wired)", sess.ID)
	}
	var p scheduleTaskPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(msg.Content)), &p); err != nil {
		return fmt.Errorf("host/delivery: schedule_task body for %s is not valid JSON: %w", sess.ID, err)
	}
	runAt := time.Now().UTC()
	if strings.TrimSpace(p.RunAt) != "" {
		t, err := time.Parse(time.RFC3339, p.RunAt)
		if err != nil {
			return fmt.Errorf("host/delivery: schedule_task run_at for %s is not RFC3339: %w", sess.ID, err)
		}
		runAt = t.UTC()
	}
	req := scheduling.ScheduledRequest{Prompt: p.Prompt, RunAt: runAt, Recurrence: p.Recurrence}
	if err := scheduling.Validate(req); err != nil {
		return fmt.Errorf("host/delivery: schedule_task invalid for %s: %w", sess.ID, err)
	}

	writer, err := d.newWriter(sess.ID)
	if err != nil {
		return fmt.Errorf("host/delivery: open inbound writer for %s: %w", sess.ID, err)
	}
	defer writer.Close()

	in := contract.MessageIn{
		ID:           d.nextScheduledID(sess.ID),
		Seq:          d.nextEvenSeq(),
		Kind:         contract.KindTask,
		Timestamp:    time.Now().UTC(),
		Status:       "scheduled",
		ProcessAfter: &runAt,
		Content:      req.Prompt,
	}
	if req.Recurrence != "" {
		rec := req.Recurrence
		in.Recurrence = &rec
	}
	if err := writer.WriteMessageIn(in); err != nil {
		return fmt.Errorf("host/delivery: enqueue scheduled message for %s: %w", sess.ID, err)
	}
	return nil
}

// nextEvenSeq returns the next EVEN host seq (frozen host-parity rule).
func (d *Delivery) nextEvenSeq() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seqCtr += 2
	return d.seqCtr
}

// nextScheduledID returns a process-unique id for a scheduled inbound message.
func (d *Delivery) nextScheduledID(sess contract.SessionID) contract.MessageID {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.scheduleCtr++
	return contract.MessageID(fmt.Sprintf("sched_%s_%d", sess, d.scheduleCtr))
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
	case "schedule_task", "schedule":
		// Scheduling carries ONLY a prompt and enqueues a future inbound message —
		// there is no script/command field and nothing is executed here, so it is a
		// non-privileged host action. (Delivery special-cases it before reaching this
		// switch; it is listed here so the authorization map is complete and any
		// privileged future action that prompt requests still passes through the
		// gateway.)
		return "", false
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
