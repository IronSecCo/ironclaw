// This file wires OpenRouter (https://openrouter.ai) as a first-class provider
// kind. OpenRouter is an OpenAI-wire-compatible aggregator: it serves chat
// completions under /api/v1, so it reuses OpenAIProvider unchanged — NewOpenAI
// detects the openrouter.ai host and selects the /api/v1 path. The only thing this
// backend contributes over the raw OpenAI path is OpenRouter's distinct default
// upstream host and model, applied in the registered factory below before
// delegating to NewOpenAI.
//
// It lives in its own file (kind constant + defaults + registration all here) so
// OpenRouter can be added or changed without touching a shared region of
// provider.go — the whole point of the provider registry.

package provider

// KindOpenRouter selects the OpenRouter aggregator backend.
const (
	KindOpenRouter         = "openrouter"
	openRouterUpstreamHost = "openrouter.ai"
	defaultOpenRouterModel = "openai/gpt-4o"
)

func init() {
	Register(KindOpenRouter, func(cfg Config) (Provider, error) {
		if cfg.UpstreamHost == "" {
			cfg.UpstreamHost = openRouterUpstreamHost
		}
		if cfg.Model == "" {
			cfg.Model = defaultOpenRouterModel
		}
		return NewOpenAI(cfg), nil
	})
}
