---
title: "Bring your own model: Ollama, Gemini, or Vertex in 5 minutes"
description: "Run an IronClaw agent on the model you already have, local, private, or your own cloud account. The model key never enters the sandbox, and provider selection is configuration, not a fork."
---

## The job: run an agent on *your* model, not someone else's

Plenty of agent runtimes assume one vendor and one cloud. The job a self-hoster is hiring for is the opposite:
**"use the model I already have, whether local, private, or my own cloud account, without rebuilding anything."**
IronClaw speaks the OpenAI-compatible API shape, so the model is a config choice, not a fork.

> IronClaw ships more backends than the three below (Anthropic, OpenAI, OpenRouter, Codex, and a credential-free `mock` provider too). For the full list with an auth-and-streaming comparison and a short decision guide, see [Choose your model provider](providers/index.md).


And because of how the runtime is built, **your model key never enters the agent sandbox**. Model calls are
brokered host-side. Bringing your own model doesn't mean trusting the box with your credential.

## Option A: Local model with Ollama (zero credentials)

This is the fastest path and needs no API key at all.

```bash
# 1. Have a model running locally (Ollama shown; LM Studio / vLLM work the same way)
ollama serve
ollama pull llama3.1

# 2. Point IronClaw at the local OpenAI-compatible endpoint
#    The model proxy allows plain-HTTP loopback for local upstreams.
ironctl onboard      # choose the local/OpenAI-compatible provider, endpoint http://localhost:11434/v1

# 3. Run a conversation, and the agent now reasons on your local model,
#    fully offline, no key, no cloud round-trip.
```

LM Studio and vLLM expose the same OpenAI-compatible surface; point the endpoint at theirs and it just works.
The model never leaves your machine, and there is no telemetry phoning home. Full walkthrough: [Run a 100% local model (Ollama)](tutorials/local-model-ollama.md).

## Option B: Google Gemini (AI Studio key)

```bash
export GEMINI_API_KEY=...     # or GOOGLE_API_KEY
ironctl onboard                # choose the Gemini provider
```

IronClaw maps your key to Gemini's `x-goog-api-key` header host-side; the sandbox never sees it.

## Option C: Google Vertex AI (your GCP project)

```bash
# Uses host-side OAuth against your GCP credentials; default region us-central1
ironctl onboard                # choose the Vertex provider, set project + region
```

Vertex reuses the same Gemini request path with host-side OAuth injection. Same isolation guarantees, your
cloud account.

## Try it with no model at all: the `mock` provider

Want to see the loop end-to-end before wiring a real model? IronClaw ships a **credential-free `mock`
provider** and a seeded mock agent, so you can run the full message-to-reason-to-approve-to-execute loop with
zero setup. That's also how our zero-credential hero demo works.

## Why "5 minutes" is honest

The slow part of most agent setups is credentials and cloud config. The local path removes both: a running
Ollama plus `ironctl onboard` is the whole story. Hosted providers add exactly one secret (a key or OAuth),
injected host-side. There's no model-specific rebuild; provider selection is configuration.

## Where this is honest about its limits

- **Still alpha.** Config formats can change between versions, so pin your version.
- **Coverage is uneven by design.** The control plane, gateway, and encrypted queues carry real
  test coverage. Live per-provider routing has lighter end-to-end coverage. The local and `mock`
  paths are the ones you can verify offline today, and we would rather you know exactly which is which.

Found a provider quirk? That's a great first issue. The repo is open and we triage fast.
