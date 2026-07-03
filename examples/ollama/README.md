# Ollama (zero-credential local model)

Run a real IronClaw agent against a model on **your own machine**, with **no cloud
API key anywhere in the stack**. This is the lowest-friction path for OSS evaluators,
demos, and CI: [Ollama](https://ollama.com) serves the OpenAI Chat Completions wire
format, and IronClaw's first-class **`ollama`** provider points the sealed sandbox at
it — the host model-proxy forwards to `localhost:11434` over plain HTTP with **no
Authorization header**, because Ollama needs no credential.

## What it configures

- An agent group `local-helper` pinned to `--provider ollama --model llama3.2`.
- Nothing else: no API key is set here or anywhere. The credential-free path is the
  whole point.

Then it sends the agent one chat and prints the reply, proving the round-trip runs
end to end on a local model.

## Prerequisites

1. **Install Ollama and pull a model:**
   ```sh
   ollama pull llama3.2      # or any model; set OLLAMA_MODEL to match
   ```
2. **Start the control-plane with the ollama provider enabled:**
   ```sh
   controlplane --ollama       # or: IRONCLAW_OLLAMA=1 controlplane
   ```
   That allowlists `localhost:11434`, forwards over plain HTTP, and makes ollama the
   deployment-default model. For a non-default port or a remote Ollama, set
   `OLLAMA_HOST` first (e.g. `export OLLAMA_HOST=127.0.0.1:11500` or
   `export OLLAMA_HOST=https://ollama.example.com`).

## Try it

```sh
export IRONCLAW_API_TOKEN=<your control-plane API token>
cd examples/ollama
./setup.sh
```

Override the defaults with env vars: `OLLAMA_MODEL` (must be pulled) and `PROMPT`.

## What to notice

- **No credential, anywhere.** Unlike every cloud provider (`openai`, `bedrock`,
  `azure`, ...), the `ollama` kind holds no key — the sandbox has none and the host
  injects none. The one exception is an Ollama behind an authenticating reverse
  proxy: set `OLLAMA_API_KEY` on the control-plane and it is injected host-side only,
  never entering the sandbox.
- **Same sealed path as any provider.** The agent still runs in a per-session Docker
  sandbox and reaches the model only through the host model-proxy allowlist — the
  isolation guarantees are identical to the hosted providers; only the credential
  handling differs.
- **`ollama` vs `local`.** The older `local` kind (LM Studio / vLLM / llama.cpp) needs
  you to pass a full `--local-model-url`; `ollama` supplies Ollama's defaults
  (`localhost:11434`, a common model) so `--provider ollama` works with nothing else
  set. See [docs/providers/ollama.md](../../docs/providers/ollama.md).
