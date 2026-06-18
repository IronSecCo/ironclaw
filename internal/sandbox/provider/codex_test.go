package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestNewCodexDefaults(t *testing.T) {
	p := NewCodex(Config{SocketPath: "/x.sock"})
	if p.cfg.UpstreamHost != codexUpstreamHost {
		t.Errorf("UpstreamHost = %q, want %q", p.cfg.UpstreamHost, codexUpstreamHost)
	}
	if p.cfg.Model != defaultCodexModel {
		t.Errorf("Model = %q, want %q", p.cfg.Model, defaultCodexModel)
	}
	if want := "http://" + codexUpstreamHost + codexResponsesPath; p.url != want {
		t.Errorf("url = %q, want %q", p.url, want)
	}
}

func TestNewProviderCodexKind(t *testing.T) {
	p, err := New(Config{Kind: KindCodex, SocketPath: "/x.sock"})
	if err != nil {
		t.Fatalf("New(codex): %v", err)
	}
	if _, ok := p.(*CodexProvider); !ok {
		t.Fatalf("New(codex) = %T, want *CodexProvider", p)
	}
}

func TestCodexQuerySuccess(t *testing.T) {
	var gotPath, gotBeta, gotOrig, gotSession, gotAccept string
	var body map[string]any
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBeta = r.Header.Get("OpenAI-Beta")
		gotOrig = r.Header.Get("originator")
		gotSession = r.Header.Get("session_id")
		gotAccept = r.Header.Get("accept")
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeSSE(w, sse(
			`{"type":"response.created"}`,
			`{"type":"reasoning","delta":"ignored"}`,
			`{"type":"response.output_text.delta","delta":"hello"}`,
			`{"type":"response.output_text.delta","delta":" codex"}`,
			`{"type":"response.completed"}`,
		))
	}))

	p := NewCodex(Config{SocketPath: sock})
	out, err := p.Query(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if out != "hello codex" {
		t.Errorf("out = %q, want %q", out, "hello codex")
	}
	if gotPath != codexResponsesPath {
		t.Errorf("path = %q, want %q", gotPath, codexResponsesPath)
	}
	if gotBeta != codexOpenAIBeta {
		t.Errorf("OpenAI-Beta = %q, want %q", gotBeta, codexOpenAIBeta)
	}
	if gotOrig != codexOriginator {
		t.Errorf("originator = %q, want %q", gotOrig, codexOriginator)
	}
	if gotSession == "" {
		t.Error("session_id header is empty")
	}
	if !strings.Contains(gotAccept, "event-stream") {
		t.Errorf("accept = %q, want text/event-stream", gotAccept)
	}
	if body["model"] != defaultCodexModel {
		t.Errorf("body.model = %v, want %v", body["model"], defaultCodexModel)
	}
	if body["stream"] != true {
		t.Errorf("body.stream = %v, want true", body["stream"])
	}
	if body["store"] != false {
		t.Errorf("body.store = %v, want false", body["store"])
	}
}

func TestCodexQueryError(t *testing.T) {
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"detail":"The 'gpt-9' model is not supported when using Codex with a ChatGPT account."}`)
	}))
	p := NewCodex(Config{SocketPath: sock})
	_, err := p.Query(context.Background(), "hi")
	if err == nil {
		t.Fatal("Query: want error on non-200, got nil")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("err = %v, want the API detail", err)
	}
}

func TestAccumulateCodexSSE(t *testing.T) {
	body := sse(
		`{"type":"response.created"}`,
		`{"type":"response.output_text.delta","delta":"a"}`,
		`{"type":"reasoning"}`,
		`{"type":"response.output_text.delta","delta":"b"}`,
		`{"type":"response.completed"}`,
	)
	got, err := accumulateCodexSSE(strings.NewReader(body))
	if err != nil {
		t.Fatalf("accumulate: %v", err)
	}
	if got != "ab" {
		t.Errorf("got %q, want %q", got, "ab")
	}

	_, err = accumulateCodexSSE(strings.NewReader(sse(`{"type":"error","error":{"message":"boom"}}`)))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("want stream error containing boom, got %v", err)
	}
}

func TestParseCodexError(t *testing.T) {
	if e := parseCodexError(400, []byte(`{"detail":"d-msg"}`)); !strings.Contains(e.Error(), "d-msg") {
		t.Errorf("detail shape: %v", e)
	}
	if e := parseCodexError(401, []byte(`{"error":{"message":"e-msg"}}`)); !strings.Contains(e.Error(), "e-msg") {
		t.Errorf("error.message shape: %v", e)
	}
	if e := parseCodexError(500, []byte(`raw-body`)); !strings.Contains(e.Error(), "raw-body") {
		t.Errorf("raw fallback: %v", e)
	}
}

func TestCodexImplementsToolConverser(t *testing.T) {
	var _ ToolConverser = (*CodexProvider)(nil)
	p, _ := New(Config{Kind: KindCodex, SocketPath: "/x.sock"})
	if _, ok := p.(ToolConverser); !ok {
		t.Fatal("CodexProvider must implement ToolConverser so codex agents can use tools")
	}
}

func TestCodexConverseEmitsToolCall(t *testing.T) {
	var body map[string]any
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeSSE(w, sse(
			`{"type":"response.created"}`,
			`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_1","call_id":"call_abc","name":"web_search","arguments":""}}`,
			`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"query\":"}`,
			`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"\"Linux\"}"}`,
			`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_1","call_id":"call_abc","name":"web_search","arguments":"{\"query\":\"Linux\"}"}}`,
			`{"type":"response.completed"}`,
		))
	}))

	p := NewCodex(Config{SocketPath: sock})
	specs := []ToolSpec{{Name: "web_search", Description: "search", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	turn, err := p.Converse(context.Background(), []Message{UserTextMessage("search Linux")}, specs)
	if err != nil {
		t.Fatalf("Converse: %v", err)
	}
	if len(turn.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %d, want 1", len(turn.ToolCalls))
	}
	tc := turn.ToolCalls[0]
	if tc.Name != "web_search" || tc.ID != "call_abc" {
		t.Errorf("tool call = %+v, want name=web_search id=call_abc", tc)
	}
	if string(tc.Input) != `{"query":"Linux"}` {
		t.Errorf("args = %s, want {\"query\":\"Linux\"}", tc.Input)
	}
	if turn.StopReason != "tool_use" {
		t.Errorf("StopReason = %q, want tool_use", turn.StopReason)
	}
	// The request must offer the tool as a flat function and choose auto.
	tools, _ := body["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("request tools = %v, want 1", body["tools"])
	}
	if tool0, _ := tools[0].(map[string]any); tool0["type"] != "function" || tool0["name"] != "web_search" {
		t.Errorf("request tool = %v, want flat function web_search", tools[0])
	}
	if body["tool_choice"] != "auto" {
		t.Errorf("tool_choice = %v, want auto", body["tool_choice"])
	}
}

func TestCodexConverseSendsToolResultAsInput(t *testing.T) {
	var body map[string]any
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeSSE(w, sse(
			`{"type":"response.output_text.delta","delta":"done"}`,
			`{"type":"response.completed"}`,
		))
	}))

	// History: user asked, assistant called a tool, tool result came back.
	history := []Message{
		UserTextMessage("search Linux"),
		{Role: "assistant", Content: []Block{{Type: "tool_use", ID: "call_abc", Name: "web_search", Input: json.RawMessage(`{"query":"Linux"}`)}}},
		ToolResultsMessage([]ToolResult{{ToolUseID: "call_abc", Content: "RESULT-XYZ"}}),
	}
	p := NewCodex(Config{SocketPath: sock})
	turn, err := p.Converse(context.Background(), history, []ToolSpec{{Name: "web_search"}})
	if err != nil {
		t.Fatalf("Converse: %v", err)
	}
	if turn.Text != "done" {
		t.Errorf("Text = %q, want done", turn.Text)
	}
	input, _ := body["input"].([]any)
	var sawCall, sawOutput bool
	for _, it := range input {
		m, _ := it.(map[string]any)
		switch m["type"] {
		case "function_call":
			if m["call_id"] == "call_abc" && m["name"] == "web_search" {
				sawCall = true
			}
		case "function_call_output":
			if m["call_id"] == "call_abc" && m["output"] == "RESULT-XYZ" {
				sawOutput = true
			}
		}
	}
	if !sawCall {
		t.Errorf("input missing function_call replay: %v", input)
	}
	if !sawOutput {
		t.Errorf("input missing function_call_output: %v", input)
	}
}

func TestAccumulateCodexConverse(t *testing.T) {
	body := sse(
		`{"type":"response.output_text.delta","delta":"thinking "}`,
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_9","call_id":"call_z","name":"read_file","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_9","delta":"{\"path\""}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_9","delta":":\"/x\"}"}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_9","call_id":"call_z","name":"read_file","arguments":"{\"path\":\"/x\"}"}}`,
		`{"type":"response.completed"}`,
	)
	res, err := accumulateCodexConverse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("accumulate: %v", err)
	}
	if res.text != "thinking " {
		t.Errorf("text = %q", res.text)
	}
	if len(res.toolCalls) != 1 || res.toolCalls[0].name != "read_file" || res.toolCalls[0].callID != "call_z" {
		t.Fatalf("toolCalls = %+v", res.toolCalls)
	}
	if res.toolCalls[0].args != `{"path":"/x"}` {
		t.Errorf("args = %q", res.toolCalls[0].args)
	}

	if _, err := accumulateCodexConverse(strings.NewReader(sse(`{"type":"response.failed","response":{"error":{"message":"kaput"}}}`))); err == nil || !strings.Contains(err.Error(), "kaput") {
		t.Errorf("want failed-response error containing kaput, got %v", err)
	}
}

func TestNewSessionID(t *testing.T) {
	id := newSessionID()
	if len(id) != 36 || strings.Count(id, "-") != 4 {
		t.Fatalf("not a uuid: %q", id)
	}
	if id[14] != '4' {
		t.Errorf("version nibble = %c, want 4 (%q)", id[14], id)
	}
	if newSessionID() == id {
		t.Error("two session ids should differ")
	}
}
