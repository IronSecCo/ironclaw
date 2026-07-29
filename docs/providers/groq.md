---
title: "Groq (fast OpenAI-compatible inference)"
description: Run IronClaw against Groq's OpenAI-compatible chat-completions API — first-class groq provider, default host and model, per-group setup, and how the key stays host-side.
---

# Groq provider

**`groq`** is IronClaw's fast-inference OpenAI-compatible backend: a single
[Groq](https://groq.com) key unlocks Groq-hosted LLMs (Llama, Mixtral, and the
open `gpt-oss` family) behind the **identical OpenAI Chat Completions wire
format**. Because Groq serves that API under `/openai/v1`, IronClaw reuses the
same request/streaming/tool-use path as the `openai` provider; the `groq` kind
only changes the upstream host and the request path.

!!! info "Where the credential lives"
    As with every provider, the sandbox has `network=none` and holds **no**
    credential. It reaches Groq only through the host **model-proxy** unix socket.
    The proxy is the sole authenticator: it stamps each forwarded request with
    your Groq key (`Authorization: Bearer`) on the way out. Your `GROQ_API_KEY`
    never enters the sandbox image, its environment, or its filesystem.

## How it differs from the OpenAI provider

Same wire format, a different host and path — all handled for you:

| | OpenAI (`openai`) | Groq (`groq`) |
|---|---|---|
| Host | `api.openai.com` | `api.groq.com` |
| Path | `/v1/chat/completions` | `/openai/v1/chat/completions` |
| Model id | `gpt-4o` | `openai/gpt-oss-120b` (default), or any Groq-hosted model |
| Auth | `Authorization: Bearer` (host-side) | `Authorization: Bearer` (host-side) |
| Reach | OpenAI models | Groq-hosted open models (Llama, Mixtral, gpt-oss) |

Groq routes by the **model id** in the request body, exactly like the OpenAI
API. The host is a single global endpoint, so — unlike Azure — no per-resource
host is required.

## 1. Enable Groq on the control-plane

Set your Groq key in the **control-plane** environment (never the sandbox):

```sh
export GROQ_API_KEY=gsk_…
```

When `GROQ_API_KEY` is set, the control-plane:

- allowlists `api.groq.com` on the model-proxy egress allowlist, and
- installs the injector that stamps every forwarded Groq request with your key
  as an `Authorization: Bearer` header.

The injector self-guards on the `api.groq.com` host, so the key is stamped only
on Groq requests and can never leak to another upstream.

## 2. Point an agent group at Groq

Pin a group to the `groq` provider and a Groq-hosted model id:

```sh
ironctl agent create --yes --id groq-helper --name "Groq Helper" \
  --provider groq --model openai/gpt-oss-120b
```

Or make Groq the deployment default for provider-less groups:

```sh
export IRONCLAW_DEV_PROVIDER=groq
export IRONCLAW_DEV_MODEL=openai/gpt-oss-120b
```

Browse available ids at [console.groq.com](https://console.groq.com/settings/models);
any model Groq hosts works — `openai/gpt-oss-120b`, `openai/gpt-oss-20b`,
`meta-llama/llama-3.3-70b-versatile` (when available), and so on.

!!! note "Default model"
    The shipped default is `openai/gpt-oss-120b`. The `llama-3.3-70b-versatile`
    id suggested in earlier notes is **deprecated** on Groq's platform; prefer
    the `gpt-oss` family or check the Groq console for the current catalog.

## 3. Verify

Send the group a message. On success you get a normal reply; the model-proxy
audit log records a `200` to `api.groq.com`. Common failures:

| Symptom | Cause | Fix |
|---|---|---|
| `401 ... No auth credentials found` | missing/invalid `GROQ_API_KEY` | set a valid key in the control-plane environment |
| `404 ... model not found` | the model id is wrong or not hosted on Groq | use an id listed at [console.groq.com](https://console.groq.com/settings/models) |
| `destination not on allowlist` | `GROQ_API_KEY` was unset when the control-plane started | set the key and restart so `api.groq.com` is allowlisted |

## Security notes

- **Credential isolation.** The Groq key stays on the host. The sandbox is
  treated as potentially compromised and never receives it; the proxy
  self-guards on the `api.groq.com` host so the key is stamped only on Groq
  requests.
- **Least-privilege egress.** Only `api.groq.com` is allowlisted. The sandbox
  cannot reach any other host or the public internet.
- **No plaintext secrets in logs.** The injector never logs credential
  material, and the model-proxy redacts the key from any response body it
  forwards.