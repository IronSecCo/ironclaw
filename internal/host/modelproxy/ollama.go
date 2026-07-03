package modelproxy

// OllamaInjector returns an Injector for the Ollama provider. Ollama needs NO
// credential — the whole point of the ollama backend is the zero-key local path — so
// with an empty apiKey this is a no-op and the proxy forwards requests to Ollama with
// no Authorization header. A non-empty key covers the rare case of an Ollama reached
// through an authenticating reverse proxy (e.g. a shared/remote Ollama behind a
// gateway that requires a Bearer token); the key then lives only on the host and never
// enters the sandbox.
//
// Ollama is OpenAI-wire-compatible and authenticates (when it does at all) via the
// same Bearer scheme, so this delegates to LocalInjector, which self-guards on the
// exact upstream host and no-ops for every other provider — safe to compose through
// MultiInjector. It is a named alias so the ollama provider's auth path is explicit
// and greppable rather than piggybacking on "local".
func OllamaInjector(host, apiKey string) Injector {
	return LocalInjector(host, apiKey)
}
