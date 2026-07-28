package provider

import (
	"strings"
	"testing"
)

func TestGroqFactoryDefaults(t *testing.T) {
	pv, err := New(Config{Kind: KindGroq})
	if err != nil {
		t.Fatalf("groq with no host/model: want defaults, got error: %v", err)
	}
	if pv == nil {
		t.Fatal("groq provider = nil, want non-nil")
	}
	op, ok := pv.(*OpenAIProvider)
	if !ok {
		t.Fatalf("groq kind = %T, want *OpenAIProvider", pv)
	}
	if op.cfg.UpstreamHost != groqUpstreamHost {
		t.Fatalf("groq default upstream host = %q, want %q", op.cfg.UpstreamHost, groqUpstreamHost)
	}
	if op.cfg.Model != defaultGroqModel {
		t.Fatalf("groq default model = %q, want %q", op.cfg.Model, defaultGroqModel)
	}
	// Groq uses /openai/v1 path
	if !strings.Contains(op.url, groqUpstreamHost+"/openai/v1/chat/completions") {
		t.Fatalf("groq url = %q, want the %s /openai/v1 path", op.url, groqUpstreamHost)
	}
}

func TestGroqFactoryOverrides(t *testing.T) {
	const host = "gateway.example.test"
	const model = "meta-llama/llama-3.1-8b-instruct"

	pv, err := New(Config{Kind: "Groq", UpstreamHost: host, Model: model})
	if err != nil {
		t.Fatalf("groq override: %v", err)
	}
	op, ok := pv.(*OpenAIProvider)
	if !ok {
		t.Fatalf("groq kind = %T, want *OpenAIProvider", pv)
	}
	if op.cfg.UpstreamHost != host {
		t.Fatalf("groq upstream host = %q, want overridden %q", op.cfg.UpstreamHost, host)
	}
	if op.cfg.Model != model {
		t.Fatalf("groq model = %q, want overridden %q", op.cfg.Model, model)
	}
	// When overridden, uses standard /v1 path (OpenAI default)
	if !strings.Contains(op.url, host+"/v1/chat/completions") {
		t.Fatalf("groq override url = %q, want the overridden host standard /v1 path", op.url)
	}
}

func TestGroqFactoryHostOnlyOverride(t *testing.T) {
	const host = "gateway.example.test"

	pv, err := New(Config{Kind: KindGroq, UpstreamHost: host})
	if err != nil {
		t.Fatalf("groq host-only override: %v", err)
	}
	op, ok := pv.(*OpenAIProvider)
	if !ok {
		t.Fatalf("groq kind = %T, want *OpenAIProvider", pv)
	}
	if op.cfg.UpstreamHost != host {
		t.Fatalf("groq upstream host = %q, want overridden %q", op.cfg.UpstreamHost, host)
	}
	if op.cfg.Model != defaultGroqModel {
		t.Fatalf("groq model = %q, want default %q", op.cfg.Model, defaultGroqModel)
	}
	if !strings.Contains(op.url, host+"/v1/chat/completions") {
		t.Fatalf("groq host-only override url = %q, want the overridden host standard /v1 path", op.url)
	}
}

func TestGroqFactoryModelOnlyOverride(t *testing.T) {
	const model = "meta-llama/llama-3.1-8b-instruct"

	pv, err := New(Config{Kind: KindGroq, Model: model})
	if err != nil {
		t.Fatalf("groq model-only override: %v", err)
	}
	op, ok := pv.(*OpenAIProvider)
	if !ok {
		t.Fatalf("groq kind = %T, want *OpenAIProvider", pv)
	}
	if op.cfg.UpstreamHost != groqUpstreamHost {
		t.Fatalf("groq upstream host = %q, want default %q", op.cfg.UpstreamHost, groqUpstreamHost)
	}
	if op.cfg.Model != model {
		t.Fatalf("groq model = %q, want overridden %q", op.cfg.Model, model)
	}
	// Groq uses /openai/v1 path
	if !strings.Contains(op.url, groqUpstreamHost+"/openai/v1/chat/completions") {
		t.Fatalf("groq model-only override url = %q, want the %s /openai/v1 path", op.url, groqUpstreamHost)
	}
}

func TestGroqFactoryRegistered(t *testing.T) {
	pv, err := New(Config{Kind: KindGroq})
	if err != nil {
		t.Fatalf("groq registry discovery: %v", err)
	}
	if pv == nil {
		t.Fatal("groq registry discovery = nil, want non-nil provider")
	}
}
