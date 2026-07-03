---
title: "Run any model securely with OpenRouter"
description: Run any model through OpenRouter behind IronClaw, with one host-side key that never enters the agent container. Switch models without re-plumbing, and run every agent inside a sealed gVisor sandbox. Copy-paste setup.
---

# Run any model securely with OpenRouter

You want the freedom to run **any model** and swap between them without re-plumbing,
while every agent run stays isolated from your network and your key. IronClaw's
`openrouter` provider gives you the full OpenRouter model catalog behind a single
host-side key, called by an agent that runs inside a per-session **gVisor sandbox**
with `network=none`.

!!! abstract "Where your key lives"
    The sandbox holds **no** credential. It reaches OpenRouter only through the host
    **model-proxy** unix socket, which stamps each forwarded request with your
    `OPENROUTER_API_KEY` on the way out. The key never enters the sandbox image, its
    environment, or its filesystem. See [Security and isolation](../security-isolation.md).

## 1. See it run with no credentials first

Prove the sandbox loop end to end with the offline demo (one Docker command, no
key) via the
[zero-credential quickstart](../quickstart.md#a-working-chat-in-5-minutes-no-credentials),
then point a group at OpenRouter below.

## 2. Set your OpenRouter key host-side

Set one key in the **control-plane** environment, never the sandbox:

```bash
export OPENROUTER_API_KEY=sk-or-…
```

## 3. Point an agent group at OpenRouter

Opt a group in explicitly (a present key never forces any group to use it):

- **Provider:** `openrouter`
- **Model:** any OpenRouter model id (for example
  `anthropic/claude-3.5-sonnet`, `openai/gpt-4o`, or `meta-llama/llama-3.1-70b-instruct`)

Set it on the group's **Provider** and **Model** fields in the web console, or
submit the change with `ironctl` and approve it at the human gateway so the switch
lands on the [audit log](../observability.md). Because OpenRouter fronts many
vendors, you can move a group between models by changing one field.

## Why "securely" is the point

OpenRouter gives you model choice. IronClaw makes each of those models safe to hand
to an autonomous agent.

- **gVisor per session.** Each conversation gets a fresh sandbox with a user-space
  kernel and `network=none`. A compromised agent cannot reach your machine or the
  internet.
- **Least-privilege egress.** Only the OpenRouter host is allowlisted on the
  model-proxy.
- **Credential custody on the host.** The key is stamped in the proxy. The agent
  sees a model reply, never the secret.

See the [containment and isolation proof](../security-isolation.md) and the full
[threat model](../threat-model.md).

## See also

- [Choose your model provider](index.md) - capability matrix and decision guide
- [Run Claude in an isolated sandbox (AWS Bedrock)](run-claude-sandbox-bedrock.md)
- [Run Llama locally (Ollama)](run-llama-locally-ollama.md)
- [Security and isolation](../security-isolation.md) - why credentials stay host-side
