---
title: "Choose your model provider"
description: One page to pick the right model backend for IronClaw. Compare auth, streaming, and credential handling across mock, local, Anthropic, OpenAI, OpenRouter, Codex, Gemini, Vertex, Bedrock, and Azure, with a short decision guide and copy-paste setup.
---

# Choose your model provider

IronClaw runs your agent behind a sealed sandbox and talks to whatever model you
point it at. **You pick the backend per agent group** — the rest of the system does
not change. This page helps you choose one, then links you straight to setup.

!!! abstract "The one invariant, whatever you choose"
    Every provider credential is held **host-side** and injected by the host
    model-proxy on the way out. **No key ever enters a sandbox.** The agent sees a
    model reply, never the secret that paid for it. That is true for a static API
    key, an OAuth bearer, or AWS SigV4 credentials alike — the difference between
    providers is *where the credential comes from*, not *whether it stays host-side*.
    See [Security and isolation](../security-isolation.md).

## Pick in 15 seconds

<div class="grid cards" markdown>

-   :material-lan-disconnect: __Local / offline / zero credential__

    No cloud, no key, nothing leaves the box.

    → **`mock`** for a demo or CI, **`local`** (Ollama / LM Studio / vLLM) for a
    real model on your own hardware.

-   :material-cloud-outline: __Hosted, fastest to first token__

    You have an API key and want the strongest model now.

    → **`anthropic`** (default), **`openai`**, **`openrouter`**, or **`gemini`**.

-   :material-office-building-outline: __Enterprise / governed cloud__

    Billing, IAM, and data boundary must live in your cloud account.

    → **`azure`** (Azure OpenAI), **`bedrock`** (AWS), or **`vertex`** (Google Cloud).

-   :material-account-key-outline: __Reuse an existing subscription__

    You already pay for ChatGPT or a gateway-fronted credential.

    → **`codex`** (ChatGPT/Codex OAuth) via a local credential gateway.

</div>

## Capability matrix

| Provider | Kind | Auth method | Credential source (host-side) | Streaming | Best for | Setup |
|---|---|---|---|---|---|---|
| **Mock** | `mock` | none | none — offline, deterministic | n/a | Demos, e2e tests, first run | [Quickstart](../quickstart.md#a-working-chat-in-5-minutes-no-credentials) |
| **Local / self-hosted** | `local` | none (optional key) | `IRONCLAW_LOCAL_MODEL_KEY` only if your server requires one | server-dependent | 100% local model, no data egress | [Ollama tutorial](../tutorials/local-model-ollama.md) |
| **Anthropic** _(default)_ | `anthropic` | API key | `ANTHROPIC_API_KEY` | yes (SSE) | Strongest default, tool use | [Quickstart](../quickstart.md#a-working-chat-in-5-minutes-no-credentials) |
| **OpenAI** | `openai` | API key | `OPENAI_API_KEY` | yes (SSE) | GPT-class models | [Setup](#anthropic-openai-openrouter) |
| **OpenRouter** | `openrouter` | API key | `OPENROUTER_API_KEY` | yes (SSE) | One key, many models | [Setup](#anthropic-openai-openrouter) |
| **Google Gemini** | `gemini` | API key | `GOOGLE_API_KEY` (or `GEMINI_API_KEY`) | yes (SSE) | Hosted Google models, generous free tier | [Setup](#gemini-google-ai-studio) |
| **Google Vertex AI** | `vertex` | OAuth2 bearer | gcloud ADC (`GOOGLE_VERTEX_USE_GCLOUD=1`) or `GOOGLE_VERTEX_ACCESS_TOKEN`; `GOOGLE_VERTEX_PROJECT` required | yes (SSE) | Gemini under GCP billing / IAM | [Setup](#vertex-ai-google-cloud) |
| **AWS Bedrock** | `bedrock` | AWS SigV4 | `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` (+ `AWS_SESSION_TOKEN`), `AWS_REGION` | no (InvokeModel) | Claude/others under AWS billing / IAM | [Setup](#aws-bedrock) |
| **ChatGPT / Codex** | `codex` | OAuth via gateway | credential gateway (`IRONCLAW_MODEL_GATEWAY_URL`, e.g. OneCLI) | yes (SSE) | Reuse a ChatGPT/Codex subscription | [Setup](#codex-chatgpt-via-a-credential-gateway) |
| **Azure OpenAI** | `azure` | api-key or Entra token | `AZURE_OPENAI_API_KEY` (or `AZURE_OPENAI_ACCESS_TOKEN`) + `AZURE_OPENAI_ENDPOINT` | yes (SSE) | GPT-class models under Azure billing / IAM | [Azure OpenAI](azure.md) |

!!! note "Streaming, and what it means here"
    Where a provider streams, IronClaw consumes the upstream token stream and
    **accumulates the full reply before it returns** — streaming is a transport
    detail on the host side, not a partial-render feature in the console. Bedrock
    uses the non-stream `InvokeModel` call today. Either way you get one complete,
    audited reply.

## Decision guide

- **Just want to see it work?** Start with **`mock`** — no key, no network. The
  [Quickstart](../quickstart.md) zero-credential demo replies in seconds.
- **Privacy or air-gap is the constraint?** Run **`local`** against Ollama, LM
  Studio, vLLM, or llama.cpp on your own box. Nothing leaves the machine, and no
  cloud credential is required. See the [local model tutorial](../tutorials/local-model-ollama.md).
- **You have an API key and want the best answer now?** Use **`anthropic`** (the
  default), **`openai`**, **`openrouter`**, or **`gemini`**. One environment
  variable and a restart.
- **Procurement, billing, and IAM must stay in your cloud?** Use **`azure`** (Azure
  OpenAI), **`bedrock`** (AWS), or **`vertex`** (Google Cloud). Credentials come from
  your existing cloud identity — an Azure api-key or Entra token, AWS SigV4, or gcloud
  ADC — and stay host-side.
- **Already paying for ChatGPT?** Front a **`codex`** OAuth credential with a local
  [credential gateway](../mcp.md) and reuse it.

## Setup by provider

Set **one** credential in the control-plane's environment and restart it. Every
value below lives host-side only.

### Anthropic, OpenAI, OpenRouter

```bash
export ANTHROPIC_API_KEY=sk-ant-…       # default provider
# or
export OPENAI_API_KEY=sk-…
# or
export OPENROUTER_API_KEY=sk-or-…
```

### Gemini (Google AI Studio)

```bash
export GOOGLE_API_KEY=…                  # GEMINI_API_KEY is honored as a fallback
```

### Vertex AI (Google Cloud)

Vertex speaks the same wire format as Gemini but authenticates with a short-lived
OAuth bearer that IronClaw refreshes host-side. A project is required; the region
defaults when unset.

```bash
export GOOGLE_VERTEX_PROJECT=my-gcp-project   # or GOOGLE_CLOUD_PROJECT
export GOOGLE_VERTEX_LOCATION=us-central1     # optional; provider default otherwise
export GOOGLE_VERTEX_USE_GCLOUD=1             # use gcloud Application Default Credentials
# or supply a token you refresh out of band:
# export GOOGLE_VERTEX_ACCESS_TOKEN=ya29.…
```

### AWS Bedrock

For orgs that consume models only through Bedrock. Credentials come from the
standard AWS environment; the region selects the regional
`bedrock-runtime.{region}.amazonaws.com` host.

```bash
export AWS_ACCESS_KEY_ID=AKIA…
export AWS_SECRET_ACCESS_KEY=…
export AWS_SESSION_TOKEN=…                # optional, for temporary credentials
export AWS_REGION=us-east-1               # or AWS_DEFAULT_REGION
```

!!! note "How Bedrock auth works"
    Host-side SigV4 signing is validated against AWS's reference vectors; requests
    use the non-stream `InvokeModel` API. The signature is region-bound, so
    `AWS_REGION` is required and no static credential ever enters the sandbox.

### Codex (ChatGPT) via a credential gateway

The Codex path routes model egress through a local, operator-vetted credential
gateway (such as OneCLI) that injects a ChatGPT/Codex OAuth token. The
control-plane and sandbox then hold **no** model credential at all.

```bash
export IRONCLAW_MODEL_GATEWAY_URL=http://127.0.0.1:10255
export IRONCLAW_MODEL_GATEWAY_HOSTS=chatgpt.com
```

### Azure OpenAI

For orgs that consume models only through Azure. Azure routes by **deployment name**
in the URL and authenticates with an `api-key` header or a Microsoft Entra bearer
token. See the full [Azure OpenAI guide](azure.md).

```bash
export AZURE_OPENAI_ENDPOINT=https://my-resource.openai.azure.com
export AZURE_OPENAI_API_KEY=…                 # or AZURE_OPENAI_ACCESS_TOKEN for Entra
export AZURE_OPENAI_API_VERSION=2024-10-21    # optional; provider default otherwise
```

## Point an agent group at a provider

A provider credential being present does not force any group to use it — you opt a
group in explicitly. Two surfaces:

- **Web console** → **Agents** → edit a group → **Provider** field (leave blank for
  the default Anthropic backend; set `openai`, `gemini`, `vertex`, `bedrock`,
  `local`, etc. to route that group elsewhere). The group's provider and model show
  on its card and in the first-run **Setup** checklist.
- **CLI** — submit a model/persona change with `ironctl` and approve it through the
  human gateway, so the switch lands on the [audit log](../observability.md) like any
  other change. See the [Quickstart](../quickstart.md#4-submit-a-change-watch-it-get-held).

Leaving the field blank keeps the sealed, single-provider default posture: only
groups that opt in reach another backend.

## Task-focused guides

Prefer to start from what you want to run? These pages lead with the job and give
you the exact setup:

- [Run Claude in an isolated sandbox (AWS Bedrock)](run-claude-sandbox-bedrock.md)
- [Run GPT securely (Azure OpenAI)](run-gpt-securely-azure-openai.md)
- [Run Llama locally (Ollama)](run-llama-locally-ollama.md)
- [Run any model (OpenRouter)](run-any-model-openrouter.md)

## See also

- [Quickstart](../quickstart.md) — first working chat, then a real provider
- [Run a 100% local model (Ollama)](../tutorials/local-model-ollama.md)
- [Security and isolation](../security-isolation.md) — why keys stay host-side
- [FAQ: which providers are supported?](../faq.md#which-model-providers-are-supported)
