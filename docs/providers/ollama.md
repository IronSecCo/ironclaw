---
title: "Ollama (zero-credential local model)"
description: Run IronClaw against a model on your own machine with no cloud API key. First-class ollama provider, OLLAMA_HOST config, per-group setup, and how the credential-free path stays sealed.
---

# Ollama provider

**`ollama`** is IronClaw's lowest-friction, **zero-credential** backend: point the
sandbox at a model running on your own machine and there is **no cloud API key
anywhere in the stack**. It is the easiest way to evaluate IronClaw, the natural fit
for demos and CI, and the only provider where "where does the credential come from"
has the answer *nowhere*.

[Ollama](https://ollama.com) serves the **identical OpenAI Chat Completions wire
format** (`POST /v1/chat/completions`) as the hosted `openai` provider, so IronClaw
reuses the same request/streaming path. The `ollama` kind only adds the ergonomic
defaults that make it zero-config.

!!! abstract "The isolation invariant still holds"
    Choosing `ollama` changes *where the model runs*, not *how the agent is sealed*.
    The agent still runs in a per-session Docker sandbox and can reach the model only
    through the host model-proxy allowlist. The difference from a cloud provider is
    that there is no secret to hold host-side — see
    [Choose your model provider](index.md) and
    [Security and isolation](../security-isolation.md).

## Quick start

1. **Install Ollama and pull a model:**
   ```sh
   ollama pull llama3.2
   ```
2. **Start the control-plane with the ollama provider:**
   ```sh
   controlplane --ollama            # or: IRONCLAW_OLLAMA=1 controlplane
   ```
3. **Chat.** With `--ollama` set, ollama is the deployment-default model, so a
   provider-less agent group runs fully local. To pin a specific group to it:
   ```sh
   ironctl agent create --yes --id local-helper --name "Local Helper" \
     --provider ollama --model llama3.2
   ```

A complete runnable version — create the group, send a chat, print the reply — is in
[`examples/ollama/`](https://github.com/IronSecCo/ironclaw/tree/main/examples/ollama).

## Configuration

| What | Flag / env | Default | Notes |
|---|---|---|---|
| Enable the provider | `--ollama` / `IRONCLAW_OLLAMA=1` | off | Opt in. Makes ollama the deployment default and allowlists its host. |
| Deployment-default model | `--ollama-model` / `IRONCLAW_OLLAMA_MODEL` | `llama3.2` | Must be pulled (`ollama pull <model>`). |
| Ollama location | `OLLAMA_HOST` | `localhost:11434` | Bare host, `host:port`, or a full `http(s)://` URL. `https://` forwards over TLS; everything else over plain HTTP. |
| Optional gateway key | `OLLAMA_API_KEY` | unset | Only for an Ollama behind an authenticating reverse proxy. Injected **host-side**; never enters the sandbox. |

`OLLAMA_HOST` follows Ollama's own conventions:

```sh
export OLLAMA_HOST=127.0.0.1:11500              # non-default port
export OLLAMA_HOST=http://192.168.1.9:11434     # another machine, plain HTTP
export OLLAMA_HOST=https://ollama.example.com   # remote Ollama behind TLS (port 11434 assumed)
```

The control-plane allowlists exactly that host on the model-proxy, so nothing else
the sandbox tries to reach will resolve.

### Per-group selection

Any agent group can pin `--provider ollama` independently of the deployment default.
A group pinned to `ollama` with no host of its own inherits the deployment's
configured Ollama host, so a provider-only pin still reaches the right server.

## How it stays credential-free

- The **sandbox** builds a bare Chat Completions request with no `Authorization`
  header. It holds no key because none exists.
- The **host model-proxy** forwards to Ollama as-is. With no `OLLAMA_API_KEY` set the
  injector is a no-op — the request goes out with no credential at all.
- If you *do* set `OLLAMA_API_KEY` (an Ollama fronted by an auth gateway), the key is
  stamped as a `Bearer` header **only on the allowlisted Ollama host** and never
  crosses into the sandbox. It self-guards so it can never leak to another upstream.

## `ollama` vs `local`

Both run a self-hosted, OpenAI-compatible model with no cloud key. The difference is
ergonomics:

- **`ollama`** knows Ollama's defaults (`localhost:11434`, a common model), so
  `--provider ollama` works with nothing else configured.
- **`local`** (LM Studio, vLLM, llama.cpp) is the generic path: you supply the full
  endpoint with `--local-model-url`. Use it for any other OpenAI-compatible server.
  See the [100% local model tutorial](../tutorials/local-model-ollama.md).

`--ollama` is ignored if `--local-model-url` is also set — run one local provider at a
time; the explicit URL wins.

## Docker / Compose

When the control-plane runs in a container and Ollama runs on the host, point
`OLLAMA_HOST` at the host gateway:

```sh
export OLLAMA_HOST=http://host.docker.internal:11434
```

That host is allowlisted the same way; the request still carries no credential.

## Troubleshooting

- **No reply / times out.** Confirm `ollama pull <model>` ran and `ollama list` shows
  it, and that the control-plane logged `ollama enabled`. The first token is slow
  while Ollama loads the model into memory.
- **`--ollama` seems ignored.** It is skipped when `--local-model-url` is set, and it
  does not override a group that pins a *different* explicit provider.
- **Wrong host.** The proxy allowlists exactly the resolved `OLLAMA_HOST`; a mismatch
  (e.g. `localhost` vs `127.0.0.1`, or a missing port) will not resolve. The log line
  `ollama enabled host=...` shows the resolved value.
