---
title: "Run Llama locally with Ollama (zero cloud credentials)"
description: Run Llama and other local models with Ollama behind IronClaw, entirely on your own machine with zero cloud API keys. The agent runs sealed and the model never leaves your box. Copy-paste setup.
---

# Run Llama locally with Ollama

You want to run **Llama** (or any open model) with **no cloud provider and no API
key**, and still run your agent boxed off from the rest of your machine. IronClaw's
`local` provider points at any OpenAI-compatible endpoint, so you can serve Llama
from **Ollama** on your own hardware and drive it through a sealed per-session
sandbox. No credential exists anywhere in the stack.

!!! abstract "Zero credential, still sealed"
    Ollama serves the OpenAI-compatible API at `http://localhost:11434/v1`. The host
    **model-proxy** allowlists that loopback host and forwards to it; the sandbox
    stays `network=none` and reaches the model only through the proxy socket, exactly
    as it does for a cloud provider. The difference is there is no key to hold. See
    [Security and isolation](../security-isolation.md).

The same steps work for **LM Studio**, **vLLM**, and **llama.cpp** - they all expose
the OpenAI `/v1` Chat Completions API.

## 1. Pull a model with Ollama

```sh
ollama pull llama3.2          # ~2 GB; any chat model works (qwen2.5, mistral, …)
ollama serve &                # if it isn't already running as a service
curl -s http://localhost:11434/v1/models   # sanity check: the OpenAI-compatible API is up
```

## 2. Point IronClaw at the local model host-side

There is no `ANTHROPIC_API_KEY` in this posture. Set the local model URL and name in
the **control-plane** environment:

```sh
export IRONCLAW_LOCAL_MODEL_URL=http://localhost:11434/v1   # the Ollama OpenAI endpoint
export IRONCLAW_LOCAL_MODEL=llama3.2                        # the model you pulled
```

Then point an agent group at the `local` provider and send it a message. For the
full end-to-end walkthrough (build, run, first reply), follow the
[Run a 100% local model tutorial](../tutorials/local-model-ollama.md).

## Why run it locally and still sandbox

Running the model locally keeps your data on your box. IronClaw keeps a possibly
compromised agent from touching the rest of it.

- **gVisor per session.** Each conversation gets a fresh sandbox with a user-space
  kernel and `network=none`. The agent cannot reach your files, your LAN, or the
  internet.
- **Least-privilege egress.** Only your loopback Ollama host is allowlisted on the
  model-proxy.
- **No data leaves the machine.** Model, control-plane, and sandbox all run locally.

See the [containment and isolation proof](../security-isolation.md) and the full
[threat model](../threat-model.md).

## See also

- [Run a 100% local model (Ollama) tutorial](../tutorials/local-model-ollama.md) - full walkthrough
- [Choose your model provider](index.md) - capability matrix and decision guide
- [Run any model (OpenRouter)](run-any-model-openrouter.md)
- [Security and isolation](../security-isolation.md) - why the sandbox stays sealed
