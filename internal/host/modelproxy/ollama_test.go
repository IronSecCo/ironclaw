package modelproxy

import (
	"net/http"
	"testing"
)

// TestOllamaInjectorNoKey is the core credential-free guarantee: with no key (the
// default and expected ollama path), the injector never stamps an Authorization
// header, so the proxy forwards to Ollama with no credential at all.
func TestOllamaInjectorNoKey(t *testing.T) {
	req, _ := http.NewRequest("POST", "http://localhost:11434/v1/chat/completions", nil)
	OllamaInjector("localhost:11434", "")("localhost:11434", req)
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization set with empty key: %q, want empty (ollama needs no credential)", got)
	}
}

// TestOllamaInjectorOptionalKey covers the rare Ollama behind an authenticating
// reverse proxy: a host-held key is stamped as Bearer on the matching host (bare host
// also matches) but is never leaked to any other upstream.
func TestOllamaInjectorOptionalKey(t *testing.T) {
	req, _ := http.NewRequest("POST", "http://localhost:11434/v1/chat/completions", nil)
	OllamaInjector("localhost:11434", "sk-gateway")("localhost", req)
	if got := req.Header.Get("Authorization"); got != "Bearer sk-gateway" {
		t.Errorf("Authorization = %q, want Bearer sk-gateway", got)
	}
	// Off-host: never leak the key to another upstream (e.g. a real cloud provider).
	other, _ := http.NewRequest("POST", "http://api.openai.com/v1/chat/completions", nil)
	OllamaInjector("localhost:11434", "sk-gateway")("api.openai.com", other)
	if got := other.Header.Get("Authorization"); got != "" {
		t.Errorf("ollama key leaked off-host: %q", got)
	}
}
