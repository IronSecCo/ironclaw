package provider

import (
	"context"
	"strings"
	"testing"
)

func TestMockProvider_QueryEchoesMarker(t *testing.T) {
	p := NewMock(Config{})
	const marker = "PWCHAT-xyz-123"
	got, err := p.Query(context.Background(), "Reply with EXACTLY: "+marker)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !strings.Contains(got, marker) {
		t.Fatalf("reply %q does not contain marker %q", got, marker)
	}
	if !strings.HasPrefix(got, mockReplyPrefix) {
		t.Fatalf("reply %q missing prefix %q", got, mockReplyPrefix)
	}
}

func TestMockProvider_QueryTrimsAndIsDeterministic(t *testing.T) {
	p := NewMock(Config{})
	a, _ := p.Query(context.Background(), "  hello  ")
	b, _ := p.Query(context.Background(), "hello")
	if a != b {
		t.Fatalf("expected trimmed inputs to be deterministic: %q vs %q", a, b)
	}
	if a != mockReplyPrefix+"hello" {
		t.Fatalf("unexpected reply %q", a)
	}
}

func TestMockProvider_QueryHonorsCancellation(t *testing.T) {
	p := NewMock(Config{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Query(ctx, "anything"); err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// TestNewMock_ViaRegistry confirms the "mock" kind resolves through the public
// New factory and that it IS a ToolConverser (so the loop can drive tool use).
func TestNewMock_ViaRegistry(t *testing.T) {
	p, err := New(Config{Kind: KindMock})
	if err != nil {
		t.Fatalf("New(mock): %v", err)
	}
	if _, ok := p.(ToolConverser); !ok {
		t.Fatal("MockProvider must implement ToolConverser")
	}
	if _, ok := p.(*MockProvider); !ok {
		t.Fatalf("New(mock) returned %T, want *MockProvider", p)
	}
}

func TestMockProvider_ConverseEchoesWhenNoDirective(t *testing.T) {
	p := NewMock(Config{})
	turn, err := p.Converse(context.Background(),
		[]Message{UserTextMessage("just say hi")}, nil)
	if err != nil {
		t.Fatalf("Converse: %v", err)
	}
	if len(turn.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(turn.ToolCalls))
	}
	if turn.Text != mockReplyPrefix+"just say hi" {
		t.Fatalf("unexpected text %q", turn.Text)
	}
}

func TestMockProvider_ConverseEmitsOfferedToolCall(t *testing.T) {
	p := NewMock(Config{})
	specs := []ToolSpec{{Name: "read_file"}, {Name: "web_search"}}
	turn, err := p.Converse(context.Background(),
		[]Message{UserTextMessage(`please tool:read_file {"path":"/x"}`)}, specs)
	if err != nil {
		t.Fatalf("Converse: %v", err)
	}
	if len(turn.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(turn.ToolCalls))
	}
	if turn.ToolCalls[0].Name != "read_file" {
		t.Fatalf("tool %q, want read_file", turn.ToolCalls[0].Name)
	}
	if string(turn.ToolCalls[0].Input) != `{"path":"/x"}` {
		t.Fatalf("args %q, want {\"path\":\"/x\"}", turn.ToolCalls[0].Input)
	}
}

func TestMockProvider_ConverseIgnoresUnofferedTool(t *testing.T) {
	p := NewMock(Config{})
	// read_file is NOT offered, so the directive must fall back to an echo rather
	// than fabricate a call to a tool the group did not enable.
	turn, err := p.Converse(context.Background(),
		[]Message{UserTextMessage(`tool:read_file {}`)}, []ToolSpec{{Name: "web_search"}})
	if err != nil {
		t.Fatalf("Converse: %v", err)
	}
	if len(turn.ToolCalls) != 0 {
		t.Fatalf("expected no tool call for an un-offered tool, got %d", len(turn.ToolCalls))
	}
}

func TestMockProvider_ConverseSurfacesToolResult(t *testing.T) {
	p := NewMock(Config{})
	history := []Message{
		UserTextMessage(`tool:read_file {"path":"/x"}`),
		{Role: "assistant", Content: []Block{{Type: "tool_use", ID: "t1", Name: "read_file"}}},
		ToolResultsMessage([]ToolResult{{ToolUseID: "t1", Content: "FILE-CONTENTS-42"}}),
	}
	turn, err := p.Converse(context.Background(), history, []ToolSpec{{Name: "read_file"}})
	if err != nil {
		t.Fatalf("Converse: %v", err)
	}
	if len(turn.ToolCalls) != 0 {
		t.Fatalf("expected terminal turn, got %d tool calls", len(turn.ToolCalls))
	}
	if !strings.Contains(turn.Text, "FILE-CONTENTS-42") {
		t.Fatalf("reply %q does not surface the tool result", turn.Text)
	}
}
