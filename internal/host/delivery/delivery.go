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
	"log"
	"strings"
	"sync"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/channels"
	"github.com/IronSecCo/ironclaw/internal/host/gateway"
	"github.com/IronSecCo/ironclaw/internal/host/questions"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
	"github.com/IronSecCo/ironclaw/internal/host/scheduling"
)

// OutboundReaderFactory returns the host's read-only outbound view for a session.
// Tests inject a fake (host/queue.MemOutbound); production wires it to
// host/queue.OpenOutbound once the encrypted-SQLite binding lands.
type OutboundReaderFactory func(contract.SessionID) (contract.OutboundReader, error)

// SkillInstallResolver resolves a NAMED, curated, signed skill into its gateway
// install ChangeRequest — resolve = fetch from the curated source + minisign-verify
// against the trust root + manifest-validate against the compiled tool set. Satisfied
// host-side by skills.InstallChange over the configured skills.Resolver (cmd/controlplane);
// a func seam so delivery does not import the skills package.
//
// This is the trust gate for the in-session skill-install proposal (RFC-0006): the
// sandbox may only NAME skill@version, so an unsigned/unknown/out-of-policy skill never
// becomes a ChangeRequest. The returned request rides ChangePermissions (the resolved
// bundle the human approves), identical to the operator `ironctl skill add` path. May be
// nil — then a sandbox skill_install proposal is refused rather than honored.
type SkillInstallResolver func(skill, version string, group contract.AgentGroupID, requestedBy contract.UserID) (contract.ChangeRequest, error)

// InboundWriterFactory returns the host's inbound writer for a session. Used by
// the schedule_task system action to enqueue a future inbound prompt. Tests inject
// a fake (host/queue.MemInbound); production wires it to host/queue.OpenInbound
// once the encrypted-SQLite binding lands. May be nil if scheduling is not wired —
// in that case schedule_task actions are refused rather than executed.
type InboundWriterFactory func(contract.SessionID) (contract.InboundWriter, error)

// Counter is the minimal metrics sink for the outbound-delivery counter. The
// daemon passes the ironclaw_deliveries_total counter; *metrics.Counter satisfies
// it. A tiny interface so delivery does not import the metrics package and tests
// can assert against a fake.
type Counter interface {
	Inc()
}

// Delivery polls outbound queues and delivers via channel adapters. It dedups
// delivered messages in memory (mirrored in the inbound `delivered` table once
// persistence lands) and re-authorizes any privilege-bearing system action the
// sandbox emits through the gateway.
type Delivery struct {
	registry   *channels.Registry
	gw         *gateway.Gateway
	reg        registry.Registry
	newReader  OutboundReaderFactory
	newWriter  InboundWriterFactory // optional; enables schedule_task
	questions  *questions.Store     // optional; enables ask_user_question tracking
	skillProp  SkillInstallResolver // optional; enables the in-session skill_install proposal
	deliveries Counter              // optional; counts each successful channel send

	mu        sync.Mutex
	delivered map[contract.MessageID]struct{}
	// seqCtr generates EVEN host seq numbers for scheduled inbound messages,
	// matching the frozen host-parity rule. Process-local and monotonic.
	seqCtr int64
	// scheduleCtr disambiguates generated scheduled-message IDs within a process.
	scheduleCtr int64
	// a2aCtr disambiguates generated agent-to-agent inbound message IDs.
	a2aCtr int64
	// a2aHops tracks the incoming a2a chain depth per session so a2a fan-out cannot
	// ping-pong indefinitely (RFC-0004). Guarded by mu.
	a2aHops map[contract.SessionID]int
	// a2aHopLimit caps the a2a chain depth (default defaultA2AHopLimit).
	a2aHopLimit int
	// a2aQuota bounds per-agent-group a2a send rate (default defaultA2ASendsPerMinute/min).
	a2aQuota *a2aQuota
}

// New constructs a Delivery.
//
// reg (the control-plane registry) and newReader (the per-session outbound reader
// factory) are required by Poll; channelReg and gw drive delivery and system
// re-authorization respectively. The inbound-writer factory for schedule_task is
// optional; set it with WithInboundWriter.
func New(channelReg *channels.Registry, gw *gateway.Gateway, reg registry.Registry, newReader OutboundReaderFactory) *Delivery {
	return &Delivery{
		registry:    channelReg,
		gw:          gw,
		reg:         reg,
		newReader:   newReader,
		delivered:   make(map[contract.MessageID]struct{}),
		a2aHops:     make(map[contract.SessionID]int),
		a2aHopLimit: defaultA2AHopLimit,
		a2aQuota:    newA2AQuota(defaultA2ASendsPerMinute),
	}
}

// WithA2ALimits overrides the agent-to-agent safety bounds (RFC-0004): the maximum
// chain hop depth and the per-agent-group send quota per minute. A non-positive
// value keeps the default. Returns d for chaining; intended for the daemon's
// config and for tests.
func (d *Delivery) WithA2ALimits(hopLimit, sendsPerMinute int) *Delivery {
	if hopLimit > 0 {
		d.a2aHopLimit = hopLimit
	}
	if sendsPerMinute > 0 {
		d.a2aQuota = newA2AQuota(sendsPerMinute)
	}
	return d
}

// WithInboundWriter wires the inbound-writer factory used by the schedule_task
// system action to enqueue a future inbound prompt. Returns d for chaining.
func (d *Delivery) WithInboundWriter(f InboundWriterFactory) *Delivery {
	d.newWriter = f
	return d
}

// WithQuestions wires the pending-question store the ask_user_question system
// action records into (RFC-0003). Returns d for chaining. When unset, an
// ask_user_question is recognized as non-privileged but recorded nowhere (logged).
func (d *Delivery) WithQuestions(s *questions.Store) *Delivery {
	d.questions = s
	return d
}

// WithSkillResolver wires the curated, signature-verifying resolver that backs the
// in-session skill_install proposal (RFC-0006). Returns d for chaining. When unset
// (skills not enabled on the control-plane) a sandbox skill_install proposal is
// refused — the sandbox can ask, but only a curated+signed skill an operator has
// provisioned can ever be proposed, and a human still approves it.
func (d *Delivery) WithSkillResolver(f SkillInstallResolver) *Delivery {
	d.skillProp = f
	return d
}

// WithMetrics wires the counter incremented once per outbound message that is
// successfully handed to a channel adapter (ironclaw_deliveries_total). Returns d
// for chaining. When unset, delivery records no metric. Agent-to-agent routing and
// gateway-routed system actions are not channel sends and are not counted.
func (d *Delivery) WithMetrics(deliveries Counter) *Delivery {
	d.deliveries = deliveries
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
	// Agent-to-agent (RFC-0004): an outbound chat addressed to the "agent" sentinel
	// channel is routed INBOUND to the target agent group, not to a platform
	// adapter. The target group id rides in PlatformID.
	if deref(msg.ChannelType) == agentChannel {
		return d.handleA2A(sess, msg)
	}
	return d.deliver(ctx, sess, msg)
}

// handleSystem re-authorizes a system action host-side. A privileged action is
// turned into a gateway ChangeRequest and is NOT executed by delivery; a
// non-privileged informational action delivers like a normal message.
func (d *Delivery) handleSystem(ctx context.Context, sess registry.Session, msg contract.MessageOut) error {
	action := contract.SystemActionName(msg.Content)
	// schedule_task is an allowed, NON-privileged host action: it only enqueues a
	// future inbound prompt (no execution path, no RCE). Handle it before the
	// privilege routing.
	if strings.EqualFold(strings.TrimSpace(action), contract.ActionScheduleTask) {
		return d.handleScheduleTask(sess, msg)
	}
	// ask_user_question is also a NON-privileged host action (RFC-0003): it records
	// a pending question for a human, executing and mutating nothing. Handle it
	// before the privilege routing (an unknown action would otherwise be gated).
	if strings.EqualFold(strings.TrimSpace(action), contract.ActionAskUser) {
		return d.handleAskUser(sess, msg)
	}
	// skill_install is a PRIVILEGED proposal that needs a resolve step the generic
	// passthrough cannot do: the sandbox names skill@version, but the gateway must see
	// the resolved, signature-verified ChangePermissions bundle the human approves. Handle
	// it before the generic privilege routing so the named skill is resolved through the
	// curated trust gate rather than forwarded as an opaque, sandbox-authored payload.
	if strings.EqualFold(strings.TrimSpace(action), string(contract.ChangeSkillInstall)) {
		return d.handleSkillInstall(sess, msg)
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
	//
	// After carries the STRUCTURED proposed config (the capability-change envelope's
	// "payload") so the gateway's verifiers can inspect it and the human approver
	// sees the real diff — not an opaque, double-encoded blob.
	after := extractAfter(msg.Content)
	req := contract.ChangeRequest{
		Kind:         kind,
		AgentGroupID: sess.AgentGroupID,
		RequestedBy:  contract.UserID("sandbox:" + string(sess.ID)),
		After:        after,
	}
	go func() { _, _ = d.gw.Submit(context.Background(), req) }()
	return nil
}

// handleScheduleTask enqueues a future inbound prompt for the session. It NEVER
// executes anything: it validates the request via scheduling.Validate and writes a
// single MessageIn with ProcessAfter=RunAt (and Recurrence carried so the sweep
// can re-enqueue it). The sweep wakes the session when the message comes due. The
// wire shape (prompt + timing, no script field) is pinned as contract.ScheduleRequest.
func (d *Delivery) handleScheduleTask(sess registry.Session, msg contract.MessageOut) error {
	if d.newWriter == nil {
		return fmt.Errorf("host/delivery: schedule_task refused for session %s (no inbound-writer wired)", sess.ID)
	}
	p, err := contract.ParseScheduleRequest(msg.Content)
	if err != nil {
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
		Status:       contract.StatusScheduled,
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

// handleAskUser records a sandbox's ask_user_question (RFC-0003) as a pending
// question for a human. It NEVER executes anything and changes no capability: it
// parses the contract.AskUserRequest and stores it in the pending-question store
// for an operator surface to answer later. When no store is wired the question is
// recognized but only logged (best-effort), never gated as a privileged change.
func (d *Delivery) handleAskUser(sess registry.Session, msg contract.MessageOut) error {
	req, err := contract.ParseAskUserRequest(msg.Content)
	if err != nil {
		return fmt.Errorf("host/delivery: ask_user_question body for %s is not valid JSON: %w", sess.ID, err)
	}
	if strings.TrimSpace(req.Question) == "" {
		return fmt.Errorf("host/delivery: ask_user_question for %s has an empty question", sess.ID)
	}
	if d.questions == nil {
		log.Printf("host/delivery: ask_user_question from %s received but no question store wired; dropping", sess.ID)
		return nil
	}
	d.questions.Record(sess.ID, sess.AgentGroupID, req)
	return nil
}

// skillInstallProposal is the payload a sandbox emits with a skill_install system
// action: it NAMES a curated skill, nothing more. There is deliberately no persona /
// tools / egress / asset / url field — the sandbox can never author skill content; it
// can only point the host at a name the operator has curated and signed.
type skillInstallProposal struct {
	Skill   string `json:"skill"`
	Version string `json:"version"`
}

// handleSkillInstall turns a sandbox skill_install PROPOSAL into a gateway
// ChangeRequest by resolving the named skill through the curated, signature-verifying
// resolver (RFC-0006) — the SAME trust gate the operator `ironctl skill add` path uses.
// It executes nothing and authors no capability: the resolver returns a ChangePermissions
// bundle (the resolved persona/tools/egress/mount) which Submit routes to the gateway's
// mandatory human-approval floor, after which the existing skill-install applier mounts
// it and respawn makes it take effect in the same session.
//
// Fail-closed: with no gateway or no resolver wired (skills not enabled), or a skill that
// is unknown/unsigned/out-of-policy, the proposal is REFUSED here and never reaches the
// gateway — a sandbox can ask, but cannot conjure a skill.
func (d *Delivery) handleSkillInstall(sess registry.Session, msg contract.MessageOut) error {
	if d.gw == nil {
		return fmt.Errorf("host/delivery: skill_install refused for session %s (no gateway)", sess.ID)
	}
	if d.skillProp == nil {
		return fmt.Errorf("host/delivery: skill_install refused for session %s (skills not enabled on this control-plane)", sess.ID)
	}
	a := contract.ParseSystemAction(msg.Content)
	var p skillInstallProposal
	if len(a.Payload) == 0 || json.Unmarshal(a.Payload, &p) != nil {
		return fmt.Errorf("host/delivery: skill_install payload for %s must be {\"skill\":...,\"version\":...}", sess.ID)
	}
	if strings.TrimSpace(p.Skill) == "" || strings.TrimSpace(p.Version) == "" {
		return fmt.Errorf("host/delivery: skill_install for %s requires both skill and version", sess.ID)
	}
	// Resolve = fetch + minisign-verify + manifest-validate. RequestedBy records the
	// sandbox origin so the human approver sees the proposal came from the agent, not an
	// operator. A resolve failure (unknown/unsigned/out-of-policy) refuses the proposal.
	req, err := d.skillProp(p.Skill, p.Version, sess.AgentGroupID, contract.UserID("sandbox:"+string(sess.ID)))
	if err != nil {
		return fmt.Errorf("host/delivery: skill_install proposal for %s rejected at resolve: %w", sess.ID, err)
	}
	// Route the resolved bundle through the gateway (Submit blocks on a human; do it in
	// the background like every other privileged action). The install is NOT applied here.
	go func() { _, _ = d.gw.Submit(context.Background(), req) }()
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
	if d.deliveries != nil {
		d.deliveries.Inc()
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
	case "create_agent":
		// Provisioning a NEW agent group (RFC-0004): a new trust principal, always
		// privileged → gateway (the CreateAgentVerifier holds it for a human).
		return contract.ChangeCreateAgent, true
	case "mcp_access", "grant_mcp", "set_mcp_access":
		// Granting access to a host-configured MCP server + named tools (RFC-0005):
		// widens the agent's tool surface with externally-served tools, always
		// privileged → gateway (a human approves the named server and tools).
		return contract.ChangeMCPAccess, true
	case "mcp_register", "add_mcp_server", "register_mcp":
		// Proposing a brand-new MCP server endpoint from chat (RFC-0007): introduces a
		// new code-execution/egress surface (a host subprocess or a remote endpoint the
		// host dials), always privileged → gateway. The MCPRegisterVerifier holds it for a
		// human who approves the EXACT command/url before it lands in the catalog; an
		// approved register grants the proposing agent nothing (it must still mcp_access).
		return contract.ChangeMCPRegister, true
	case "skill_install":
		// Proposing a curated, signed skill install from chat (RFC-0006). Delivery
		// special-cases this BEFORE this switch (handleSkillInstall) because it needs a
		// resolve step — the sandbox names skill@version and the host resolves it through
		// the curated trust gate into the ChangePermissions bundle the human approves.
		// Listed here so the authorization map is complete and stays privileged (gated,
		// never executed inline) even if the special-case is ever removed.
		return contract.ChangePermissions, true
	case "script", "exec", "run", "shell":
		// An unapproved script/RCE path: there is NO direct execution. Map it to the
		// most privileged change kind so it is always human-gated, never run inline.
		return contract.ChangePermissions, true
	case contract.ActionScheduleTask, "schedule":
		// Scheduling carries ONLY a prompt and enqueues a future inbound message —
		// there is no script/command field and nothing is executed here, so it is a
		// non-privileged host action. (Delivery special-cases it before reaching this
		// switch; it is listed here so the authorization map is complete and any
		// privileged future action that prompt requests still passes through the
		// gateway.)
		return "", false
	case contract.ActionAskUser:
		// A question for a human (RFC-0003): records a pending question, executes
		// nothing and changes no capability, so it is non-privileged. Delivery
		// special-cases it before this switch; listed here so the map is complete and
		// it is never mistaken for an unknown (privileged) action.
		return "", false
	case "typing", "presence", "ack", "noop", "":
		// Informational, non-privileged: safe to deliver directly.
		return "", false
	default:
		// Unknown action: refuse to execute, treat as privileged so it is gated.
		return contract.ChangePermissions, true
	}
}

// extractAfter returns the structured proposed config to record as a
// ChangeRequest.After. The capability-change envelope (contract.SystemAction)
// carries the proposed config in its Payload: when present it is returned verbatim
// so the gateway verifiers and the approver see the real config. If the body is a
// JSON object without a payload, the whole object is used; a non-JSON body is
// encoded as a JSON string so After is always valid JSON.
func extractAfter(content string) json.RawMessage {
	if a := contract.ParseSystemAction(content); len(a.Payload) > 0 {
		return a.Payload
	}
	c := strings.TrimSpace(content)
	if strings.HasPrefix(c, "{") && json.Valid([]byte(c)) {
		return json.RawMessage(c)
	}
	b, _ := json.Marshal(content)
	return b
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
