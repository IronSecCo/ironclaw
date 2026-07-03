package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestBedrockQuerySuccess checks the Bedrock InvokeModel envelope: the model id
// rides in the path, the body carries anthropic_version and NO model field, and the
// non-streaming Anthropic Messages response is decoded to its text.
func TestBedrockQuerySuccess(t *testing.T) {
	var gotBody []byte
	var gotPath, gotHost, gotCT string
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHost = r.Host
		gotCT = r.Header.Get("content-type")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"content":[{"type":"text","text":"hello world"}],"stop_reason":"end_turn"}`)
	}))

	p, err := NewBedrock(Config{
		SocketPath:   sock,
		UpstreamHost: "bedrock-runtime.us-east-1.amazonaws.com",
		Model:        "anthropic.claude-3-5-sonnet-20241022-v2:0",
	})
	if err != nil {
		t.Fatalf("NewBedrock: %v", err)
	}
	out, err := p.Query(context.Background(), "hi there")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if out != "hello world" {
		t.Fatalf("Query output = %q, want %q", out, "hello world")
	}
	// Model id rides in the path (colon preserved literal); host is the regional
	// bedrock-runtime endpoint.
	if want := "/model/anthropic.claude-3-5-sonnet-20241022-v2:0/invoke"; gotPath != want {
		t.Fatalf("path = %q, want %q", gotPath, want)
	}
	if gotHost != "bedrock-runtime.us-east-1.amazonaws.com" {
		t.Fatalf("Host = %q, want the regional bedrock host", gotHost)
	}
	if gotCT != "application/json" {
		t.Fatalf("content-type = %q, want application/json", gotCT)
	}
	// The sandbox never sends AWS auth — that is added host-side by the injector.
	// Body carries the Bedrock schema marker and no "model"/"stream" fields.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(gotBody, &raw); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if _, ok := raw["model"]; ok {
		t.Fatalf("request body carried a model field; Bedrock takes the model from the URL")
	}
	if _, ok := raw["stream"]; ok {
		t.Fatalf("request body carried a stream field; InvokeModel is non-streaming here")
	}
	var av string
	if err := json.Unmarshal(raw["anthropic_version"], &av); err != nil || av != bedrockAnthropicVersion {
		t.Fatalf("anthropic_version = %q (err %v), want %q", av, err, bedrockAnthropicVersion)
	}
}

// TestBedrockConverseToolUse checks the tool path reuses the shared Messages wire
// format: a tool offer goes out and a tool_use content block comes back as a ToolCall.
func TestBedrockConverseToolUse(t *testing.T) {
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"content":[{"type":"tool_use","id":"tu_1","name":"read_file","input":{"path":"a.txt"}}],"stop_reason":"tool_use"}`)
	}))
	p, err := NewBedrock(Config{SocketPath: sock, UpstreamHost: "bedrock-runtime.eu-west-1.amazonaws.com"})
	if err != nil {
		t.Fatalf("NewBedrock: %v", err)
	}
	turn, err := p.Converse(context.Background(),
		[]Message{UserTextMessage("read a.txt")},
		[]ToolSpec{{Name: "read_file", InputSchema: json.RawMessage(`{"type":"object"}`)}})
	if err != nil {
		t.Fatalf("Converse: %v", err)
	}
	if turn.StopReason != "tool_use" || len(turn.ToolCalls) != 1 || turn.ToolCalls[0].Name != "read_file" {
		t.Fatalf("turn = %+v, want a single read_file tool call", turn)
	}
	if turn.ToolCalls[0].ID != "tu_1" || !strings.Contains(string(turn.ToolCalls[0].Input), `"a.txt"`) {
		t.Fatalf("tool call = %+v, want id tu_1 with the input echoed", turn.ToolCalls[0])
	}
}

// TestBedrockError decodes the Bedrock error envelope (top-level "message").
func TestBedrockError(t *testing.T) {
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"message":"The security token included in the request is invalid."}`)
	}))
	p, err := NewBedrock(Config{SocketPath: sock, UpstreamHost: "bedrock-runtime.us-east-1.amazonaws.com"})
	if err != nil {
		t.Fatalf("NewBedrock: %v", err)
	}
	_, err = p.Query(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "security token") {
		t.Fatalf("Query error = %v, want it to surface the Bedrock message", err)
	}
}

// TestBedrockRequiresHost checks the factory rejects an empty host (the SigV4
// signature is region-bound, so there is no safe default).
func TestBedrockRequiresHost(t *testing.T) {
	if _, err := NewBedrock(Config{SocketPath: "/x.sock"}); err == nil {
		t.Fatal("NewBedrock with no host succeeded, want an error")
	}
	if _, err := New(Config{Kind: "bedrock"}); err == nil {
		t.Fatal("New(Kind:bedrock) with no host succeeded, want an error")
	}
}

// TestBedrockFactory checks New(Kind:"bedrock") builds a BedrockProvider with the
// model id in the path and applies the default model.
func TestBedrockFactory(t *testing.T) {
	pv, err := New(Config{Kind: "Bedrock", UpstreamHost: "bedrock-runtime.ap-southeast-2.amazonaws.com"})
	if err != nil {
		t.Fatalf("bedrock: %v", err)
	}
	bp, ok := pv.(*BedrockProvider)
	if !ok {
		t.Fatalf("bedrock kind = %T, want *BedrockProvider", pv)
	}
	want := "http://bedrock-runtime.ap-southeast-2.amazonaws.com/model/" + defaultBedrockModel + "/invoke"
	if bp.url != want {
		t.Fatalf("bedrock url = %q, want %q", bp.url, want)
	}
}
