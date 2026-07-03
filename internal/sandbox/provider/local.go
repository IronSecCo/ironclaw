// This file wires KindLocal: a LOCAL, self-hosted OpenAI-compatible model server —
// Ollama (http://localhost:11434/v1), LM Studio, vLLM, or llama.cpp — running on
// the operator's own machine. It speaks the identical Chat Completions wire format
// as KindOpenAI (the same /v1/chat/completions path), so it reuses OpenAIProvider;
// the only differences are that the upstream is the operator's loopback host (set
// host-side; there is no sensible default) and that NO cloud credential is required —
// the host model-proxy forwards to the local server over plain HTTP and injects a
// key only if the operator configured one. This is the "100% local, zero cloud
// credential" path: the model runs on the same box, so no data leaves it. See
// modelproxy.WithInsecureUpstreams.
//
// Because there is no default loopback host, the factory requires cfg.UpstreamHost
// rather than silently falling back to api.openai.com (which would send "local"
// traffic to the cloud). Colocated here so adding the local backend touches no
// shared line in provider.go.

package provider

import "fmt"

// KindLocal selects a local, self-hosted OpenAI-compatible model server.
const KindLocal = "local"

func init() {
	Register(KindLocal, func(cfg Config) (Provider, error) {
		if cfg.UpstreamHost == "" {
			return nil, fmt.Errorf("sandbox/provider: local provider requires an upstream host (set --model-host, e.g. localhost:11434)")
		}
		return NewOpenAI(cfg), nil
	})
}
