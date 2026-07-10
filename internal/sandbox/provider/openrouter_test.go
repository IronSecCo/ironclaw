package provider

import (
	"strings"
	"testing"
)

func TestOpenRouterFactoryDefaults(t *testing.T) {
	pv, err := New(Config{Kind: KindOpenRouter})
	if err != nil {
		t.Fatalf("openrouter with no host/model: want defaults, got error: %v", err)
	}
	if pv == nil {
		t.Fatal("openrouter provider = nil, want non-nil")
	}
	op, ok := pv.(*OpenAIProvider)
	if !ok {
		t.Fatalf("openrouter kind = %T, want *OpenAIProvider", pv)
	}
	if op.cfg.UpstreamHost != openRouterUpstreamHost {
		t.Fatalf("openrouter default upstream host = %q, want %q", op.cfg.UpstreamHost, openRouterUpstreamHost)
	}
	if op.cfg.Model != defaultOpenRouterModel {
		t.Fatalf("openrouter default model = %q, want %q", op.cfg.Model, defaultOpenRouterModel)
	}
	if !strings.Contains(op.url, openRouterUpstreamHost+"/api/v1/chat/completions") {
		t.Fatalf("openrouter url = %q, want the %s /api/v1 path", op.url, openRouterUpstreamHost)
	}
}

func TestOpenRouterFactoryOverrides(t *testing.T) {
	const host = "gateway.example.test"
	const model = "meta-llama/llama-3.1-8b-instruct"

	pv, err := New(Config{Kind: "OpenRouter", UpstreamHost: host, Model: model})
	if err != nil {
		t.Fatalf("openrouter override: %v", err)
	}
	op, ok := pv.(*OpenAIProvider)
	if !ok {
		t.Fatalf("openrouter kind = %T, want *OpenAIProvider", pv)
	}
	if op.cfg.UpstreamHost != host {
		t.Fatalf("openrouter upstream host = %q, want overridden %q", op.cfg.UpstreamHost, host)
	}
	if op.cfg.Model != model {
		t.Fatalf("openrouter model = %q, want overridden %q", op.cfg.Model, model)
	}
	if !strings.Contains(op.url, host+"/v1/chat/completions") {
		t.Fatalf("openrouter override url = %q, want the overridden host standard /v1 path", op.url)
	}
}
