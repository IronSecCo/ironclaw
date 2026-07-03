// This file adds Ollama as a first-class, zero-credential provider so anyone can run
// IronClaw against a model on their own machine with NO API key at all — the lowest-
// friction path for OSS evaluators, demos, and CI. Ollama serves the IDENTICAL
// OpenAI Chat Completions wire format (POST /v1/chat/completions) as KindOpenAI, so
// this file does NOT fork the translation or streaming logic: NewOllama returns a
// *OpenAIProvider whose only differences are ergonomic defaults —
//
//   - Host: defaults to the Ollama loopback default (localhost:11434) when cfg leaves
//     it empty, so `--provider ollama` works with no other configuration. The
//     control-plane overrides it from OLLAMA_HOST for a non-default port or a remote
//     Ollama. The proxy reaches it over plain HTTP (Ollama serves no TLS on loopback)
//     and allowlists only that host.
//   - Model: defaults to a common small local model when cfg leaves it empty. Operators
//     override it with --model (the model must be pulled: `ollama pull <model>`).
//   - Auth: NONE. Ollama needs no credential; the sandbox holds none and the host
//     model-proxy forwards with no Authorization header. An OPTIONAL key is injected
//     host-side only for an Ollama behind an authenticating reverse proxy
//     (modelproxy.OllamaInjector) — never by the sandbox.

package provider

// KindOllama routes to Ollama (https://ollama.com), the popular self-hosted model
// runner, at its loopback default localhost:11434. It is the lowest-friction,
// ZERO-CREDENTIAL path: no cloud key at all, ideal for OSS evaluators, demos, and CI.
// Ollama serves the identical OpenAI Chat Completions wire format as KindOpenAI (POST
// /v1/chat/completions), so it reuses OpenAIProvider; unlike KindLocal it supplies
// zero-config defaults (the localhost:11434 host and a common local model) so
// `--provider ollama` works with nothing else set. The control-plane backfills the
// host from OLLAMA_HOST for a non-default port or a remote Ollama, allowlists only that
// host, and forwards over plain HTTP. See NewOllama and modelproxy.OllamaInjector.
const KindOllama = "ollama"

func init() {
	Register(KindOllama, func(cfg Config) (Provider, error) { return NewOllama(cfg), nil })
}

// defaultOllamaHost is the host:port the Ollama provider allowlists and dials when
// cfg.UpstreamHost is empty. It is Ollama's own default bind address, so `--provider
// ollama` with nothing else reaches a stock local Ollama. The control-plane backfills
// a different host from OLLAMA_HOST.
const defaultOllamaHost = "localhost:11434"

// defaultOllamaModel is the model id used when cfg.Model is empty. It targets a small,
// widely-pulled local model so the credential-free quickstart works after a single
// `ollama pull`. Operators override it with --model.
const defaultOllamaModel = "llama3.2"

// NewOllama constructs an Ollama backend, reusing OpenAIProvider unchanged (the wire
// format is identical) and only supplying Ollama's zero-config defaults: the
// localhost:11434 loopback host and a common local model when cfg leaves them empty.
// No credential is required or held — Ollama needs none; the host model-proxy forwards
// over plain HTTP and injects a key only if the operator configured one for a guarded
// reverse proxy. Callers usually go through New.
func NewOllama(cfg Config) *OpenAIProvider {
	if cfg.UpstreamHost == "" {
		cfg.UpstreamHost = defaultOllamaHost
	}
	if cfg.Model == "" {
		cfg.Model = defaultOllamaModel
	}
	// NewOpenAI fills the remaining defaults (socket, max tokens, timeout) and builds
	// the http://<host>/v1/chat/completions URL that Ollama's OpenAI-compat API serves.
	return NewOpenAI(cfg)
}
