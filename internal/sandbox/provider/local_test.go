package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestLocalQuerySuccess exercises the local provider (KindLocal) end to end against
// a fake OpenAI-compatible server on a unix socket — the Ollama/LM Studio/vLLM path.
// It asserts the request keeps the standard Chat Completions path, addresses the
// operator's loopback host (so the model-proxy allowlist matches it), and carries
// no Authorization header (a local server needs no cloud credential — the sandbox
// holds none and the host injects one only if the operator configured a key).
func TestLocalQuerySuccess(t *testing.T) {
	const localHost = "localhost:11434"
	var gotBody []byte
	var gotPath, gotHost, gotAuth string
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHost = r.Host
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		writeSSE(w, chatHelloStream)
	}))

	p, err := New(Config{Kind: KindLocal, SocketPath: sock, UpstreamHost: localHost, Model: "llama3.2"})
	if err != nil {
		t.Fatalf("New(local): %v", err)
	}
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
	if gotHost != localHost {
		t.Fatalf("Host = %q, want %q (proxy allowlists on Host)", gotHost, localHost)
	}
	if gotAuth != "" {
		t.Fatalf("Authorization = %q, want empty (local server, no credential in sandbox)", gotAuth)
	}

	var req oaiChatRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if req.Model != "llama3.2" {
		t.Fatalf("model = %q, want %q", req.Model, "llama3.2")
	}
}

// TestLocalFactory checks the factory wiring for KindLocal: a host is required (no
// silent fallback to api.openai.com), it is case-insensitive, and it builds an
// OpenAIProvider pointed at the operator's loopback URL.
func TestLocalFactory(t *testing.T) {
	if _, err := New(Config{Kind: KindLocal}); err == nil {
		t.Fatal("local without host: want error, got nil")
	}
	pv, err := New(Config{Kind: "Local", UpstreamHost: "127.0.0.1:1234", Model: "qwen2.5"}) // case-insensitive
	if err != nil {
		t.Fatalf("local: %v", err)
	}
	op, ok := pv.(*OpenAIProvider)
	if !ok {
		t.Fatalf("local kind = %T, want *OpenAIProvider", pv)
	}
	if !strings.Contains(op.url, "127.0.0.1:1234/v1/chat/completions") {
		t.Fatalf("local url = %q, want the loopback /v1/chat/completions path", op.url)
	}
}
