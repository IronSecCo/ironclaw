// Package loop is the sandbox reasoning poll loop: read pending, format the
// prompt, call the provider, parse the model's structured output into outbound
// writes, mark processing/completed, and heartbeat (touch /workspace/.heartbeat).
// It ports the reference poll-loop semantics (trigger=0 accumulate, follow-up
// polling during streaming, slash-command handling).
//
// CONTRACT: read-only import of github.com/IronSecCo/ironclaw/internal/contract.
package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/sandbox/provider"
	"github.com/IronSecCo/ironclaw/internal/sandbox/queue"
	"github.com/IronSecCo/ironclaw/internal/sandbox/tools"
)

// Defaults for optional Config fields.
const (
	defaultPollInterval  = 2 * time.Second
	defaultHeartbeatPath = "/workspace/.heartbeat"
)

// DefaultSystemPrompt frames the agent's role and the security boundary in-band,
// as defense in depth alongside the structural guarantees (no self-edit/install
// tools exist; capability changes only flow through the host gateway). cmd/sandbox
// wires it into the provider so every turn carries it.
const DefaultSystemPrompt = `You are IronClaw, a security-isolated assistant running inside a sandbox.

Operating constraints (these are enforced by the platform; do not claim to bypass them):
- You have no direct network access. Model calls go through a host proxy, and any web or external-API access is only via specific approved tools (for example web_search or http_fetch) that reach a host-mediated, audited broker — and only when the operator has enabled them. Check your actual available tools before assuming you cannot reach the network: if web_search is present, use it to look things up.
- You CANNOT install packages, add integrations or MCP servers, change your persona, edit your enabled tools, change permissions, or alter mounts on your own. To request any such change, call the request_capability_change tool — it submits the request to a control-plane gateway that requires human approval. Never claim to have applied such a change yourself.
- You are meant to be capable. When the user asks for something you cannot currently do, do NOT just decline — request the capability: use request_api_access to reach a new external API/website, or request_capability_change to enable a built-in tool you don't have (e.g. {"add":["web_search"]}). Tell the user you've submitted it for human approval and continue with whatever you can do meanwhile. Once a human approves, the capability becomes available to you (on your next message) and you should then use it. The human-approval step is the ONLY thing standing between you and the new capability — so always offer to request it rather than treating it as impossible.
- File operations are limited to your workspace directory.

Be concise and helpful. Use the available tools when they help; otherwise answer directly.`

// Config wires the loop's dependencies. Inbound, Outbound, and Provider are
// required; the rest take defaults.
type Config struct {
	Inbound  contract.InboundReader
	Outbound contract.OutboundWriter
	Provider provider.Provider
	Tools    *tools.Registry

	// HeartbeatPath is touched every poll so the host sweep can detect a stale
	// sandbox. Defaults to /workspace/.heartbeat.
	HeartbeatPath string
	// PollInterval is the gap between polls. Defaults to 2s.
	PollInterval time.Duration
	// Clock returns the current time; injectable for tests. Defaults to time.Now.
	Clock func() time.Time
	// Logger receives non-fatal poll diagnostics. Defaults to log.Default().
	Logger *log.Logger

	// ProviderBackoffMax caps the exponential backoff applied after a model
	// provider error. Defaults to 60s.
	ProviderBackoffMax time.Duration
	// ProviderBreakerThreshold is the number of consecutive provider failures
	// that trips the circuit breaker open. Defaults to 5.
	ProviderBreakerThreshold int
	// Jitter returns a fraction in [0,1) used to de-synchronise provider
	// retries. Injectable for deterministic tests; defaults to math/rand.
	Jitter func() float64
}

// Loop is the sandbox reasoning poll loop.
type Loop struct {
	cfg Config

	// breaker paces retries after model provider errors (exponential backoff +
	// circuit breaker) so a down model API is not polled at the fixed interval.
	breaker *backoff

	// buffer accumulates trigger=0 messages that have not yet engaged the model.
	buffer      []contract.MessageIn
	bufferedIDs map[contract.MessageID]struct{}
	// doneIDs dedups messages already engaged, across the lag between the
	// sandbox marking completion and the host advancing inbound status.
	doneIDs map[contract.MessageID]struct{}

	// outCounter disambiguates outbound message ids written within the same clock
	// tick (the clock is injectable and may be fixed in tests).
	outCounter uint64

	// hbMu serializes heartbeat file writes (the steady poll and the
	// during-streaming keepalive ticker can both write).
	hbMu sync.Mutex

	bindingPendingLogged bool
}

// maxToolIterations bounds the agentic tool-use loop so a misbehaving model
// cannot spin forever.
const maxToolIterations = 8

// Durable session-state keys. The loop persists its in-memory poll state
// under these keys via contract.OutboundWriter.PutSessionState so a sandbox
// restart resumes mid-accumulation instead of dropping work. They share the
// session_state table with other keys (e.g. "reset_at") and are namespaced to
// avoid collision.
const (
	stateKeyBuffer  = "loop.buffer"   // JSON array of contract.MessageIn awaiting engagement
	stateKeyDoneIDs = "loop.done_ids" // JSON array of contract.MessageID already engaged
)

// sessionStateLoader reads back durable per-session state previously written via
// contract.OutboundWriter.PutSessionState. The sandbox outbound queue satisfies
// it; it is kept loop-local (not added to the frozen OutboundWriter interface)
// so the contract stays write-only by design. When the configured Outbound
// implements it, New restores the loop's buffered + deduped message state.
type sessionStateLoader interface {
	LoadSessionState() (map[string]string, error)
}

// New constructs a Loop, validating required dependencies and applying defaults.
func New(cfg Config) (*Loop, error) {
	if cfg.Inbound == nil {
		return nil, errors.New("sandbox/loop: Inbound is required")
	}
	if cfg.Outbound == nil {
		return nil, errors.New("sandbox/loop: Outbound is required")
	}
	if cfg.Provider == nil {
		return nil, errors.New("sandbox/loop: Provider is required")
	}
	if cfg.Tools == nil {
		cfg.Tools = tools.NewRegistry()
	}
	if cfg.HeartbeatPath == "" {
		cfg.HeartbeatPath = defaultHeartbeatPath
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultPollInterval
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	if cfg.ProviderBackoffMax <= 0 {
		cfg.ProviderBackoffMax = defaultProviderBackoffMax
	}
	if cfg.ProviderBreakerThreshold <= 0 {
		cfg.ProviderBreakerThreshold = defaultBreakerThreshold
	}
	l := &Loop{
		cfg:         cfg,
		breaker:     newBackoff(cfg.PollInterval, cfg.ProviderBackoffMax, cfg.ProviderBreakerThreshold, cfg.Jitter),
		bufferedIDs: make(map[contract.MessageID]struct{}),
		doneIDs:     make(map[contract.MessageID]struct{}),
	}
	// Restore durable poll state so a respawn resumes mid-accumulation.
	l.restoreState()
	return l, nil
}

// restoreState rehydrates the poll loop's buffer and dedup set from durable
// session state, so a sandbox restart resumes accumulated trigger=0 messages and
// does not re-engage messages already completed before the restart. It is
// best-effort: if the Outbound does not persist state, the crypto binding is
// pending, or a value is unreadable, the loop simply starts with empty state —
// the previous behavior. Buffered ids are rederived from the restored buffer.
func (l *Loop) restoreState() {
	loader, ok := l.cfg.Outbound.(sessionStateLoader)
	if !ok {
		return
	}
	state, err := loader.LoadSessionState()
	if err != nil {
		l.cfg.Logger.Printf("sandbox/loop: restore session state: %v", err)
		return
	}
	if raw := state[stateKeyBuffer]; raw != "" {
		var buf []contract.MessageIn
		if err := json.Unmarshal([]byte(raw), &buf); err != nil {
			l.cfg.Logger.Printf("sandbox/loop: restore buffer: %v", err)
		} else {
			l.buffer = buf
			for _, m := range buf {
				l.bufferedIDs[m.ID] = struct{}{}
			}
		}
	}
	if raw := state[stateKeyDoneIDs]; raw != "" {
		var ids []contract.MessageID
		if err := json.Unmarshal([]byte(raw), &ids); err != nil {
			l.cfg.Logger.Printf("sandbox/loop: restore done ids: %v", err)
		} else {
			for _, id := range ids {
				l.doneIDs[id] = struct{}{}
			}
		}
	}
}

// persistState snapshots the loop's buffer and dedup set to durable session
// state. It is best-effort: a write failure is logged, not fatal —
// losing the snapshot only reverts to the previous behavior of dropping
// in-flight state on an unclean exit. Called after the buffer grows (new
// messages accumulated) and after an engage clears it / extends the done set.
func (l *Loop) persistState() {
	buf, err := json.Marshal(l.buffer)
	if err != nil {
		l.cfg.Logger.Printf("sandbox/loop: marshal buffer: %v", err)
		return
	}
	if err := l.cfg.Outbound.PutSessionState(stateKeyBuffer, string(buf)); err != nil {
		l.cfg.Logger.Printf("sandbox/loop: persist buffer: %v", err)
	}

	ids := make([]contract.MessageID, 0, len(l.doneIDs))
	for id := range l.doneIDs {
		ids = append(ids, id)
	}
	done, err := json.Marshal(ids)
	if err != nil {
		l.cfg.Logger.Printf("sandbox/loop: marshal done ids: %v", err)
		return
	}
	if err := l.cfg.Outbound.PutSessionState(stateKeyDoneIDs, string(done)); err != nil {
		l.cfg.Logger.Printf("sandbox/loop: persist done ids: %v", err)
	}
}

// Run drives the poll loop until ctx is cancelled. A corruption streak on the
// inbound queue is fatal and returned so the caller can exit and let the host
// respawn the sandbox; other poll errors are logged and retried next interval.
func (l *Loop) Run(ctx context.Context) error {
	firstPoll := true
	for {
		err := l.poll(ctx, firstPoll)
		wait := l.cfg.PollInterval
		switch {
		case err == nil:
			// A clean poll closes the circuit and clears any failure streak.
			l.breaker.reset()
		case errors.Is(err, queue.ErrCorruptionStreak):
			return err // fatal: host must respawn with a fresh mount
		case errors.Is(err, contract.ErrCryptoBindingPending):
			if !l.bindingPendingLogged {
				l.cfg.Logger.Printf("sandbox/loop: encrypted queue binding pending; idling until it is wired (see docs/building.md)")
				l.bindingPendingLogged = true
			}
		case errors.Is(err, ErrProvider):
			// Back off exponentially with jitter instead of retrying at the fixed
			// poll interval, and trip the breaker after a sustained outage.
			wait = l.breaker.fail()
			if l.breaker.tripped() && !l.breaker.logged {
				l.cfg.Logger.Printf("sandbox/loop: model provider unavailable after %d consecutive failures; circuit open, backing off up to %s",
					l.breaker.consecutiveFailures(), l.cfg.ProviderBackoffMax)
				l.breaker.logged = true
			}
			l.cfg.Logger.Printf("sandbox/loop: provider error (attempt %d): %v; retrying in %s",
				l.breaker.consecutiveFailures(), err, wait.Round(time.Millisecond))
		default:
			l.cfg.Logger.Printf("sandbox/loop: poll error: %v", err)
		}
		firstPoll = false

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// poll performs one poll cycle: heartbeat, read pending, accumulate, and engage
// when a trigger or slash command is present (or on cold start).
func (l *Loop) poll(ctx context.Context, firstPoll bool) error {
	l.heartbeat()

	pending, err := l.cfg.Inbound.PendingMessages(firstPoll)
	if err != nil {
		return err
	}

	// Prune doneIDs the host has advanced past so the dedup set stays bounded.
	pendingSet := make(map[contract.MessageID]struct{}, len(pending))
	for _, m := range pending {
		pendingSet[m.ID] = struct{}{}
	}
	for id := range l.doneIDs {
		if _, still := pendingSet[id]; !still {
			delete(l.doneIDs, id)
		}
	}

	// Buffer freshly-seen messages (not already done or buffered).
	added := false
	for _, m := range pending {
		if _, done := l.doneIDs[m.ID]; done {
			continue
		}
		if _, buffered := l.bufferedIDs[m.ID]; buffered {
			continue
		}
		l.buffer = append(l.buffer, m)
		l.bufferedIDs[m.ID] = struct{}{}
		added = true
	}
	// Persist the grown buffer so an unclean exit before engagement does not drop
	// accumulated trigger=0 messages. Best-effort; engage persists again.
	if added {
		l.persistState()
	}

	if !l.shouldEngage(firstPoll) {
		return nil // keep accumulating
	}
	return l.engage(ctx)
}

// shouldEngage reports whether the buffered messages warrant a model turn: any
// triggering message (trigger != 0), any slash command, a due scheduled message
// (one with a process_after — the queue only returns it once due, and it carries
// trigger=0), or a cold-start backlog.
func (l *Loop) shouldEngage(firstPoll bool) bool {
	if len(l.buffer) == 0 {
		return false
	}
	if firstPoll {
		return true
	}
	for _, m := range l.buffer {
		if m.Trigger != 0 || isSlashCommand(m.Content) || m.ProcessAfter != nil {
			return true
		}
	}
	return false
}

// engage processes the buffered messages: slash commands are handled locally,
// remaining chat messages go to the model as one turn, all are acked, and the
// buffer is cleared.
func (l *Loop) engage(ctx context.Context) error {
	working := l.buffer
	ids := make([]contract.MessageID, len(working))
	for i, m := range working {
		ids[i] = m.ID
	}

	if err := l.cfg.Outbound.MarkProcessing(ids); err != nil {
		return fmt.Errorf("mark processing: %w", err)
	}

	routing := l.routing()

	var chat []contract.MessageIn
	skipChat := false
	for _, m := range working {
		if isSlashCommand(m.Content) {
			reply, isReset := l.handleSlash(m)
			if isReset {
				skipChat = true
			}
			if reply != "" {
				if err := l.writeReply(reply, m.ID, routing); err != nil {
					return err
				}
			}
			continue
		}
		chat = append(chat, m)
	}

	if len(chat) > 0 && !skipChat {
		prompt := formatPrompt(chat)
		// Keep the heartbeat fresh while the (possibly long, streamed) model call
		// runs so the host sweep does not treat the sandbox as stale mid-turn.
		stopHB := l.startHeartbeat()
		resp, capEnvelopes, err := l.respond(ctx, prompt)
		stopHB()
		if err != nil {
			return fmt.Errorf("provider respond: %w", providerErr(err))
		}
		if strings.TrimSpace(resp) != "" {
			if err := l.writeReply(resp, chat[len(chat)-1].ID, routing); err != nil {
				return err
			}
		}
		// Forward any capability-change requests the agent made to the host
		// gateway as system messages — the sandbox never applies them itself.
		for _, env := range capEnvelopes {
			if err := l.writeSystem(env, routing); err != nil {
				return err
			}
		}
	}

	if err := l.cfg.Outbound.MarkCompleted(ids); err != nil {
		return fmt.Errorf("mark completed: %w", err)
	}

	for _, id := range ids {
		delete(l.bufferedIDs, id)
		l.doneIDs[id] = struct{}{}
	}
	l.buffer = nil
	// Persist the cleared buffer + extended done set so a restart neither replays
	// these messages nor loses the dedup record of them.
	l.persistState()
	return nil
}

// writeReply writes one outbound chat message in reply to inReplyTo, routed by
// the session's platform coordinates when known.
func (l *Loop) writeReply(content string, inReplyTo contract.MessageID, routing contract.SessionRouting) error {
	id := inReplyTo
	out := contract.MessageOut{
		ID:        contract.MessageID(l.newOutboundID()),
		InReplyTo: &id,
		Timestamp: l.cfg.Clock().UTC(),
		Kind:      contract.KindChat,
		Content:   content,
	}
	if routing.ChannelType != "" {
		ct := routing.ChannelType
		out.ChannelType = &ct
	}
	if routing.PlatformID != "" {
		pid := routing.PlatformID
		out.PlatformID = &pid
	}
	out.ThreadID = routing.ThreadID

	if err := l.cfg.Outbound.WriteMessageOut(out); err != nil {
		return fmt.Errorf("write outbound: %w", err)
	}
	return nil
}

// routing fetches the session routing, treating any error (including a pending
// binding) as "unknown" — the message still gets written, just without platform
// coordinates.
func (l *Loop) routing() contract.SessionRouting {
	sr, err := l.cfg.Inbound.SessionRouting()
	if err != nil {
		return contract.SessionRouting{}
	}
	return sr
}

// respond produces the model's reply to prompt. When the provider supports tool
// use and tools are registered, it runs the agentic tool loop; otherwise it does
// a single Query. It also returns any capability-change envelopes emitted by the
// request_capability_change tool, for forwarding to the host gateway.
func (l *Loop) respond(ctx context.Context, prompt string) (string, []string, error) {
	converser, ok := l.cfg.Provider.(provider.ToolConverser)
	if !ok || len(l.cfg.Tools.Names()) == 0 {
		text, err := l.cfg.Provider.Query(ctx, prompt)
		return text, nil, err
	}
	return l.runAgent(ctx, converser, prompt)
}

// runAgent drives the Messages API tool-use loop: send the prompt with the tool
// specs, execute any tool calls the model requests, feed the results back, and
// repeat until the model stops calling tools. Capability-change envelopes are
// collected for the host gateway.
func (l *Loop) runAgent(ctx context.Context, converser provider.ToolConverser, prompt string) (string, []string, error) {
	history := []provider.Message{provider.UserTextMessage(prompt)}
	specs := l.toolSpecs()
	var capEnvelopes []string

	for i := 0; i < maxToolIterations; i++ {
		turn, err := converser.Converse(ctx, history, specs)
		if err != nil {
			return "", nil, err
		}
		if len(turn.ToolCalls) == 0 {
			return turn.Text, capEnvelopes, nil
		}

		history = append(history, turn.Assistant)
		results := make([]provider.ToolResult, 0, len(turn.ToolCalls))
		for _, call := range turn.ToolCalls {
			out, isErr := l.invokeTool(ctx, call)
			results = append(results, provider.ToolResult{ToolUseID: call.ID, Content: out, IsError: isErr})
			// A tool that forwards to the host (capability change, scheduling)
			// renders its successful output into a system-action wire body; the loop
			// writes it as a KindSystem outbound message for the host to
			// re-authorize. A tool that emits chat (send_message, send_file) renders
			// its output into one or more KindChat outbound messages, written here so
			// the host delivery can enforce destination permission. The sandbox never
			// acts on either directly.
			if !isErr {
				if fwd, ok := l.cfg.Tools.Get(call.Name); ok {
					if hf, ok := fwd.(tools.HostForwarder); ok {
						if body, ferr := hf.ToHostAction(out); ferr == nil && body != "" {
							capEnvelopes = append(capEnvelopes, body)
						}
					}
					if em, ok := fwd.(tools.OutboundEmitter); ok {
						if msgs, ferr := em.ToOutbound(out); ferr == nil {
							for _, msg := range msgs {
								l.writeEmitted(msg)
							}
						} else {
							l.cfg.Logger.Printf("sandbox/loop: emit outbound from %s: %v", call.Name, ferr)
						}
					}
				}
			}
		}
		history = append(history, provider.ToolResultsMessage(results))
	}
	return "", capEnvelopes, fmt.Errorf("tool loop exceeded %d iterations", maxToolIterations)
}

// invokeTool runs one tool call, returning its output and whether it errored.
// Errors are returned as tool-result content (is_error) so the model can adapt
// rather than aborting the turn.
func (l *Loop) invokeTool(ctx context.Context, call provider.ToolCall) (string, bool) {
	tool, ok := l.cfg.Tools.Get(call.Name)
	if !ok {
		return fmt.Sprintf("unknown tool: %s", call.Name), true
	}
	out, err := tool.Invoke(ctx, call.Input)
	if err != nil {
		return err.Error(), true
	}
	return out, false
}

// toolSpecs renders the registered tools as provider tool specs.
func (l *Loop) toolSpecs() []provider.ToolSpec {
	registered := l.cfg.Tools.List()
	specs := make([]provider.ToolSpec, len(registered))
	for i, t := range registered {
		specs[i] = provider.ToolSpec{Name: t.Name(), Description: t.Description(), InputSchema: t.JSONSchema()}
	}
	return specs
}

// writeSystem writes an outbound system message — used to forward a capability
// change request to the host gateway, which re-authorizes system actions and
// routes the change through the mandatory verifier+approval chain.
func (l *Loop) writeSystem(content string, routing contract.SessionRouting) error {
	out := contract.MessageOut{
		ID:        contract.MessageID(l.newOutboundID()),
		Timestamp: l.cfg.Clock().UTC(),
		Kind:      contract.KindSystem,
		Content:   content,
	}
	if routing.ChannelType != "" {
		ct := routing.ChannelType
		out.ChannelType = &ct
	}
	if routing.PlatformID != "" {
		pid := routing.PlatformID
		out.PlatformID = &pid
	}
	out.ThreadID = routing.ThreadID

	if err := l.cfg.Outbound.WriteMessageOut(out); err != nil {
		return fmt.Errorf("write system outbound: %w", err)
	}
	return nil
}

// writeEmitted writes a chat message a tool produced (send_message / send_file).
// The tool resolves the destination coordinates; the loop assigns the message its
// ID and timestamp and lets the queue assign the seq. A write failure is logged,
// not fatal — one failed send must not abort the whole turn.
func (l *Loop) writeEmitted(msg contract.MessageOut) {
	msg.ID = contract.MessageID(l.newOutboundID())
	msg.Timestamp = l.cfg.Clock().UTC()
	if msg.Kind == "" {
		msg.Kind = contract.KindChat
	}
	if err := l.cfg.Outbound.WriteMessageOut(msg); err != nil {
		l.cfg.Logger.Printf("sandbox/loop: write emitted outbound: %v", err)
	}
}

// handleSlash processes a built-in slash command and returns the reply text plus
// whether it was a reset (which suppresses the chat turn for this engage).
func (l *Loop) handleSlash(m contract.MessageIn) (reply string, isReset bool) {
	fields := strings.Fields(strings.TrimSpace(m.Content))
	if len(fields) == 0 {
		return "", false
	}
	cmd := strings.ToLower(fields[0])
	switch cmd {
	case "/help":
		names := l.cfg.Tools.Names()
		if len(names) == 0 {
			return "Commands: /help, /status, /reset. No tools are enabled.", false
		}
		return "Commands: /help, /status, /reset. Tools: " + strings.Join(names, ", "), false
	case "/status":
		return fmt.Sprintf("sandbox online; %d tool(s) enabled; %d message(s) buffered.",
			len(l.cfg.Tools.Names()), len(l.buffer)), false
	case "/reset":
		_ = l.cfg.Outbound.PutSessionState("reset_at", l.cfg.Clock().UTC().Format(time.RFC3339Nano))
		return "Context reset.", true
	default:
		return fmt.Sprintf("Unknown command: %s. Try /help.", cmd), false
	}
}

// heartbeat writes the current time to the heartbeat file. A failure is logged
// but non-fatal — if heartbeats stop, the host sweep respawns the sandbox.
func (l *Loop) heartbeat() {
	ts := l.cfg.Clock().UTC().Format(time.RFC3339Nano)
	l.hbMu.Lock()
	defer l.hbMu.Unlock()
	if err := os.WriteFile(l.cfg.HeartbeatPath, []byte(ts), 0o644); err != nil {
		l.cfg.Logger.Printf("sandbox/loop: heartbeat write failed: %v", err)
	}
}

// startHeartbeat runs a background ticker that refreshes the heartbeat every poll
// interval until the returned stop function is called (and the ticker drains).
// It is used to keep the sandbox alive across a long streamed model turn — the
// keepalive aspect of follow-up polling during streaming.
func (l *Loop) startHeartbeat() (stop func()) {
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		t := time.NewTicker(l.cfg.PollInterval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				l.heartbeat()
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

// newOutboundID derives a unique outbound message id. A per-loop counter
// disambiguates ids written within the same clock tick (the clock is injectable
// and may be fixed in tests). It is only used for the message PRIMARY KEY;
// ordering is by seq, which the queue assigns.
func (l *Loop) newOutboundID() string {
	l.outCounter++
	return fmt.Sprintf("out-%d-%d", l.cfg.Clock().UnixNano(), l.outCounter)
}

// formatPrompt renders the buffered chat messages into a single prompt in seq
// order. Platform messages get a light source-attribution header so the model
// can tell who/where a multi-message turn came from; plain messages with no
// platform context stay bare.
func formatPrompt(msgs []contract.MessageIn) string {
	parts := make([]string, 0, len(msgs))
	for _, m := range msgs {
		content := strings.TrimSpace(m.Content)
		if hdr := messageHeader(m); hdr != "" {
			content = hdr + "\n" + content
		}
		parts = append(parts, content)
	}
	return strings.Join(parts, "\n\n")
}

// messageHeader builds a one-line source attribution for a platform message, or
// "" when there is no platform context (a plain chat turn stays unannotated).
func messageHeader(m contract.MessageIn) string {
	if m.ChannelType == nil && m.PlatformID == nil {
		return ""
	}
	kind := m.Kind
	if kind == "" {
		kind = contract.KindChat
	}
	parts := []string{string(kind)}
	if m.ChannelType != nil {
		parts = append(parts, "via "+*m.ChannelType)
	}
	if m.PlatformID != nil {
		parts = append(parts, *m.PlatformID)
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// isSlashCommand reports whether content is a slash command.
func isSlashCommand(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), "/")
}
