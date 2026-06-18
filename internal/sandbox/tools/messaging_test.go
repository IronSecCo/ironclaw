package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// fakeMsgCtx is a static MessageContext for the messaging-tool tests.
type fakeMsgCtx struct {
	dests   []contract.Destination
	routing contract.SessionRouting
}

func (f fakeMsgCtx) Destinations() ([]contract.Destination, error)    { return f.dests, nil }
func (f fakeMsgCtx) SessionRouting() (contract.SessionRouting, error) { return f.routing, nil }
func sp(s string) *string                                             { return &s }

func dest(name, channel, platform string) contract.Destination {
	return contract.Destination{Name: name, ChannelType: sp(channel), PlatformID: sp(platform)}
}

// emitOne runs a tool's Invoke then ToOutbound and asserts exactly one message.
func emitOne(t *testing.T, tool interface {
	Invoke(context.Context, json.RawMessage) (string, error)
	ToOutbound(string) ([]contract.MessageOut, error)
}, input string) contract.MessageOut {
	t.Helper()
	out, err := tool.Invoke(context.Background(), json.RawMessage(input))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	msgs, err := tool.ToOutbound(out)
	if err != nil {
		t.Fatalf("ToOutbound: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected exactly 1 emitted message, got %d", len(msgs))
	}
	return msgs[0]
}

func TestSendMessageNamedDestination(t *testing.T) {
	ctxt := fakeMsgCtx{dests: []contract.Destination{dest("alerts", "slack", "C123")}}
	tool := NewSendMessageTool(ctxt)
	msg := emitOne(t, tool, `{"text":"deploy done","to":"alerts"}`)

	if msg.Kind != contract.KindChat {
		t.Fatalf("kind = %q, want chat", msg.Kind)
	}
	if msg.ChannelType == nil || *msg.ChannelType != "slack" || msg.PlatformID == nil || *msg.PlatformID != "C123" {
		t.Fatalf("coords wrong: ct=%v pid=%v", msg.ChannelType, msg.PlatformID)
	}
	if msg.Content != "deploy done" {
		t.Fatalf("content = %q", msg.Content)
	}
}

func TestSendMessageCurrentThread(t *testing.T) {
	ctxt := fakeMsgCtx{routing: contract.SessionRouting{ChannelType: "discord", PlatformID: "D1", ThreadID: sp("t-9")}}
	tool := NewSendMessageTool(ctxt)
	// No "to" → current thread.
	msg := emitOne(t, tool, `{"text":"hi there"}`)
	if msg.ChannelType == nil || *msg.ChannelType != "discord" || msg.PlatformID == nil || *msg.PlatformID != "D1" {
		t.Fatalf("expected current-thread coords, got ct=%v pid=%v", msg.ChannelType, msg.PlatformID)
	}
	if msg.ThreadID == nil || *msg.ThreadID != "t-9" {
		t.Fatalf("expected thread t-9, got %v", msg.ThreadID)
	}
}

func TestSendMessageUnknownDestination(t *testing.T) {
	ctxt := fakeMsgCtx{dests: []contract.Destination{dest("alerts", "slack", "C1")}}
	tool := NewSendMessageTool(ctxt)
	_, err := tool.Invoke(context.Background(), json.RawMessage(`{"text":"x","to":"nope"}`))
	if err == nil || !strings.Contains(err.Error(), "alerts") {
		t.Fatalf("expected unknown-destination error listing known names, got %v", err)
	}
}

func TestSendMessageValidation(t *testing.T) {
	tool := NewSendMessageTool(fakeMsgCtx{})
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"text":"  "}`)); err == nil {
		t.Fatal("empty text should error")
	}
	// No routing configured and no destination named → cannot resolve a target.
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"text":"hello"}`)); err == nil {
		t.Fatal("missing routing with no destination should error")
	}
}

func TestSendFileSendsTextContent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("line one\nline two"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	ws, err := NewWorkspace(dir)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	ctxt := fakeMsgCtx{dests: []contract.Destination{dest("ops", "slack", "C9")}}
	tool := NewSendFileTool(ws, ctxt)

	msg := emitOne(t, tool, `{"path":"note.txt","to":"ops","caption":"the log"}`)
	if msg.ChannelType == nil || *msg.ChannelType != "slack" {
		t.Fatalf("coords wrong: %v", msg.ChannelType)
	}
	if !strings.Contains(msg.Content, "[file: note.txt]") || !strings.Contains(msg.Content, "line one") || !strings.Contains(msg.Content, "the log") {
		t.Fatalf("content missing header/caption/body: %q", msg.Content)
	}
}

func TestSendFileRejectsBadPaths(t *testing.T) {
	dir := t.TempDir()
	ws, _ := NewWorkspace(dir)
	tool := NewSendFileTool(ws, fakeMsgCtx{routing: contract.SessionRouting{ChannelType: "slack", PlatformID: "C1"}})

	// Escaping path.
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"path":"../secret"}`)); err == nil {
		t.Fatal("path escaping the workspace should error")
	}
	// Missing file.
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"path":"missing.txt"}`)); err == nil {
		t.Fatal("missing file should error")
	}
	// Binary (invalid UTF-8) file is refused.
	if err := os.WriteFile(filepath.Join(dir, "bin"), []byte{0xff, 0xfe, 0x00}, 0o644); err != nil {
		t.Fatalf("seed binary: %v", err)
	}
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"path":"bin"}`)); err == nil {
		t.Fatal("binary file should be refused")
	}
}

func TestToOutboundRejectsMissingCoords(t *testing.T) {
	// A malformed envelope with no coordinates must not produce a sendable message.
	if _, err := toOutbound(`{"content":"x"}`); err == nil {
		t.Fatal("missing channel/platform should error")
	}
	if _, err := toOutbound(`not json`); err == nil {
		t.Fatal("invalid JSON should error")
	}
}

func TestListDestinations(t *testing.T) {
	ctxt := fakeMsgCtx{dests: []contract.Destination{
		dest("ops", "slack", "C9"),
		{Name: "alerts", DisplayName: sp("Alerts"), ChannelType: sp("discord"), PlatformID: sp("D1")},
	}}
	tool := NewListDestinationsTool(ctxt)
	out, err := tool.Invoke(context.Background(), nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var views []destinationView
	if err := json.Unmarshal([]byte(out), &views); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(views) != 2 || views[0].Name != "alerts" || views[1].Name != "ops" {
		t.Fatalf("expected sorted [alerts, ops], got %+v", views)
	}
}

func TestMessagingToolsAreNotForbidden(t *testing.T) {
	// The messaging tools must be registrable (they emit chat, not capability changes).
	reg := NewRegistry()
	for _, tool := range []Tool{
		NewSendMessageTool(fakeMsgCtx{}),
		NewSendFileTool(nil, fakeMsgCtx{}),
		NewListDestinationsTool(fakeMsgCtx{}),
	} {
		if err := reg.Register(tool); err != nil {
			t.Fatalf("register %s: %v", tool.Name(), err)
		}
	}
}
