---
title: "OpenRouter (one key, 100+ models)"
description: Run IronClaw against 100+ models — Claude, GPT, Llama, Mistral, Gemini — through a single OpenRouter key. First-class openrouter provider, vendor/model ids, per-group setup, and how the key stays host-side.
---

# OpenRouter provider

**`openrouter`** is IronClaw's widest-reach backend: a single
[OpenRouter](https://openrouter.ai) key unlocks **100+ models** across vendors —
Claude, GPT, Llama, Mistral, Gemini, and more — behind one integration. Swap models
by changing a `vendor/model` id; no new credential, host, or wiring per model.

OpenRouter serves the **identical OpenAI Chat Completions wire format**
(`POST /api/v1/chat/completions`) as the hosted `openai` provider, so IronClaw reuses
the same request/streaming/tool-use path. The `openrouter` kind only changes the
upstream host and the request path.

!!! info "Where the credential lives"
    As with every provider, the sandbox has `network=none` and holds **no** credential.
    It reaches OpenRouter only through the host **model-proxy** unix socket. The proxy
    is the sole authenticator: it stamps each forwarded request with your OpenRouter key
    (`Authorization: Bearer`) on the way out. Your `OPENROUTER_API_KEY` never enters the
    sandbox image, its environment, or its filesystem.

## How it differs from the OpenAI provider

Same wire format, a different gateway — all handled for you:

| | OpenAI (`openai`) | OpenRouter (`openrouter`) |
|---|---|---|
| Host | `api.openai.com` | `openrouter.ai` |
| Path | `/v1/chat/completions` | `/api/v1/chat/completions` |
| Model id | `gpt-4o` | `vendor/model`, e.g. `anthropic/claude-3.5-sonnet` |
| Auth | `Authorization: Bearer` (host-side) | `Authorization: Bearer` (host-side) |
| Reach | OpenAI models | 100+ models across many vendors |

OpenRouter routes by the **`vendor/model`** id in the request body — the vendor prefix
selects the upstream provider OpenRouter fronts. The host is a single global endpoint,
so — unlike Azure — no per-resource host is required.

## 1. Enable OpenRouter on the control-plane

Set your OpenRouter key in the **control-plane** environment (never the sandbox):

```sh
export OPENROUTER_API_KEY=sk-or-…
```

When `OPENROUTER_API_KEY` is set, the control-plane:

- allowlists `openrouter.ai` on the model-proxy egress allowlist, and
- installs the injector that stamps every forwarded OpenRouter request with your key as
  an `Authorization: Bearer` header.

The injector self-guards on the `openrouter.ai` host, so the key is stamped only on
OpenRouter requests and can never leak to another upstream.

## 2. Point an agent group at OpenRouter

Pin a group to the `openrouter` provider and a `vendor/model` id:

```sh
ironctl agent create --yes --id router-helper --name "Router Helper" \
  --provider openrouter --model anthropic/claude-3.5-sonnet
```

Or make OpenRouter the deployment default for provider-less groups:

```sh
export IRONCLAW_DEV_PROVIDER=openrouter
export IRONCLAW_DEV_MODEL=anthropic/claude-3.5-sonnet
```

Browse available ids at [openrouter.ai/models](https://openrouter.ai/models); any
`vendor/model` OpenRouter supports works — `openai/gpt-4o`, `meta-llama/llama-3.1-70b-instruct`,
`mistralai/mistral-large`, `google/gemini-2.0-flash-001`, and so on.

A complete runnable version — create the group, send a chat, print the reply — is in
[`examples/openrouter/`](https://github.com/IronSecCo/ironclaw/tree/main/examples/openrouter).

## 3. Verify

Send the group a message. On success you get a normal reply; the model-proxy audit log
records a `200` to `openrouter.ai`. Common failures:

| Symptom | Cause | Fix |
|---|---|---|
| `401 ... No auth credentials found` | missing/invalid `OPENROUTER_API_KEY` | set a valid key in the control-plane environment |
| `404 ... No endpoints found for <id>` | the `vendor/model` id is wrong or unavailable | use an id listed at [openrouter.ai/models](https://openrouter.ai/models) |
| `destination not on allowlist` | `OPENROUTER_API_KEY` was unset when the control-plane started | set the key and restart so `openrouter.ai` is allowlisted |
| `402 ... insufficient credits` | the OpenRouter account is out of credit | top up the OpenRouter account |

## Security notes

- **Credential isolation.** The OpenRouter key stays on the host. The sandbox is
  treated as potentially compromised and never receives it; the proxy self-guards on
  the `openrouter.ai` host so the key is stamped only on OpenRouter requests.
- **Least-privilege egress.** Only `openrouter.ai` is allowlisted. The sandbox cannot
  reach any other host or the public internet — even though OpenRouter fronts many
  vendors, egress from the box is one host.
- **No plaintext secrets in logs.** The injector never logs credential material, and
  the model-proxy redacts the key from any response body it forwards.

## Try it credential-free first

Not sure yet? The [`examples/openrouter/`](https://github.com/IronSecCo/ironclaw/tree/main/examples/openrouter)
setup runs against the offline **`mock`** provider with `PROVIDER=mock`, proving the
full sealed round-trip with **no key at all**, before you point it at a real
OpenRouter model.
