---
title: Azure OpenAI (Azure AI Foundry)
description: Run IronClaw against models served through Azure OpenAI / Azure AI Foundry, authenticated host-side with an api-key or a Microsoft Entra token. The sandbox never holds your Azure credentials.
---

# Azure OpenAI

Many organizations can only reach foundation models through **Azure OpenAI** (Azure AI Foundry) —
a direct OpenAI or Anthropic key is not allowed by policy, but an Azure subscription with a
deployed model is. IronClaw's `azure` provider serves exactly that case. Azure OpenAI speaks the
same **OpenAI Chat Completions** wire format IronClaw already uses, so tool use and streaming work
unchanged; only the request envelope and the auth header differ.

!!! info "Where the credential lives"
    As with every provider, the sandbox has `network=none` and holds **no** credential. It reaches
    Azure only through the host **model-proxy** unix socket. The proxy is the sole authenticator:
    it stamps each forwarded request with your **`api-key`** (or a Microsoft **Entra** bearer token)
    on the way out. Your `AZURE_OPENAI_API_KEY` never enters the sandbox image, its environment, or
    its filesystem.

## How it differs from the OpenAI provider

Azure OpenAI is the OpenAI Chat Completions API with a different envelope, all handled for you:

| | OpenAI (`openai`) | Azure OpenAI (`azure`) |
|---|---|---|
| Host | `api.openai.com` | `<resource>.openai.azure.com` |
| Model selection | `model` field in the body | **deployment name** in the URL path |
| API version | n/a | `api-version` query parameter |
| Path | `/v1/chat/completions` | `/openai/deployments/<deployment>/chat/completions` |
| Auth | `Authorization: Bearer` (host-side) | `api-key` header **or** Entra `Authorization: Bearer` (host-side) |

Azure routes by **deployment**, not by model id — the deployment you created in the Azure portal
selects the underlying model — and the host is **per-resource**, so the provider requires an
explicit model host and deployment. The control-plane fills the host in for you from
`AZURE_OPENAI_ENDPOINT` (below).

## 1. Enable Azure OpenAI on the control-plane

Set your Azure OpenAI **endpoint** plus one credential in the **control-plane** environment (never
the sandbox). A static api-key or a Microsoft Entra ID (Azure AD) bearer token both work:

```sh
export AZURE_OPENAI_ENDPOINT=https://my-resource.openai.azure.com   # your resource endpoint
export AZURE_OPENAI_API_KEY=…                                       # static api-key, OR:
# export AZURE_OPENAI_ACCESS_TOKEN=…                                # Microsoft Entra bearer (refresh out of band)
export AZURE_OPENAI_API_VERSION=2024-10-21                          # optional; provider default otherwise
```

When `AZURE_OPENAI_ENDPOINT` and one credential are set, the control-plane:

- allowlists your `<resource>.openai.azure.com` host on the model-proxy egress allowlist, and
- installs the injector that stamps every forwarded Azure request with your credential — the
  `api-key` header for a static key, or `Authorization: Bearer` for an Entra token.

Only `*.openai.azure.com` endpoints are accepted, so a misconfigured endpoint cannot silently widen
egress to an arbitrary host.

!!! warning "One resource per deployment"
    The control-plane allowlists and authenticates a single Azure resource host (from
    `AZURE_OPENAI_ENDPOINT`). Point agent groups at deployments on that resource. To serve a second
    resource, run a second deployment or extend the allowlist.

## 2. Point an agent group at Azure

Pin a group to the `azure` provider and your **deployment name**. The host and api-version are
inherited from the deployment, so you usually do not set them:

- **Provider:** `azure`
- **Model:** your Azure **deployment name** (e.g. `gpt-4o` — the name you gave the deployment, not
  the base model id)

Or make Azure the deployment default for provider-less groups:

```sh
export IRONCLAW_DEV_PROVIDER=azure
export IRONCLAW_DEV_MODEL=gpt-4o        # your deployment name
```

Under the hood the sandbox launches with
`--provider azure --model <deployment> --model-host <resource>.openai.azure.com --model-api-version <version>` —
the same flags you can pass directly when running `cmd/sandbox` by hand.

## 3. Verify

Send the group a message. On success you get a normal reply; the model-proxy audit log records a
`200` to `<resource>.openai.azure.com`. Common failures:

| Symptom | Cause | Fix |
|---|---|---|
| `401 ... Access denied due to invalid subscription key` | wrong/expired `AZURE_OPENAI_API_KEY` | refresh the key in the control-plane environment |
| `404 ... DeploymentNotFound` | group's model is not a deployment on this resource | set **Model** to the exact deployment name from the Azure portal |
| `destination not on allowlist` | endpoint host differs from the group's host | ensure the group inherits the deployment host, or match `AZURE_OPENAI_ENDPOINT` |
| `unsupported api-version` | the deployment needs a different `api-version` | set `AZURE_OPENAI_API_VERSION` (or the group's api-version) to a version the deployment supports |

## Security notes

- **Credential isolation.** The Azure key (or Entra token) stays on the host. The sandbox is
  treated as potentially compromised and never receives it; the proxy self-guards on the
  `*.openai.azure.com` host so the credential is stamped only on Azure requests.
- **Least-privilege egress.** Only your `<resource>.openai.azure.com` host is allowlisted. The
  sandbox cannot reach any other Azure service or the public internet.
- **No plaintext secrets in logs.** The injector never logs credential material; a token-source
  error leaves the request unauthenticated (the upstream rejects it) rather than surfacing the
  secret.

## Integration test

A live, env-gated test exercises the provider against a real Azure endpoint. It is skipped in CI
and any credential-free run:

```sh
export AZURE_OPENAI_ENDPOINT=https://my-resource.openai.azure.com
export AZURE_OPENAI_API_KEY=…
export AZURE_OPENAI_DEPLOYMENT=gpt-4o
go test ./internal/sandbox/provider/ -run TestAzureIntegration -v
```
