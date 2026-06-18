package provider

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// serveUnix starts an HTTP server on a unix socket with the given handler and
// returns the socket path. The server is closed when the test ends.
//
// It uses a SHORT temp dir rather than t.TempDir(): the latter embeds the test
// name in the path, which for long test names overflows the ~104-byte unix socket
// path limit (sun_path) on macOS, failing with "bind: invalid argument".
func serveUnix(t *testing.T, handler http.Handler) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "icsock")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	srv := &http.Server{Handler: handler}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return sock
}

// sse renders a sequence of event data objects as a text/event-stream body. Only
// the data lines matter to the accumulator, so the event: lines are omitted.
func sse(events ...string) string {
	var b strings.Builder
	for _, e := range events {
		b.WriteString("data: ")
		b.WriteString(e)
		b.WriteString("\n\n")
	}
	return b.String()
}

func writeSSE(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, body)
}

// helloWorldStream produces the text "hello world" across two deltas.
var helloWorldStream = sse(
	`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello "}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world"}}`,
	`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
	`{"type":"message_stop"}`,
)

func TestQuerySuccess(t *testing.T) {
	var gotBody []byte
	var gotVersion, gotPath, gotHost string
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVersion = r.Header.Get("anthropic-version")
		gotPath = r.URL.Path
		gotHost = r.Host
		gotBody, _ = io.ReadAll(r.Body)
		writeSSE(w, helloWorldStream)
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
	// Integration: the request must address the real upstream host so the
	// model-proxy's allowlist matches and routes it (not a placeholder host).
	if gotHost != defaultUpstreamHost {
		t.Fatalf("request Host = %q, want %q (proxy allowlists on Host)", gotHost, defaultUpstreamHost)
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
	if !req.Stream {
		t.Fatalf("stream = false, want true")
	}
	if req.Thinking == nil || req.Thinking.Type != "adaptive" {
		t.Fatalf("thinking = %+v, want adaptive", req.Thinking)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" ||
		len(req.Messages[0].Content) != 1 || req.Messages[0].Content[0].Text != "hi there" {
		t.Fatalf("messages = %+v, want one user text message %q", req.Messages, "hi there")
	}
	if strings.Contains(strings.ToLower(string(gotBody)), "x-api-key") {
		t.Fatalf("request body unexpectedly references an api key")
	}
}

func TestQueryUpstreamHostOverride(t *testing.T) {
	var gotHost string
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		writeSSE(w, helloWorldStream)
	}))
	p := NewAnthropic(Config{SocketPath: sock, UpstreamHost: "models.example.test"})
	if _, err := p.Query(context.Background(), "x"); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if gotHost != "models.example.test" {
		t.Fatalf("Host = %q, want override models.example.test", gotHost)
	}
}

func TestQueryDisableThinking(t *testing.T) {
	var gotBody []byte
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		writeSSE(w, sse(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`))
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

func TestQuerySendsSystemPrompt(t *testing.T) {
	var gotBody []byte
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		writeSSE(w, sse(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`))
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
		writeSSE(w, sse(
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"let me check"}}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"read_file"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"a.txt\"}"}}`,
			`{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`,
			`{"type":"message_stop"}`,
		))
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
		t.Fatalf("reassembled tool input = %s (err %v)", turn.ToolCalls[0].Input, err)
	}
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

// TestAccumulateSSE exercises the stream reducer directly: text concatenation,
// stop reason, and stream-error handling.
func TestAccumulateSSE(t *testing.T) {
	mr, err := accumulateSSE(strings.NewReader(sse(
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
	)))
	if err != nil {
		t.Fatalf("accumulateSSE: %v", err)
	}
	if extractText(mr) != "hi" || mr.StopReason != "end_turn" {
		t.Fatalf("accumulated = %+v", mr)
	}

	if _, err := accumulateSSE(strings.NewReader(sse(
		`{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`,
	))); err == nil {
		t.Fatal("expected an error from a stream error event")
	}
}

func TestQueryAPIError(t *testing.T) {
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"type":"error","error":{"type":"permission_error","message":"destination not on allowlist"}}`)
	}))

	p := NewAnthropic(Config{SocketPath: sock})
	_, err := p.Query(context.Background(), "x")
	if err == nil {
		t.Fatal("Query: want error, got nil")
	}
	if !strings.Contains(err.Error(), "allowlist") || !strings.Contains(err.Error(), "403") {
		t.Fatalf("error = %q, want it to mention status 403 and the API message", err)
	}
}

func TestQueryTransportError(t *testing.T) {
	p := NewAnthropic(Config{SocketPath: filepath.Join(t.TempDir(), "nope.sock")})
	if _, err := p.Query(context.Background(), "x"); err == nil {
		t.Fatal("Query: want transport error, got nil")
	}
}
