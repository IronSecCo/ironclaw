// This file wires Groq (https://groq.com) as a first-class provider
// kind. Groq is an OpenAI-wire-compatible provider: it serves chat
// completions under /openai/v1, so it reuses OpenAIProvider unchanged —
// NewOpenAI detects the api.groq.com host and selects the /openai/v1 path.
// The only thing this backend contributes over the raw OpenAI path is
// Groq's distinct default upstream host and model, applied in the
// registered factory below before delegating to NewOpenAI.
//
// It lives in its own file (kind constant + defaults + registration all here) so
// Groq can be added or changed without touching a shared region of
// provider.go — the whole point of the provider registry.

package provider

const (
	KindGroq         = "groq"
	groqUpstreamHost = "api.groq.com"
	defaultGroqModel = "openai/gpt-oss-120b"
)

func init() {
	Register(KindGroq, func(cfg Config) (Provider, error) {
		if cfg.UpstreamHost == "" {
			cfg.UpstreamHost = groqUpstreamHost
		}

		if cfg.Model == "" {
			cfg.Model = defaultGroqModel
		}

		return NewOpenAI(cfg), nil
	})
}
