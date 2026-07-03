package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestOllamaQuerySuccess exercises the ollama provider (KindOllama) end to end against
// a fake OpenAI-compatible server on a unix socket. Ollama serves the IDENTICAL Chat
// Completions wire format, so this asserts the request keeps the standard path,
// addresses the configured host (so the model-proxy allowlist matches), and carries NO
// Authorization header — the whole point of the ollama backend is the zero-credential
// local path: the sandbox holds no key and the host injects none by default.
func TestOllamaQuerySuccess(t *testing.T) {
	const ollamaHost = "localhost:11434"
	var gotBody []byte
	var gotPath, gotHost, gotAuth string
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHost = r.Host
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		writeSSE(w, chatHelloStream)
	}))

	p, err := New(Config{Kind: KindOllama, SocketPath: sock, UpstreamHost: ollamaHost, Model: "llama3.2"})
	if err != nil {
		t.Fatalf("New(ollama): %v", err)
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
	if gotHost != ollamaHost {
		t.Fatalf("Host = %q, want %q (proxy allowlists on Host)", gotHost, ollamaHost)
	}
	if gotAuth != "" {
		t.Fatalf("Authorization = %q, want empty (ollama needs no credential)", gotAuth)
	}

	var req oaiChatRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if req.Model != "llama3.2" {
		t.Fatalf("model = %q, want %q", req.Model, "llama3.2")
	}
}

// TestOllamaFactoryDefaults verifies the zero-config ergonomics that distinguish
// KindOllama from KindLocal: with NO host and NO model set, the factory backfills
// Ollama's loopback default (localhost:11434) and a common local model, and builds an
// OpenAIProvider pointed at that loopback /v1/chat/completions URL. This is what makes
// `--provider ollama` work with nothing else configured.
func TestOllamaFactoryDefaults(t *testing.T) {
	pv, err := New(Config{Kind: KindOllama})
	if err != nil {
		t.Fatalf("ollama with no host/model: want defaults, got error: %v", err)
	}
	op, ok := pv.(*OpenAIProvider)
	if !ok {
		t.Fatalf("ollama kind = %T, want *OpenAIProvider", pv)
	}
	if !strings.Contains(op.url, defaultOllamaHost+"/v1/chat/completions") {
		t.Fatalf("ollama url = %q, want the %s loopback path", op.url, defaultOllamaHost)
	}
	if op.cfg.Model != defaultOllamaModel {
		t.Fatalf("ollama default model = %q, want %q", op.cfg.Model, defaultOllamaModel)
	}
}

// TestOllamaFactoryOverrides checks that explicit host/model win over the defaults
// (a non-default port or a remote Ollama backfilled from OLLAMA_HOST by the control
// plane), that the kind is case-insensitive, and that no credential is ever required.
func TestOllamaFactoryOverrides(t *testing.T) {
	pv, err := New(Config{Kind: "Ollama", UpstreamHost: "192.168.1.9:11434", Model: "qwen2.5"}) // case-insensitive
	if err != nil {
		t.Fatalf("ollama override: %v", err)
	}
	op := pv.(*OpenAIProvider)
	if !strings.Contains(op.url, "192.168.1.9:11434/v1/chat/completions") {
		t.Fatalf("ollama url = %q, want the overridden host path", op.url)
	}
	if op.cfg.Model != "qwen2.5" {
		t.Fatalf("ollama model = %q, want overridden qwen2.5", op.cfg.Model)
	}
}
