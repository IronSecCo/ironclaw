// OWNER: AGENT2

package provider

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// serveUnix starts an HTTP server on a unix socket with the given handler and
// returns the socket path. The server is closed when the test ends.
func serveUnix(t *testing.T, handler http.Handler) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "proxy.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	srv := &http.Server{Handler: handler}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return sock
}

func TestQuerySuccess(t *testing.T) {
	var gotBody []byte
	var gotVersion, gotPath string
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVersion = r.Header.Get("anthropic-version")
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("content-type", "application/json")
		// Thinking block has empty text and must be ignored; two text blocks concatenate.
		io.WriteString(w, `{"content":[{"type":"thinking","text":""},{"type":"text","text":"hello "},{"type":"text","text":"world"}],"stop_reason":"end_turn"}`)
	}))

	p := NewAnthropic(Config{SocketPath: sock})
	out, err := p.Query(context.Background(), "hi there")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if out != "hello world" {
		t.Fatalf("Query output = %q, want %q", out, "hello world")
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("request path = %q, want /v1/messages", gotPath)
	}
	if gotVersion != anthropicVersion {
		t.Fatalf("anthropic-version = %q, want %q", gotVersion, anthropicVersion)
	}

	var req messagesRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if req.Model != defaultModel {
		t.Fatalf("model = %q, want %q", req.Model, defaultModel)
	}
	if req.MaxTokens != defaultMaxTokens {
		t.Fatalf("max_tokens = %d, want %d", req.MaxTokens, defaultMaxTokens)
	}
	if req.Thinking == nil || req.Thinking.Type != "adaptive" {
		t.Fatalf("thinking = %+v, want adaptive", req.Thinking)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" ||
		len(req.Messages[0].Content) != 1 || req.Messages[0].Content[0].Text != "hi there" {
		t.Fatalf("messages = %+v, want one user text message %q", req.Messages, "hi there")
	}
	// The sandbox must not send credentials — the host proxy injects them.
	if h := strings.TrimSpace(string(gotBody)); strings.Contains(strings.ToLower(h), "x-api-key") {
		t.Fatalf("request body unexpectedly references an api key")
	}
}

func TestQueryDisableThinking(t *testing.T) {
	var gotBody []byte
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("content-type", "application/json")
		io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`)
	}))

	p := NewAnthropic(Config{SocketPath: sock, DisableThinking: true, Model: "claude-sonnet-4-6", MaxTokens: 1024})
	if _, err := p.Query(context.Background(), "x"); err != nil {
		t.Fatalf("Query: %v", err)
	}
	var req messagesRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Thinking != nil {
		t.Fatalf("thinking = %+v, want nil when disabled", req.Thinking)
	}
	if req.Model != "claude-sonnet-4-6" || req.MaxTokens != 1024 {
		t.Fatalf("overrides not applied: model=%q max=%d", req.Model, req.MaxTokens)
	}
}

func TestQueryAPIError(t *testing.T) {
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"bad model"}}`)
	}))

	p := NewAnthropic(Config{SocketPath: sock})
	_, err := p.Query(context.Background(), "x")
	if err == nil {
		t.Fatal("Query: want error, got nil")
	}
	if !strings.Contains(err.Error(), "bad model") || !strings.Contains(err.Error(), "400") {
		t.Fatalf("error = %q, want it to mention status 400 and the API message", err)
	}
}

func TestQueryTransportError(t *testing.T) {
	// Point at a socket that does not exist.
	p := NewAnthropic(Config{SocketPath: filepath.Join(t.TempDir(), "nope.sock")})
	if _, err := p.Query(context.Background(), "x"); err == nil {
		t.Fatal("Query: want transport error, got nil")
	}
}

func TestQuerySendsSystemPrompt(t *testing.T) {
	var gotBody []byte
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("content-type", "application/json")
		io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`)
	}))

	p := NewAnthropic(Config{SocketPath: sock, System: "be terse"})
	if _, err := p.Query(context.Background(), "x"); err != nil {
		t.Fatalf("Query: %v", err)
	}
	var req messagesRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.System != "be terse" {
		t.Fatalf("system = %q, want %q", req.System, "be terse")
	}
}

func TestConverseParsesToolUse(t *testing.T) {
	var gotBody []byte
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("content-type", "application/json")
		io.WriteString(w, `{"stop_reason":"tool_use","content":[`+
			`{"type":"text","text":"let me check"},`+
			`{"type":"tool_use","id":"toolu_1","name":"read_file","input":{"path":"a.txt"}}]}`)
	}))

	p := NewAnthropic(Config{SocketPath: sock})
	turn, err := p.Converse(context.Background(),
		[]Message{UserTextMessage("read a.txt")},
		[]ToolSpec{{Name: "read_file", Description: "read a file", InputSchema: json.RawMessage(`{"type":"object"}`)}})
	if err != nil {
		t.Fatalf("Converse: %v", err)
	}
	if turn.StopReason != "tool_use" {
		t.Fatalf("stop_reason = %q, want tool_use", turn.StopReason)
	}
	if turn.Text != "let me check" {
		t.Fatalf("text = %q, want %q", turn.Text, "let me check")
	}
	if len(turn.ToolCalls) != 1 || turn.ToolCalls[0].Name != "read_file" || turn.ToolCalls[0].ID != "toolu_1" {
		t.Fatalf("tool calls = %+v", turn.ToolCalls)
	}
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(turn.ToolCalls[0].Input, &input); err != nil || input.Path != "a.txt" {
		t.Fatalf("tool input = %s (err %v)", turn.ToolCalls[0].Input, err)
	}
	// The assistant message echoes both the text and the tool_use block, for
	// appending to history before the tool result.
	if len(turn.Assistant.Content) != 2 || turn.Assistant.Role != "assistant" {
		t.Fatalf("assistant message = %+v", turn.Assistant)
	}

	var req messagesRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "read_file" {
		t.Fatalf("tools in request = %+v, want read_file", req.Tools)
	}
	if req.Thinking != nil {
		t.Fatalf("Converse must not enable thinking (tool-use loop), got %+v", req.Thinking)
	}
}
