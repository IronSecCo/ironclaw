package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// geminiHelloStream produces "hello world" across two content parts, then stops.
var geminiHelloStream = sse(
	`{"candidates":[{"content":{"role":"model","parts":[{"text":"hello "}]}}]}`,
	`{"candidates":[{"content":{"role":"model","parts":[{"text":"world"}]},"finishReason":"STOP"}]}`,
)

func TestGeminiQuerySuccess(t *testing.T) {
	var gotBody []byte
	var gotPath, gotHost string
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHost = r.Host
		gotBody, _ = io.ReadAll(r.Body)
		writeSSE(w, geminiHelloStream)
	}))

	p := NewGemini(Config{SocketPath: sock})
	out, err := p.Query(context.Background(), "hi there")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if out != "hello world" {
		t.Fatalf("Query output = %q, want %q", out, "hello world")
	}
	// The model id rides in the path; the proxy allowlists on Host.
	if !strings.Contains(gotPath, defaultGeminiModel) || !strings.Contains(gotPath, ":streamGenerateContent") {
		t.Fatalf("path = %q, want it to carry the model and :streamGenerateContent", gotPath)
	}
	if gotHost != geminiUpstreamHost {
		t.Fatalf("Host = %q, want %q (proxy allowlists on Host)", gotHost, geminiUpstreamHost)
	}

	var req gemRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if len(req.Contents) != 1 || req.Contents[0].Role != "user" ||
		len(req.Contents[0].Parts) != 1 || req.Contents[0].Parts[0].Text != "hi there" {
		t.Fatalf("contents = %+v, want one user text part %q", req.Contents, "hi there")
	}
}

func TestGeminiSystem(t *testing.T) {
	var gotBody []byte
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		writeSSE(w, geminiHelloStream)
	}))
	p := NewGemini(Config{SocketPath: sock, System: "be terse"})
	if _, err := p.Query(context.Background(), "x"); err != nil {
		t.Fatalf("Query: %v", err)
	}
	var req gemRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.SystemInstruction == nil || len(req.SystemInstruction.Parts) != 1 ||
		req.SystemInstruction.Parts[0].Text != "be terse" {
		t.Fatalf("systemInstruction = %+v, want a single 'be terse' part", req.SystemInstruction)
	}
}

func TestGeminiTools(t *testing.T) {
	var gotBody []byte
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		writeSSE(w, sse(
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"let me check"}]}}]}`,
			`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"read_file","args":{"path":"a.txt"}}}]},"finishReason":"STOP"}]}`,
		))
	}))

	p := NewGemini(Config{SocketPath: sock})
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
		t.Fatalf("stop reason = %q, want tool_use (function call present)", turn.StopReason)
	}
	if len(turn.ToolCalls) != 1 || turn.ToolCalls[0].Name != "read_file" {
		t.Fatalf("tool calls = %+v", turn.ToolCalls)
	}
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(turn.ToolCalls[0].Input, &input); err != nil || input.Path != "a.txt" {
		t.Fatalf("tool input = %s (err %v)", turn.ToolCalls[0].Input, err)
	}
	// The assistant message must round-trip back as Anthropic-shaped blocks.
	if len(turn.Assistant.Content) != 2 || turn.Assistant.Content[1].Type != "tool_use" {
		t.Fatalf("assistant content = %+v", turn.Assistant.Content)
	}

	// The request must carry the tool in Gemini functionDeclarations shape.
	var req gemRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if len(req.Tools) != 1 || len(req.Tools[0].FunctionDeclarations) != 1 ||
		req.Tools[0].FunctionDeclarations[0].Name != "read_file" {
		t.Fatalf("tools in request = %+v", req.Tools)
	}
}

// TestToGeminiContents checks the translation of an Anthropic-shaped tool-use round
// (assistant tool_use → user tool_result) into Gemini's role:"model" functionCall +
// role:"user" functionResponse shape, with the function name recovered by id.
func TestToGeminiContents(t *testing.T) {
	history := []Message{
		UserTextMessage("read a.txt"),
		{Role: "assistant", Content: []Block{
			{Type: "text", Text: "checking"},
			{Type: "tool_use", ID: "call_0", Name: "read_file", Input: json.RawMessage(`{"path":"a.txt"}`)},
		}},
		ToolResultsMessage([]ToolResult{{ToolUseID: "call_0", Content: "file body"}}),
	}
	contents := toGeminiContents(history)
	if len(contents) != 3 {
		t.Fatalf("got %d contents, want 3: %+v", len(contents), contents)
	}
	if contents[0].Role != "user" || len(contents[0].Parts) != 1 || contents[0].Parts[0].Text != "read a.txt" {
		t.Fatalf("contents[0] = %+v", contents[0])
	}
	if contents[1].Role != "model" || len(contents[1].Parts) != 2 ||
		contents[1].Parts[1].FunctionCall == nil || contents[1].Parts[1].FunctionCall.Name != "read_file" {
		t.Fatalf("contents[1] = %+v", contents[1])
	}
	fr := contents[2].Parts[0].FunctionResponse
	if contents[2].Role != "user" || fr == nil || fr.Name != "read_file" {
		t.Fatalf("contents[2] = %+v, want a functionResponse named read_file", contents[2])
	}
	// The result string must be wrapped in the required {output: ...} struct.
	var resp struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal(fr.Response, &resp); err != nil || resp.Output != "file body" {
		t.Fatalf("functionResponse.response = %s (err %v)", fr.Response, err)
	}
}

func TestGeminiError(t *testing.T) {
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"code":400,"message":"API key not valid","status":"INVALID_ARGUMENT"}}`)
	}))
	p := NewGemini(Config{SocketPath: sock})
	_, err := p.Query(context.Background(), "x")
	if err == nil {
		t.Fatal("Query: want error, got nil")
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "API key not valid") {
		t.Fatalf("error = %q, want it to mention 400 and the API message", err)
	}
}

func TestGeminiFactory(t *testing.T) {
	pv, err := New(Config{Kind: "Gemini"}) // case-insensitive
	if err != nil {
		t.Fatalf("gemini: %v", err)
	}
	gp, ok := pv.(*GeminiProvider)
	if !ok {
		t.Fatalf("gemini kind = %T, want *GeminiProvider", pv)
	}
	if !strings.Contains(gp.url, geminiUpstreamHost) || !strings.Contains(gp.url, ":streamGenerateContent?alt=sse") {
		t.Fatalf("gemini url = %q, want the AI Studio host and SSE streaming path", gp.url)
	}
}
