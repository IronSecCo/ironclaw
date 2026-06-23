package smoke

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	hostqueue "github.com/IronSecCo/ironclaw/internal/host/queue"
	"github.com/IronSecCo/ironclaw/internal/sandbox/loop"
	"github.com/IronSecCo/ironclaw/internal/sandbox/provider"
	sandboxqueue "github.com/IronSecCo/ironclaw/internal/sandbox/queue"
	"github.com/IronSecCo/ironclaw/internal/sandbox/tools"
)

// demoKey is a fixed per-session key. The demo path uses a real, random key; a
// deterministic one keeps the test reproducible while still exercising the full
// encrypted-SQLite (SQLCipher) binding both sides open against.
func demoKey() contract.SessionKey {
	var k contract.SessionKey
	for i := range k {
		k[i] = byte(i + 1)
	}
	return k
}

// TestZeroCredChatDemoEndToEnd is the smoke test guarding the README hero flow:
// the zero-credential `mock`-provider chat demo. It wires the same components the
// running daemon does — the host writes one inbound chat message, the sandbox
// reasoning loop reads it through the encrypted inbound queue, runs the offline
// mock provider, and writes the reply to the encrypted outbound queue, which the
// host reads back — all with no network, no credential, and no Docker.
//
// If any link in that chain regresses (queue schema, loop engagement, mock echo
// contract, outbound routing), this test fails instead of the demo silently
// breaking for a first-time contributor.
func TestZeroCredChatDemoEndToEnd(t *testing.T) {
	dir := t.TempDir()
	key := demoKey()
	inboundPath := filepath.Join(dir, "inbound.db")
	outboundPath := filepath.Join(dir, "outbound.db")

	const userText = "hello from the hero demo"
	const wantReply = "mock-agent received: " + userText

	// Host side (sole inbound writer) seeds one immediate chat message, exactly as
	// a real chat send would. trigger=1 engages the model on the first poll rather
	// than accumulating.
	hostIn, err := hostqueue.OpenInbound(inboundPath, key)
	if err != nil {
		t.Fatalf("open host inbound: %v", err)
	}
	msgID := contract.MessageID("demo-msg-1")
	if err := hostIn.WriteMessageIn(contract.MessageIn{
		ID:        msgID,
		Seq:       2, // even: host parity
		Kind:      contract.KindChat,
		Timestamp: time.Now().UTC(),
		Status:    contract.StatusQueued,
		Trigger:   1,
		Content:   userText,
	}); err != nil {
		t.Fatalf("write inbound message: %v", err)
	}
	if err := hostIn.Close(); err != nil {
		t.Fatalf("close host inbound: %v", err)
	}

	// Sandbox side: read-only inbound view and the outbound writer. Opening the
	// outbound writer creates the encrypted outbound DB so the host reader below
	// has a file to open.
	sbIn, err := sandboxqueue.OpenInbound(inboundPath, key)
	if err != nil {
		t.Fatalf("open sandbox inbound: %v", err)
	}
	sbOut, err := sandboxqueue.OpenOutbound(outboundPath, key)
	if err != nil {
		t.Fatalf("open sandbox outbound: %v", err)
	}

	// The zero-credential offline backend. It makes no network call — not even to
	// the host model-proxy socket — so the demo needs no API key.
	prov, err := provider.New(provider.Config{Kind: provider.KindMock})
	if err != nil {
		t.Fatalf("build mock provider: %v", err)
	}

	lp, err := loop.New(loop.Config{
		Inbound:       sbIn,
		Outbound:      sbOut,
		Provider:      prov,
		HeartbeatPath: filepath.Join(dir, ".heartbeat"),
		PollInterval:  5 * time.Millisecond,
		Logger:        log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("build loop: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	loopDone := make(chan error, 1)
	go func() { loopDone <- lp.Run(ctx) }()

	// Host reader of the outbound queue (sole outbound reader).
	hostOut, err := hostqueue.OpenOutbound(outboundPath, key)
	if err != nil {
		t.Fatalf("open host outbound: %v", err)
	}
	defer hostOut.Close()

	var got []contract.MessageOut
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		msgs, err := hostOut.DueMessages()
		if err != nil {
			t.Fatalf("read outbound: %v", err)
		}
		if len(msgs) > 0 {
			got = msgs
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not stop after context cancel")
	}

	if len(got) == 0 {
		t.Fatal("no outbound reply within deadline: zero-cred chat demo path is broken")
	}
	if len(got) != 1 {
		t.Fatalf("got %d outbound messages, want exactly 1", len(got))
	}
	reply := got[0]
	if reply.Content != wantReply {
		t.Fatalf("reply content = %q, want %q", reply.Content, wantReply)
	}
	if reply.Kind != contract.KindChat {
		t.Fatalf("reply kind = %q, want %q", reply.Kind, contract.KindChat)
	}
	if reply.InReplyTo == nil || *reply.InReplyTo != msgID {
		t.Fatalf("reply InReplyTo = %v, want %q", reply.InReplyTo, msgID)
	}
}

// echoUpperTool is a hermetic in-sandbox tool: it upper-cases its "text" input.
// It exists only to drive the loop's agentic tool-use path end-to-end with no
// network — the mock provider's `tool:<name> {json}` directive selects it.
type echoUpperTool struct{}

func (echoUpperTool) Name() string        { return "echo_upper" }
func (echoUpperTool) Description() string { return "Upper-cases the provided text." }
func (echoUpperTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)
}

func (echoUpperTool) Invoke(_ context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", err
	}
	return strings.ToUpper(in.Text), nil
}

// TestZeroCredToolUseDemoEndToEnd guards the demo's "agent uses an added tool
// with no credential" story. With a tool registered, the loop drives the agentic
// Converse path: the offline mock parses the `tool:echo_upper {json}` directive,
// emits the tool call, the loop invokes the (hermetic) tool, feeds the result
// back, and the mock surfaces it. This exercises loop.runAgent + invokeTool +
// the mock Converse turn — the tool-use integration the README hero shows off —
// with zero network and zero credentials.
func TestZeroCredToolUseDemoEndToEnd(t *testing.T) {
	dir := t.TempDir()
	key := demoKey()
	inboundPath := filepath.Join(dir, "inbound.db")
	outboundPath := filepath.Join(dir, "outbound.db")

	const userText = `tool:echo_upper {"text":"ping"}`
	const wantReply = "mock-agent tool result: PING"

	hostIn, err := hostqueue.OpenInbound(inboundPath, key)
	if err != nil {
		t.Fatalf("open host inbound: %v", err)
	}
	msgID := contract.MessageID("tool-msg-1")
	if err := hostIn.WriteMessageIn(contract.MessageIn{
		ID:        msgID,
		Seq:       2,
		Kind:      contract.KindChat,
		Timestamp: time.Now().UTC(),
		Status:    contract.StatusQueued,
		Trigger:   1,
		Content:   userText,
	}); err != nil {
		t.Fatalf("write inbound message: %v", err)
	}
	if err := hostIn.Close(); err != nil {
		t.Fatalf("close host inbound: %v", err)
	}

	sbIn, err := sandboxqueue.OpenInbound(inboundPath, key)
	if err != nil {
		t.Fatalf("open sandbox inbound: %v", err)
	}
	sbOut, err := sandboxqueue.OpenOutbound(outboundPath, key)
	if err != nil {
		t.Fatalf("open sandbox outbound: %v", err)
	}

	prov, err := provider.New(provider.Config{Kind: provider.KindMock})
	if err != nil {
		t.Fatalf("build mock provider: %v", err)
	}

	toolReg := tools.NewRegistry()
	if err := toolReg.Register(echoUpperTool{}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	lp, err := loop.New(loop.Config{
		Inbound:       sbIn,
		Outbound:      sbOut,
		Provider:      prov,
		Tools:         toolReg,
		HeartbeatPath: filepath.Join(dir, ".heartbeat"),
		PollInterval:  5 * time.Millisecond,
		Logger:        log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("build loop: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	loopDone := make(chan error, 1)
	go func() { loopDone <- lp.Run(ctx) }()

	hostOut, err := hostqueue.OpenOutbound(outboundPath, key)
	if err != nil {
		t.Fatalf("open host outbound: %v", err)
	}
	defer hostOut.Close()

	var got []contract.MessageOut
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		msgs, err := hostOut.DueMessages()
		if err != nil {
			t.Fatalf("read outbound: %v", err)
		}
		// Skip any KindSystem envelopes; we want the chat reply surfacing the tool result.
		for _, m := range msgs {
			if m.Kind == contract.KindChat {
				got = append(got, m)
			}
		}
		if len(got) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not stop after context cancel")
	}

	if len(got) == 0 {
		t.Fatal("no outbound chat reply within deadline: zero-cred tool-use demo path is broken")
	}
	if got[0].Content != wantReply {
		t.Fatalf("reply content = %q, want %q", got[0].Content, wantReply)
	}
}
