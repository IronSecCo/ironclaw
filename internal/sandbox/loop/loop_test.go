package loop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/sandbox/provider"
	"github.com/nivardsec/ironclaw/internal/sandbox/tools"
)

// fakeInbound is an in-memory contract.InboundReader for loop tests.
type fakeInbound struct {
	pending []contract.MessageIn
	routing contract.SessionRouting
}

func (f *fakeInbound) PendingMessages(bool) ([]contract.MessageIn, error) { return f.pending, nil }
func (f *fakeInbound) Destinations() ([]contract.Destination, error)      { return nil, nil }
func (f *fakeInbound) SessionRouting() (contract.SessionRouting, error)   { return f.routing, nil }
func (f *fakeInbound) Close() error                                       { return nil }

// fakeOutbound records every write for assertions.
type fakeOutbound struct {
	writes     []contract.MessageOut
	processing [][]contract.MessageID
	completed  [][]contract.MessageID
	state      map[string]string
}

func (f *fakeOutbound) WriteMessageOut(m contract.MessageOut) error {
	f.writes = append(f.writes, m)
	return nil
}
func (f *fakeOutbound) MarkProcessing(ids []contract.MessageID) error {
	f.processing = append(f.processing, ids)
	return nil
}
func (f *fakeOutbound) MarkCompleted(ids []contract.MessageID) error {
	f.completed = append(f.completed, ids)
	return nil
}
func (f *fakeOutbound) PutSessionState(k, v string) error {
	if f.state == nil {
		f.state = map[string]string{}
	}
	f.state[k] = v
	return nil
}

// LoadSessionState makes fakeOutbound satisfy the loop's sessionStateLoader, so
// New restores from it — the same object that persisted the state reads it back,
// mirroring the real outbound queue's single-handle behavior.
func (f *fakeOutbound) LoadSessionState() (map[string]string, error) {
	out := map[string]string{}
	for k, v := range f.state {
		out[k] = v
	}
	return out, nil
}
func (f *fakeOutbound) Close() error { return nil }

// fakeProvider records prompts and returns a canned reply.
type fakeProvider struct {
	reply      string
	calls      int
	lastPrompt string
}

func (f *fakeProvider) Query(_ context.Context, prompt string) (string, error) {
	f.calls++
	f.lastPrompt = prompt
	return f.reply, nil
}

func newTestLoop(t *testing.T, in *fakeInbound, out *fakeOutbound, prov *fakeProvider) *Loop {
	t.Helper()
	l, err := New(Config{
		Inbound:       in,
		Outbound:      out,
		Provider:      prov,
		HeartbeatPath: filepath.Join(t.TempDir(), "heartbeat"),
		Clock:         func() time.Time { return time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC) },
		Logger:        log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return l
}

func msg(id, content string, trigger int) contract.MessageIn {
	return contract.MessageIn{ID: contract.MessageID(id), Kind: contract.KindChat, Content: content, Trigger: trigger}
}

// TestAccumulateThenEngage asserts that a trigger=0 message accumulates silently
// and a later trigger!=0 message engages the model on the whole buffer.
func TestAccumulateThenEngage(t *testing.T) {
	in := &fakeInbound{routing: contract.SessionRouting{ChannelType: "slack", PlatformID: "C123"}}
	out := &fakeOutbound{}
	prov := &fakeProvider{reply: "the answer"}
	l := newTestLoop(t, in, out, prov)
	ctx := context.Background()

	// Poll 1: a single accumulate (trigger=0) message — must not engage.
	in.pending = []contract.MessageIn{msg("m1", "first", 0)}
	if err := l.poll(ctx, false); err != nil {
		t.Fatalf("poll 1: %v", err)
	}
	if prov.calls != 0 || len(out.writes) != 0 {
		t.Fatalf("poll 1 engaged early: calls=%d writes=%d", prov.calls, len(out.writes))
	}

	// Poll 2: a triggering message arrives — engage on [m1, m2].
	in.pending = []contract.MessageIn{msg("m1", "first", 0), msg("m2", "second", 1)}
	if err := l.poll(ctx, false); err != nil {
		t.Fatalf("poll 2: %v", err)
	}
	if prov.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", prov.calls)
	}
	if prov.lastPrompt != "first\n\nsecond" {
		t.Fatalf("prompt = %q, want %q", prov.lastPrompt, "first\n\nsecond")
	}
	if len(out.writes) != 1 {
		t.Fatalf("outbound writes = %d, want 1", len(out.writes))
	}
	w := out.writes[0]
	if w.Content != "the answer" {
		t.Fatalf("outbound content = %q, want %q", w.Content, "the answer")
	}
	if w.InReplyTo == nil || *w.InReplyTo != "m2" {
		t.Fatalf("in_reply_to = %v, want m2", w.InReplyTo)
	}
	if w.ChannelType == nil || *w.ChannelType != "slack" || w.PlatformID == nil || *w.PlatformID != "C123" {
		t.Fatalf("routing not applied: %+v", w)
	}
	if len(out.processing) != 1 || len(out.completed) != 1 {
		t.Fatalf("processing/completed not recorded once each: %d/%d", len(out.processing), len(out.completed))
	}
}

// TestDedupAcrossHostLag asserts an already-engaged message that is still pending
// (host has not advanced status yet) is not reprocessed.
func TestDedupAcrossHostLag(t *testing.T) {
	in := &fakeInbound{}
	out := &fakeOutbound{}
	prov := &fakeProvider{reply: "ok"}
	l := newTestLoop(t, in, out, prov)
	ctx := context.Background()

	in.pending = []contract.MessageIn{msg("m1", "hi", 1)}
	if err := l.poll(ctx, false); err != nil {
		t.Fatalf("poll 1: %v", err)
	}
	if prov.calls != 1 {
		t.Fatalf("calls after first poll = %d, want 1", prov.calls)
	}

	// Same message still pending — must not re-engage.
	if err := l.poll(ctx, false); err != nil {
		t.Fatalf("poll 2: %v", err)
	}
	if prov.calls != 1 {
		t.Fatalf("calls after dedup poll = %d, want 1", prov.calls)
	}
	if len(out.writes) != 1 {
		t.Fatalf("outbound writes = %d, want 1", len(out.writes))
	}
}

// TestFirstPollDrainsBacklog asserts a cold start engages even on trigger=0 backlog.
func TestFirstPollDrainsBacklog(t *testing.T) {
	in := &fakeInbound{pending: []contract.MessageIn{msg("m1", "backlog", 0)}}
	out := &fakeOutbound{}
	prov := &fakeProvider{reply: "drained"}
	l := newTestLoop(t, in, out, prov)

	if err := l.poll(context.Background(), true); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if prov.calls != 1 || len(out.writes) != 1 {
		t.Fatalf("cold start did not drain backlog: calls=%d writes=%d", prov.calls, len(out.writes))
	}
}

// TestDueScheduledMessageEngages asserts a due scheduled task (process_after set,
// trigger=0, as the host's schedule_task writes) engages the model. The queue
// only returns it once due, so its presence in the buffer means it should run.
func TestDueScheduledMessageEngages(t *testing.T) {
	past := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	sched := contract.MessageIn{
		ID:           "s1",
		Kind:         contract.KindTask,
		Content:      "run the daily report",
		Trigger:      0,
		ProcessAfter: &past,
	}
	in := &fakeInbound{pending: []contract.MessageIn{sched}}
	out := &fakeOutbound{}
	prov := &fakeProvider{reply: "report done"}
	l := newTestLoop(t, in, out, prov)

	if err := l.poll(context.Background(), false); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if prov.calls != 1 {
		t.Fatalf("due scheduled task did not engage: provider calls=%d, want 1", prov.calls)
	}
	if len(out.writes) != 1 || out.writes[0].Content != "report done" {
		t.Fatalf("outbound writes = %+v, want one reply %q", out.writes, "report done")
	}
}

// TestSlashCommandHandledLocally asserts slash commands reply without a model call.
func TestSlashCommandHandledLocally(t *testing.T) {
	in := &fakeInbound{pending: []contract.MessageIn{msg("m1", "/help", 0)}}
	out := &fakeOutbound{}
	prov := &fakeProvider{reply: "should not be used"}
	l := newTestLoop(t, in, out, prov)

	if err := l.poll(context.Background(), false); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if prov.calls != 0 {
		t.Fatalf("slash command called the model: calls=%d", prov.calls)
	}
	if len(out.writes) != 1 {
		t.Fatalf("slash command writes = %d, want 1", len(out.writes))
	}
	if got := out.writes[0].Content; got == "" || got[0] != 'C' { // "Commands: ..."
		t.Fatalf("slash reply = %q, want a /help listing", got)
	}
}

// TestResetSkipsModelTurn asserts /reset suppresses the chat turn and records state.
func TestResetSkipsModelTurn(t *testing.T) {
	in := &fakeInbound{pending: []contract.MessageIn{
		msg("m1", "please answer", 0),
		msg("m2", "/reset", 0),
	}}
	out := &fakeOutbound{}
	prov := &fakeProvider{reply: "should be skipped"}
	l := newTestLoop(t, in, out, prov)

	if err := l.poll(context.Background(), false); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if prov.calls != 0 {
		t.Fatalf("/reset did not skip the model turn: calls=%d", prov.calls)
	}
	if out.state["reset_at"] == "" {
		t.Fatal("/reset did not record session state")
	}
}

// TestNewRequiresDependencies asserts construction validates required fields.
func TestNewRequiresDependencies(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New with no deps should error")
	}
	if _, err := New(Config{Inbound: &fakeInbound{}}); err == nil {
		t.Fatal("New without Outbound/Provider should error")
	}
}

// TestFormatPrompt asserts plain messages stay bare and platform messages get a
// source-attribution header.
func TestFormatPrompt(t *testing.T) {
	plain := []contract.MessageIn{msg("a", "hello", 0), msg("b", "world", 0)}
	if got := formatPrompt(plain); got != "hello\n\nworld" {
		t.Fatalf("plain prompt = %q, want %q", got, "hello\n\nworld")
	}

	ct, pid := "slack", "C123"
	m := contract.MessageIn{ID: "c", Kind: contract.KindChat, Content: "hi", ChannelType: &ct, PlatformID: &pid}
	got := formatPrompt([]contract.MessageIn{m})
	if !strings.Contains(got, "[chat via slack C123]") || !strings.Contains(got, "hi") {
		t.Fatalf("attributed prompt = %q, want header + content", got)
	}
}

// TestDefaultSystemPrompt asserts the system prompt frames the security boundary.
func TestDefaultSystemPrompt(t *testing.T) {
	for _, want := range []string{"request_capability_change", "sandbox"} {
		if !strings.Contains(DefaultSystemPrompt, want) {
			t.Fatalf("DefaultSystemPrompt missing %q", want)
		}
	}
}

// TestStartHeartbeatFires asserts the during-streaming keepalive refreshes the
// heartbeat file while it is running.
func TestStartHeartbeatFires(t *testing.T) {
	hb := filepath.Join(t.TempDir(), "hb")
	l, err := New(Config{
		Inbound:       &fakeInbound{},
		Outbound:      &fakeOutbound{},
		Provider:      &fakeProvider{},
		HeartbeatPath: hb,
		PollInterval:  5 * time.Millisecond,
		Clock:         func() time.Time { return time.Unix(0, 0).UTC() },
		Logger:        log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := os.Stat(hb); !os.IsNotExist(err) {
		t.Fatalf("heartbeat file should not exist before start (err=%v)", err)
	}

	stop := l.startHeartbeat()
	defer stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(hb); err == nil {
			return // keepalive wrote the heartbeat
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("startHeartbeat did not refresh the heartbeat file within 2s")
}

// fakeConverser is a provider.Provider + provider.ToolConverser that replays a
// scripted sequence of turns.
type fakeConverser struct {
	turns     []provider.Turn
	idx       int
	calls     int
	lastTools []provider.ToolSpec
}

func (f *fakeConverser) Query(context.Context, string) (string, error) {
	return "", errors.New("Query must not be called when tool use is available")
}

func (f *fakeConverser) Converse(_ context.Context, _ []provider.Message, specs []provider.ToolSpec) (provider.Turn, error) {
	f.calls++
	f.lastTools = specs
	if f.idx >= len(f.turns) {
		return provider.Turn{StopReason: "end_turn"}, nil
	}
	t := f.turns[f.idx]
	f.idx++
	return t, nil
}

// echoTool is a trivial test tool that records its invocations.
type echoTool struct {
	invoked   int
	lastInput json.RawMessage
}

func (e *echoTool) Name() string                { return "echo" }
func (e *echoTool) Description() string         { return "echo the input back" }
func (e *echoTool) JSONSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (e *echoTool) Invoke(_ context.Context, input json.RawMessage) (string, error) {
	e.invoked++
	e.lastInput = input
	return "echoed: " + string(input), nil
}

func newToolLoop(t *testing.T, in *fakeInbound, out *fakeOutbound, prov provider.Provider, reg *tools.Registry) *Loop {
	t.Helper()
	l, err := New(Config{
		Inbound:       in,
		Outbound:      out,
		Provider:      prov,
		Tools:         reg,
		HeartbeatPath: filepath.Join(t.TempDir(), "heartbeat"),
		Clock:         func() time.Time { return time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC) },
		Logger:        log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return l
}

// TestToolUseLoopInvokesTools asserts the agentic loop executes a requested tool
// and returns the model's final text after the tool result.
func TestToolUseLoopInvokesTools(t *testing.T) {
	reg := tools.NewRegistry()
	echo := &echoTool{}
	if err := reg.Register(echo); err != nil {
		t.Fatalf("register echo: %v", err)
	}
	prov := &fakeConverser{turns: []provider.Turn{
		{
			StopReason: "tool_use",
			ToolCalls:  []provider.ToolCall{{ID: "t1", Name: "echo", Input: json.RawMessage(`{"x":1}`)}},
			Assistant:  provider.Message{Role: "assistant"},
		},
		{StopReason: "end_turn", Text: "done"},
	}}
	in := &fakeInbound{pending: []contract.MessageIn{msg("m1", "hello", 1)}}
	out := &fakeOutbound{}
	l := newToolLoop(t, in, out, prov, reg)

	if err := l.poll(context.Background(), false); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if echo.invoked != 1 {
		t.Fatalf("echo invoked %d times, want 1", echo.invoked)
	}
	if prov.calls != 2 {
		t.Fatalf("Converse calls = %d, want 2 (tool turn + final turn)", prov.calls)
	}
	if len(prov.lastTools) == 0 {
		t.Fatal("tools were not offered to the model")
	}
	if len(out.writes) != 1 || out.writes[0].Content != "done" || out.writes[0].Kind != contract.KindChat {
		t.Fatalf("outbound writes = %+v, want one chat reply %q", out.writes, "done")
	}
}

// TestToolUseLoopWithMockProvider drives the agentic tool loop with the REAL
// shipped MockProvider (not a scripted fake): a `tool:echo {...}` directive in the
// inbound message makes the mock emit that call, the loop runs the tool, and the
// mock surfaces the tool's output. This is the credential-free proof that an agent
// can actually USE an enabled tool end-to-end.
func TestToolUseLoopWithMockProvider(t *testing.T) {
	reg := tools.NewRegistry()
	echo := &echoTool{}
	if err := reg.Register(echo); err != nil {
		t.Fatalf("register echo: %v", err)
	}
	prov := provider.NewMock(provider.Config{})
	in := &fakeInbound{pending: []contract.MessageIn{msg("m1", `run tool:echo {"x":1}`, 1)}}
	out := &fakeOutbound{}
	l := newToolLoop(t, in, out, prov, reg)

	if err := l.poll(context.Background(), false); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if echo.invoked != 1 {
		t.Fatalf("echo invoked %d times, want 1", echo.invoked)
	}
	if len(out.writes) != 1 {
		t.Fatalf("outbound writes = %d, want 1", len(out.writes))
	}
	if !strings.Contains(out.writes[0].Content, "echoed:") {
		t.Fatalf("reply %q does not surface the echo tool result", out.writes[0].Content)
	}
}

// TestCapabilityChangeForwardedToOutbound asserts that when the agent invokes
// request_capability_change, the envelope is forwarded to the host gateway as a
// system message — the sandbox never applies the change itself.
func TestCapabilityChangeForwardedToOutbound(t *testing.T) {
	reg := tools.NewRegistry()
	if err := reg.Register(tools.NewRequestCapabilityChangeTool()); err != nil {
		t.Fatalf("register capability tool: %v", err)
	}
	prov := &fakeConverser{turns: []provider.Turn{
		{
			StopReason: "tool_use",
			ToolCalls: []provider.ToolCall{{
				ID:    "t1",
				Name:  tools.CapabilityChangeToolName,
				Input: json.RawMessage(`{"kind":"packages","payload":{"add":["jq"]},"reason":"need jq"}`),
			}},
			Assistant: provider.Message{Role: "assistant"},
		},
		{StopReason: "end_turn", Text: "submitted your request"},
	}}
	in := &fakeInbound{pending: []contract.MessageIn{msg("m1", "install jq", 1)}}
	out := &fakeOutbound{}
	l := newToolLoop(t, in, out, prov, reg)

	if err := l.poll(context.Background(), false); err != nil {
		t.Fatalf("poll: %v", err)
	}

	var chatWrites, systemWrites int
	var envelope contract.MessageOut
	for _, w := range out.writes {
		switch w.Kind {
		case contract.KindChat:
			chatWrites++
		case contract.KindSystem:
			systemWrites++
			envelope = w
		}
	}
	if chatWrites != 1 {
		t.Fatalf("chat writes = %d, want 1", chatWrites)
	}
	if systemWrites != 1 {
		t.Fatalf("system (gateway) writes = %d, want 1", systemWrites)
	}

	// The forwarded content must be in the host's system-action wire format
	// (keyed on "action"), which host delivery parses and maps to a gateway
	// ChangeKind. The action name is the ChangeKind string ("packages").
	var sysAction struct {
		Action  string          `json:"action"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal([]byte(envelope.Content), &sysAction); err != nil {
		t.Fatalf("forwarded system message not JSON: %v", err)
	}
	if sysAction.Action != string(contract.ChangePackages) {
		t.Fatalf("forwarded action = %q, want %q (host maps this to a gateway ChangeKind)", sysAction.Action, contract.ChangePackages)
	}
	if len(sysAction.Payload) == 0 {
		t.Fatal("forwarded system message dropped the payload")
	}
}

// TestPersistsBufferedState asserts that accumulating a trigger=0 message writes
// the buffer to durable session state, so an unclean exit before engagement can
// recover it.
func TestPersistsBufferedState(t *testing.T) {
	in := &fakeInbound{pending: []contract.MessageIn{msg("m1", "hold me", 0)}}
	out := &fakeOutbound{}
	prov := &fakeProvider{reply: "unused"}
	l := newTestLoop(t, in, out, prov)

	if err := l.poll(context.Background(), false); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if prov.calls != 0 {
		t.Fatalf("trigger=0 should not engage: calls=%d", prov.calls)
	}
	raw := out.state[stateKeyBuffer]
	if raw == "" || raw == "null" || raw == "[]" {
		t.Fatalf("buffer not persisted: %q", raw)
	}
	var buf []contract.MessageIn
	if err := json.Unmarshal([]byte(raw), &buf); err != nil {
		t.Fatalf("persisted buffer not valid JSON: %v", err)
	}
	if len(buf) != 1 || buf[0].ID != "m1" || buf[0].Content != "hold me" {
		t.Fatalf("persisted buffer = %+v, want [m1 \"hold me\"]", buf)
	}
}

// TestRestoresBufferAcrossRestart asserts a fresh Loop over the same outbound
// state recovers an accumulated (un-engaged) message and engages it on the
// cold-start poll, instead of dropping it.
func TestRestoresBufferAcrossRestart(t *testing.T) {
	in := &fakeInbound{}
	out := &fakeOutbound{}
	prov := &fakeProvider{reply: "ok"}

	// Incarnation 1 accumulates a trigger=0 message but never engages it.
	l1 := newTestLoop(t, in, out, prov)
	in.pending = []contract.MessageIn{msg("m1", "remember me", 0)}
	if err := l1.poll(context.Background(), false); err != nil {
		t.Fatalf("incarnation 1 poll: %v", err)
	}
	if prov.calls != 0 {
		t.Fatalf("incarnation 1 should not engage: calls=%d", prov.calls)
	}

	// Incarnation 2 is a new Loop sharing the persisted outbound state. The
	// message is still pending (the host has not advanced it). The cold-start
	// poll must engage on the restored buffer.
	l2 := newTestLoop(t, in, out, prov)
	if len(l2.buffer) != 1 || l2.buffer[0].ID != "m1" {
		t.Fatalf("restored buffer = %+v, want [m1]", l2.buffer)
	}
	if err := l2.poll(context.Background(), true); err != nil {
		t.Fatalf("restart poll: %v", err)
	}
	if prov.calls != 1 {
		t.Fatalf("restart dropped the restored buffer: calls=%d, want 1", prov.calls)
	}
	if !strings.Contains(prov.lastPrompt, "remember me") {
		t.Fatalf("restored prompt = %q, want it to contain the buffered message", prov.lastPrompt)
	}
}

// TestDoneIDsSurviveRestart asserts the restored dedup set prevents re-engaging a
// message that was completed before the restart but still shows pending due to
// host status lag.
func TestDoneIDsSurviveRestart(t *testing.T) {
	in := &fakeInbound{}
	out := &fakeOutbound{}
	prov := &fakeProvider{reply: "ok"}

	// Incarnation 1 engages and completes m1 (trigger=1 engages immediately).
	l1 := newTestLoop(t, in, out, prov)
	in.pending = []contract.MessageIn{msg("m1", "hello", 1)}
	if err := l1.poll(context.Background(), false); err != nil {
		t.Fatalf("incarnation 1 poll: %v", err)
	}
	if prov.calls != 1 {
		t.Fatalf("incarnation 1 calls = %d, want 1", prov.calls)
	}

	// Incarnation 2 restarts while the host still reports m1 pending. The restored
	// done set must suppress a second engage.
	l2 := newTestLoop(t, in, out, prov)
	if _, done := l2.doneIDs["m1"]; !done {
		t.Fatalf("restored done set missing m1: %v", l2.doneIDs)
	}
	if err := l2.poll(context.Background(), true); err != nil {
		t.Fatalf("restart poll: %v", err)
	}
	if prov.calls != 1 {
		t.Fatalf("m1 was reprocessed after restart: calls=%d, want 1", prov.calls)
	}
}
