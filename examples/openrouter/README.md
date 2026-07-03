# OpenRouter (one key, 100+ models)

Run a real IronClaw agent against **any of 100+ models** — Claude, GPT, Llama,
Mistral, Gemini — through a **single** [OpenRouter](https://openrouter.ai) key. Change
models by changing a `vendor/model` id; nothing else in the stack changes. OpenRouter
serves the OpenAI Chat Completions wire format, and IronClaw's first-class
**`openrouter`** provider points the sealed sandbox at it — the host model-proxy stamps
your key host-side, so the sandbox never sees a secret.

## What it configures

- An agent group `router-helper` pinned to `--provider openrouter --model anthropic/claude-3.5-sonnet`.
- Nothing else: the OpenRouter key lives only in the control-plane environment
  (`OPENROUTER_API_KEY`), never in the script or the sandbox.

Then it sends the agent one chat and prints the reply, proving the round-trip runs end
to end through OpenRouter.

## Try it credential-free first (mock)

Prove the whole sealed path works with **no key at all**:

```sh
export IRONCLAW_API_TOKEN=<your control-plane API token>
cd examples/openrouter
PROVIDER=mock ./setup.sh
```

That pins the group to the offline `mock` provider — inbound → per-session Docker
sandbox → encrypted queue → reply — with zero credentials, then prints the mock reply.

## Try it against OpenRouter

1. **Set your OpenRouter key on the control-plane** (never in the sandbox):
   ```sh
   export OPENROUTER_API_KEY=sk-or-…
   ```
   That allowlists `openrouter.ai` on the model-proxy and injects the key host-side.
2. **Run it:**
   ```sh
   export IRONCLAW_API_TOKEN=<your control-plane API token>
   cd examples/openrouter
   ./setup.sh
   ```

Override the defaults with env vars: `MODEL` (any `vendor/model` at
[openrouter.ai/models](https://openrouter.ai/models)) and `PROMPT`. For example:

```sh
MODEL=meta-llama/llama-3.1-70b-instruct ./setup.sh
MODEL=openai/gpt-4o ./setup.sh
```

## What to notice

- **One integration, many vendors.** The vendor prefix in the `vendor/model` id
  (`anthropic/…`, `openai/…`, `meta-llama/…`) selects the upstream OpenRouter fronts.
  No new host, credential, or wiring per model — just a different id.
- **Key stays host-side.** Unlike the sandbox, which holds nothing, the OpenRouter key
  lives only in the control-plane environment and is stamped onto requests by the
  model-proxy. The injector self-guards on the `openrouter.ai` host, so it can never
  leak to another upstream.
- **Same sealed path as any provider.** The agent still runs in a per-session Docker
  sandbox and reaches the model only through the host model-proxy allowlist — the
  isolation guarantees are identical to every other provider; only the reach differs.

See [docs/providers/openrouter.md](../../docs/providers/openrouter.md) for the full
reference.
