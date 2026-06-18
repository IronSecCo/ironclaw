package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// chatHelloStream produces "hello world" across two content deltas, then stops.
var chatHelloStream = sse(
	`{"choices":[{"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
	`{"choices":[{"delta":{"content":"hello "},"finish_reason":null}]}`,
	`{"choices":[{"delta":{"content":"world"},"finish_reason":null}]}`,
	`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	`[DONE]`,
)

func TestOpenAIQuerySuccess(t *testing.T) {
	var gotBody []byte
	var gotPath, gotHost string
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHost = r.Host
		gotBody, _ = io.ReadAll(r.Body)
		writeSSE(w, chatHelloStream)
	}))

	p := NewOpenAI(Config{SocketPath: sock})
	out, err := p.Query(context.Background(), "hi there")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if out != "hello world" {
		t.Fatalf("Query output = %q, want %q", out, "hello world")
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotHost != openAIUpstreamHost {
		t.Fatalf("Host = %q, want %q (proxy allowlists on Host)", gotHost, openAIUpstreamHost)
	}

	var req oaiChatRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if req.Model != defaultOpenAIModel {
		t.Fatalf("model = %q, want %q", req.Model, defaultOpenAIModel)
	}
	if !req.Stream {
		t.Fatal("stream = false, want true")
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" || req.Messages[0].Content != "hi there" {
		t.Fatalf("messages = %+v, want one user message %q", req.Messages, "hi there")
	}
}

func TestOpenAISystem(t *testing.T) {
	var gotBody []byte
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		writeSSE(w, chatHelloStream)
	}))
	p := NewOpenAI(Config{SocketPath: sock, System: "be terse"})
	if _, err := p.Query(context.Background(), "x"); err != nil {
		t.Fatalf("Query: %v", err)
	}
	var req oaiChatRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[0].Content != "be terse" {
		t.Fatalf("messages = %+v, want a leading system message", req.Messages)
	}
}

func TestOpenAITools(t *testing.T) {
	var gotBody []byte
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		writeSSE(w, sse(
			`{"choices":[{"delta":{"content":"let me check"},"finish_reason":null}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read_file","arguments":""}}]},"finish_reason":null}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]},"finish_reason":null}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.txt\"}"}}]},"finish_reason":null}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`[DONE]`,
		))
	}))

	p := NewOpenAI(Config{SocketPath: sock})
	turn, err := p.Converse(context.Background(),
		[]Message{UserTextMessage("read a.txt")},
		[]ToolSpec{{Name: "read_file", Description: "read a file", InputSchema: json.RawMessage(`{"type":"object"}`)}})
	if err != nil {
		t.Fatalf("Converse: %v", err)
	}
	if turn.Text != "let me check" {
		t.Fatalf("text = %q", turn.Text)
	}
	if turn.StopReason != "tool_use" {
		t.Fatalf("stop reason = %q, want tool_use (normalized)", turn.StopReason)
	}
	if len(turn.ToolCalls) != 1 || turn.ToolCalls[0].Name != "read_file" || turn.ToolCalls[0].ID != "call_1" {
		t.Fatalf("tool calls = %+v", turn.ToolCalls)
	}
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(turn.ToolCalls[0].Input, &input); err != nil || input.Path != "a.txt" {
		t.Fatalf("reassembled tool input = %s (err %v)", turn.ToolCalls[0].Input, err)
	}
	// The assistant message must round-trip back as Anthropic-shaped blocks.
	if len(turn.Assistant.Content) != 2 || turn.Assistant.Content[1].Type != "tool_use" {
		t.Fatalf("assistant content = %+v", turn.Assistant.Content)
	}

	// The request must carry the tool in OpenAI function shape.
	var req oaiChatRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Type != "function" || req.Tools[0].Function.Name != "read_file" {
		t.Fatalf("tools in request = %+v", req.Tools)
	}
}

// TestToOpenAIMessages checks the translation of an Anthropic-shaped tool-use
// round (assistant tool_use → user tool_result) into OpenAI's assistant-with-
// tool_calls + role:"tool" message shape, in order.
func TestToOpenAIMessages(t *testing.T) {
	history := []Message{
		UserTextMessage("read a.txt"),
		{Role: "assistant", Content: []Block{
			{Type: "text", Text: "checking"},
			{Type: "tool_use", ID: "call_1", Name: "read_file", Input: json.RawMessage(`{"path":"a.txt"}`)},
		}},
		ToolResultsMessage([]ToolResult{{ToolUseID: "call_1", Content: "file body"}}),
	}
	msgs := toOpenAIMessages(history)
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != "user" || msgs[0].Content != "read a.txt" {
		t.Fatalf("msg0 = %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || len(msgs[1].ToolCalls) != 1 ||
		msgs[1].ToolCalls[0].ID != "call_1" || msgs[1].ToolCalls[0].Function.Arguments != `{"path":"a.txt"}` {
		t.Fatalf("msg1 = %+v", msgs[1])
	}
	if msgs[2].Role != "tool" || msgs[2].ToolCallID != "call_1" || msgs[2].Content != "file body" {
		t.Fatalf("msg2 = %+v", msgs[2])
	}
}

func TestOpenAIError(t *testing.T) {
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"invalid api key","type":"invalid_request_error"}}`)
	}))
	p := NewOpenAI(Config{SocketPath: sock})
	_, err := p.Query(context.Background(), "x")
	if err == nil {
		t.Fatal("Query: want error, got nil")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("error = %q, want it to mention 401 and the API message", err)
	}
}

func TestNewProviderFactory(t *testing.T) {
	if pv, err := New(Config{}); err != nil {
		t.Fatalf("default: %v", err)
	} else if _, ok := pv.(*AnthropicProvider); !ok {
		t.Fatalf("default kind = %T, want *AnthropicProvider", pv)
	}
	if pv, err := New(Config{Kind: KindAnthropic}); err != nil || pv == nil {
		t.Fatalf("anthropic: %v", err)
	}
	if pv, err := New(Config{Kind: KindOpenAI}); err != nil {
		t.Fatalf("openai: %v", err)
	} else if _, ok := pv.(*OpenAIProvider); !ok {
		t.Fatalf("openai kind = %T, want *OpenAIProvider", pv)
	}
	if pv, err := New(Config{Kind: "OpenRouter"}); err != nil { // case-insensitive
		t.Fatalf("openrouter: %v", err)
	} else if op, ok := pv.(*OpenAIProvider); !ok {
		t.Fatalf("openrouter kind = %T, want *OpenAIProvider", pv)
	} else if !strings.Contains(op.url, "openrouter.ai/api/v1/chat/completions") {
		t.Fatalf("openrouter url = %q, want the /api/v1 path", op.url)
	}
	if _, err := New(Config{Kind: "bogus"}); err == nil {
		t.Fatal("unknown kind: want error, got nil")
	}
}
